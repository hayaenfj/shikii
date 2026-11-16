package notebook

import (
	"slices"
	"testing"
)

// A page whose original evidence cannot be read holds the batch today: it is the first unreviewed task, so prepare keeps returning it
// and the next page never comes (2026-11-16, 29 of 103 reviewed). The agent declares it blocked at its current revision with a reason;
// the batch moves on, the page is reported by its id and does not count as reviewed, and it returns to review once its revision changes.
func TestMemoryUpgradeSkipsAPageDeclaredBlocked(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i, title := range []string{"Blocked rule", "Second rule", "Third rule"} {
		if _, err := b.Ingest(draft(seed(t, b, i+1), title)); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Total != 3 || len(u.Tasks) != 2 {
		t.Fatal("start with two bounded tasks", u, err)
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
	if len(u.Tasks) != 2 || slices.ContainsFunc(u.Tasks, func(task MaintenanceTask) bool { return task.PageID == blocked.PageID }) {
		t.Fatal("the batch must move on past the blocked page to the next unreviewed pages", u.Tasks)
	}

	resumed := Book{Root: b.Root, Scope: b.Scope}
	for _, task := range u.Tasks {
		if _, err := resumed.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Reviewed the sources and current policy."}); err != nil {
			t.Fatal(err)
		}
	}
	u, err = resumed.MemoryUpgrade()
	if err != nil || u.Status != "blocked" || u.Reviewed != 2 || len(u.Tasks) != 0 || !slices.Contains(u.Blocked, blocked.PageID) {
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
