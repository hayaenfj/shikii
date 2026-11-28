package notebook

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// saveResult saves one work result observed at the given time; n keeps the content (and so the id) of each one apart.
func saveResult(t *testing.T, b Book, kind WorkSource, n int, observed time.Time) string {
	t.Helper()
	o, err := b.CaptureWork(kind, fmt.Sprintf(`{"changed":"result %d"}`, n), "work", "test", observed)
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// The count behind the nap notice reads only a work result (the title shikii gives it) observed and saved after the time.
func TestCountWorkResultsSinceCountsOnlyWorkResultsSavedAfterTheTime(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	now := time.Now()
	since := now.Add(-24 * time.Hour)
	if n, err := b.CountWorkResultsSince(time.Time{}); err != nil || n != 0 {
		t.Fatalf("a project with no sources has no results: %d %v", n, err)
	}
	saveResult(t, b, WorkResult(), 1, now.Add(-time.Hour))
	saveResult(t, b, WorkResult(), 2, now.Add(-2*time.Hour))
	saveResult(t, b, WorkResult(), 3, now.Add(-48*time.Hour)) // observed before the time
	saveResult(t, b, WorkReview(), 4, now.Add(-time.Hour))    // another report of shikii, not a result
	if _, err := b.CaptureObserved(Observation{Kind: "agent", Title: "Checked the README", Content: "ok", Reference: "README.md", Version: "v", ObservedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	old := saveResult(t, b, WorkResult(), 5, now.Add(-time.Hour)) // observed after the time, but its file is older than the time
	dir, err := b.directory(false, "sources")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, old+".json"), now.Add(-72*time.Hour), now.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, err := b.CountWorkResultsSince(since); err != nil || n != 2 {
		t.Fatalf("two results were observed and saved after the time: %d %v", n, err)
	}
	if n, err := b.CountWorkResultsSince(time.Time{}); err != nil || n != 4 {
		t.Fatalf("with no time every result counts (a result of the same kind observed long ago too, and a file of any age): %d %v", n, err)
	}
}

// completedUpgrade makes the pages with the given titles and reviews every one of them, so that the memory upgrade is complete.
func completedUpgrade(t *testing.T, b Book, titles ...string) map[string]Page {
	t.Helper()
	pages := map[string]Page{}
	for i, title := range titles {
		r, err := b.Ingest(draft(seed(t, b, i+1), title))
		if err != nil {
			t.Fatal(err)
		}
		pages[title] = *r.Page
	}
	u, err := b.MemoryUpgrade()
	if err != nil || len(u.Tasks) != len(titles) {
		t.Fatal(u, err)
	}
	for _, task := range u.Tasks {
		if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked current policy and original evidence."}); err != nil {
			t.Fatal(err)
		}
	}
	if u, err = b.MemoryUpgrade(); err != nil || u.Status != "complete" {
		t.Fatal(u, err)
	}
	return pages
}

// A completed upgrade is reopened for what changed after it: an article edited since and an article that is new come back for review;
// an article reviewed and unchanged, and a new article another one has already replaced, do not.
func TestRefreshMemoryUpgradeBringsBackOnlyChangedAndNewArticles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	pages := completedUpgrade(t, b, "Kept as it was", "Edited later", "Also kept")
	edited := draft(seed(t, b, 2), "Edited later")
	edited.ID, edited.Lesson = pages["Edited later"].ID, edited.Lesson+" A condition added after the review."
	if _, err := b.Ingest(edited); err != nil {
		t.Fatal(err)
	}
	added, err := b.Ingest(draft(seed(t, b, 7), "New article"))
	if err != nil {
		t.Fatal(err)
	}
	oldNew, err := b.Ingest(draft(seed(t, b, 8), "New article replaced at once"))
	if err != nil {
		t.Fatal(err)
	}
	replacement := draft(seed(t, b, 9), "Its replacement")
	replacement.Replaces = []string{oldNew.Page.ID}
	replaced, err := b.Ingest(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if u, err := b.MemoryUpgrade(); err != nil || u.Status != "complete" || u.Total != 3 {
		t.Fatalf("before the refresh nothing changes a completed upgrade: %+v %v", u, err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Total != 5 || u.Reviewed != 2 {
		t.Fatalf("five targets (the three, the new article and its replacement), two of them reviewed and unchanged: %+v %v", u, err)
	}
	var back []string
	for _, task := range u.Tasks {
		back = append(back, task.PageID)
	}
	want := []string{pages["Edited later"].ID, added.Page.ID, replaced.Page.ID}
	sort.Strings(back)
	sort.Strings(want)
	if fmt.Sprint(back) != fmt.Sprint(want) {
		t.Fatalf("only the edited article and the new ones come back for review:\n got %v\nwant %v", back, want)
	}
	for _, task := range u.Tasks {
		if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Reviewed what changed since the last upgrade."}); err != nil {
			t.Fatal(err)
		}
	}
	if u, err = b.MemoryUpgrade(); err != nil || u.Status != "complete" || u.Reviewed != 5 {
		t.Fatalf("the upgrade completes again once they are reviewed: %+v %v", u, err)
	}
}

// With no checkpoint there is nothing to reopen and none is made. An upgrade still under review gains the article that is new and nothing
// else, and when no article is new its checkpoint is not written again.
func TestRefreshMemoryUpgradeMakesNoCheckpointAndOnlyAddsNewArticlesToAnUnfinishedOne(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	if _, err := b.Ingest(draft(seed(t, b, 1), "First article")); err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(memoryUpgradePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("no checkpoint is made by a refresh: %v", err)
	}
	if _, err := b.MemoryUpgrade(); err != nil { // makes the checkpoint, with one target, not complete
		t.Fatal(err)
	}
	before, err := os.ReadFile(memoryUpgradePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	if same, err := os.ReadFile(memoryUpgradePath(dir)); err != nil || string(same) != string(before) {
		t.Fatalf("an upgrade that is not complete and has no new article is not written again:\n%s\n%s\n%v", before, same, err)
	}
	second, err := b.Ingest(draft(seed(t, b, 2), "Second article"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshMemoryUpgrade(); err != nil {
		t.Fatal(err)
	}
	c := upgradeCheckpointOf(t, b)
	if len(c.Targets) != 2 || c.Targets[second.Page.ID] != second.Page.Revision || len(c.Reviews) != 0 || c.Complete {
		t.Fatalf("an upgrade that is not complete gains the new article as a target and nothing else: %+v", c)
	}
}

// A count that is not finished by its deadline gives no count (a count of part of the sources would read as the whole one): the notice
// at a session start leaves itself for the next one. With no deadline, or one not reached, the count is the same as CountWorkResultsSince.
func TestCountWorkResultsUntilGivesNoCountPastItsDeadline(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	now := time.Now()
	for i := 1; i <= 3; i++ {
		saveResult(t, b, WorkResult(), i, now.Add(-time.Hour))
	}
	if n, err := b.CountWorkResultsUntil(time.Time{}, now.Add(-time.Second)); err != ErrWorkCountTooLong || n != 0 {
		t.Fatalf("a deadline already past: no count, and the error that says so: %d %v", n, err)
	}
	for _, deadline := range []time.Time{{}, now.Add(time.Hour)} {
		if n, err := b.CountWorkResultsUntil(time.Time{}, deadline); err != nil || n != 3 {
			t.Fatalf("with time to spare the count is the whole one: %d %v", n, err)
		}
	}
	empty := Book{Root: t.TempDir(), Scope: "/project"}
	if n, err := empty.CountWorkResultsUntil(time.Time{}, now.Add(-time.Second)); err != nil || n != 0 {
		t.Fatalf("no sources, nothing left to count: %d %v", n, err)
	}
}
