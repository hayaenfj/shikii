package notebook

import (
	"fmt"
	"slices"
	"testing"
)

// A page whose original evidence cannot be read holds the batch today: it is the first unreviewed task, so prepare keeps returning it
// and the next page never comes (2026-11-16, 29 of 103 reviewed). The agent declares it blocked at its current revision with a reason;
// the batch moves on, the page is reported by its id and does not count as reviewed, and it returns to review once its revision changes.
func TestMemoryUpgradeSkipsAPageDeclaredBlocked(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	// one page more than a batch holds, so that the page after the batch is the one that enters when a page is declared blocked
	total := MemoryUpgradeBatch + 1
	for i := 1; i <= total; i++ {
		if _, err := b.Ingest(draft(seed(t, b, i), fmt.Sprintf("Rule %02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Total != total || len(u.Tasks) != MemoryUpgradeBatch {
		t.Fatal("start with a full batch of bounded tasks", u, err)
	}
	blocked := u.Tasks[0]
	if err := b.BlockMemoryUpgrade(blocked.PageID, blocked.Revision, blocked.Key, ""); err == nil {
		t.Fatal("a blocked declaration needs a reason")
	}
	if err := b.BlockMemoryUpgrade(blocked.PageID, blocked.Revision, blocked.Key, "The cited source file no longer exists."); err != nil {
		t.Fatal("a page can be declared blocked at its current task", err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Reviewed != 0 {
		t.Fatal("a blocked page is neither reviewed nor the end of the upgrade", u, err)
	}
	if !slices.Contains(u.Blocked, blocked.PageID) {
		t.Fatal("the blocked page must be reported by its id", u.Blocked)
	}
	if len(u.Tasks) != MemoryUpgradeBatch || slices.ContainsFunc(u.Tasks, func(task MaintenanceTask) bool { return task.PageID == blocked.PageID }) {
		t.Fatal("the batch must move on past the blocked page to the next unreviewed pages", u.Tasks)
	}

	resumed := Book{Root: b.Root, Scope: b.Scope}
	for _, task := range u.Tasks {
		if _, err := resumed.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Reviewed the sources and current policy."}); err != nil {
			t.Fatal(err)
		}
	}
	u, err = resumed.MemoryUpgrade()
	if err != nil || u.Status != "blocked" || u.Reviewed != MemoryUpgradeBatch || len(u.Tasks) != 0 || !slices.Contains(u.Blocked, blocked.PageID) {
		t.Fatal("when only blocked pages remain the upgrade says so instead of waiting", u, err)
	}

	// a stale declaration is refused: the page changed after the agent read it
	before := blocked
	d := draft(seed(t, b, 1), "Blocked rule")
	d.ID = blocked.PageID
	d.Lesson += " Updated conditions."
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if err := b.BlockMemoryUpgrade(before.PageID, before.Revision, before.Key, "The old revision is blocked."); err == nil {
		t.Fatal("a declaration for an earlier revision must be refused")
	}
	u, err = resumed.MemoryUpgrade()
	if err != nil || slices.Contains(u.Blocked, blocked.PageID) || len(u.Tasks) != 1 || u.Tasks[0].PageID != blocked.PageID {
		t.Fatal("a page whose revision changed returns to review", u, err)
	}
}

// startUpgrade makes one article per title, starts the memory upgrade and returns the task of each article by its title.
func startUpgrade(t *testing.T, b Book, titles ...string) map[string]MaintenanceTask {
	t.Helper()
	for i, title := range titles {
		if _, err := b.Ingest(draft(seed(t, b, i+1), title)); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.MemoryUpgrade()
	if err != nil || len(u.Tasks) != len(titles) {
		t.Fatal(u, err)
	}
	tasks := map[string]MaintenanceTask{}
	for _, task := range u.Tasks {
		tasks[task.Title] = task
	}
	return tasks
}

func reviewUpgradeTask(t *testing.T, b Book, task MaintenanceTask) {
	t.Helper()
	if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked current policy and original evidence."}); err != nil {
		t.Fatal(err)
	}
}

func blockUpgradeTask(t *testing.T, b Book, task MaintenanceTask) {
	t.Helper()
	if err := b.BlockMemoryUpgrade(task.PageID, task.Revision, task.Key, "The original evidence cannot be read."); err != nil {
		t.Fatal(err)
	}
}

func upgradeCheckpointOf(t *testing.T, b Book) memoryUpgradeCheckpoint {
	t.Helper()
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	c, err := readMemoryUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// One page declared blocked keeps the upgrade from completing for good, so a refresh that only reopened a completed upgrade would never add
// the articles written after it. A refresh adds the new articles to an upgrade that is not complete as well.
func TestRefreshMemoryUpgradeAddsANewArticleWhileAPageIsBlocked(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	tasks := startUpgrade(t, b, "Reviewed", "Blocked")
	reviewUpgradeTask(t, b, tasks["Reviewed"])
	blockUpgradeTask(t, b, tasks["Blocked"])
	if u, err := b.MemoryUpgrade(); err != nil || u.Status != "blocked" || u.Total != 2 {
		t.Fatalf("the upgrade stops with one page blocked: %+v %v", u, err)
	}
	added, err := b.Ingest(draft(seed(t, b, 9), "Written after"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Total != 3 || u.Reviewed != 1 || len(u.Tasks) != 1 || u.Tasks[0].PageID != added.Page.ID {
		t.Fatalf("the article written after the upgrade is a target and comes back for review, the reviewed one stays reviewed: %+v %v", u, err)
	}
	if !slices.Contains(u.Blocked, tasks["Blocked"].PageID) {
		t.Fatalf("the blocked page stays blocked: %v", u.Blocked)
	}
}

// Only live articles are added: one that was withdrawn or deleted, and one another article has replaced, are not (the replacement is).
func TestRefreshMemoryUpgradeDoesNotAddWithdrawnDeletedOrReplacedArticlesWhileAPageIsBlocked(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	tasks := startUpgrade(t, b, "Reviewed", "Blocked")
	reviewUpgradeTask(t, b, tasks["Reviewed"])
	blockUpgradeTask(t, b, tasks["Blocked"])
	withdrawn, err := b.Ingest(draft(seed(t, b, 10), "Withdrawn at once"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Withdraw(Withdrawal{ID: withdrawn.Page.ID, Revision: withdrawn.Page.Revision, Sources: []string{fmt.Sprintf("%016x", 10)}, Reason: "The work that made it took it back."}); err != nil {
		t.Fatal(err)
	}
	deleted, err := b.Ingest(draft(seed(t, b, 11), "Deleted at once"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Manage(Change{ID: deleted.Page.ID, Revision: deleted.Page.Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	replaced, err := b.Ingest(draft(seed(t, b, 12), "Replaced at once"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := draft(seed(t, b, 13), "Its replacement")
	replacement.Replaces = []string{replaced.Page.ID}
	kept, err := b.Ingest(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for id := range upgradeCheckpointOf(t, b).Targets {
		got = append(got, id)
	}
	want := []string{tasks["Reviewed"].PageID, tasks["Blocked"].PageID, kept.Page.ID}
	slices.Sort(got)
	slices.Sort(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the targets are the two articles of the upgrade and the replacement, not the withdrawn, the deleted and the replaced one:\n got %v\nwant %v", got, want)
	}
}

// What the upgrade has recorded stays when a refresh adds an article: the reviews, the withdrawn articles and the blocked marks.
func TestRefreshMemoryUpgradeKeepsTheReviewsTheWithdrawalsAndTheBlockedMarks(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	tasks := startUpgrade(t, b, "Reviewed", "Blocked", "Withdrawn")
	reviewUpgradeTask(t, b, tasks["Reviewed"])
	blockUpgradeTask(t, b, tasks["Blocked"])
	if _, err := b.Withdraw(Withdrawal{ID: tasks["Withdrawn"].PageID, Revision: tasks["Withdrawn"].Revision, Sources: []string{fmt.Sprintf("%016x", 3)}, Reason: "The work that made it took it back."}); err != nil {
		t.Fatal(err)
	}
	if u, err := b.MemoryUpgrade(); err != nil || u.Status != "blocked" || u.Withdrawn != 1 { // this call records the withdrawal
		t.Fatalf("premise: the upgrade stops with one page blocked and one withdrawn: %+v %v", u, err)
	}
	before := upgradeCheckpointOf(t, b)
	if len(before.Reviews) != 1 || len(before.Blocked) != 1 || len(before.Withdrawn) != 1 {
		t.Fatalf("premise: one review, one blocked mark and one withdrawal are recorded: %+v", before)
	}
	added, err := b.Ingest(draft(seed(t, b, 9), "Written after"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	after := upgradeCheckpointOf(t, b)
	if fmt.Sprint(after.Reviews) != fmt.Sprint(before.Reviews) || fmt.Sprint(after.Blocked) != fmt.Sprint(before.Blocked) || fmt.Sprint(after.Withdrawn) != fmt.Sprint(before.Withdrawn) {
		t.Fatalf("the reviews, the blocked marks and the withdrawals are as they were:\n%+v\n%+v", before, after)
	}
	if after.Targets[added.Page.ID] != added.Page.Revision || len(after.Targets) != 4 || after.Complete {
		t.Fatalf("the new article is added, nothing else is: %+v", after)
	}
}

// A complete upgrade is reopened even when no article is new: an article edited since comes back for review.
func TestRefreshMemoryUpgradeReopensACompleteUpgradeWhenNoArticleIsNew(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	pages := completedUpgrade(t, b, "Kept as it was", "Edited later")
	edited := draft(seed(t, b, 2), "Edited later")
	edited.ID, edited.Lesson = pages["Edited later"].ID, edited.Lesson+" A condition added after the review."
	if _, err := b.Ingest(edited); err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Total != 2 || u.Reviewed != 1 || len(u.Tasks) != 1 || u.Tasks[0].PageID != pages["Edited later"].ID {
		t.Fatalf("only the edited article is back for review: %+v %v", u, err)
	}
}
