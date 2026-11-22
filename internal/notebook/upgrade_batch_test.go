package notebook

import (
	"fmt"
	"testing"
)

// A review of the memory upgrade takes ten pages at a time (it was two). With more than ten waiting, the first call returns ten of
// them and the rest wait for the next call. The count is spelled as a literal here, beside the constant, so that a change of the
// constant has to change this test too.
func TestMemoryUpgradeBatchHoldsTenPagesAndTheEleventhWaitsForTheNextCall(t *testing.T) {
	if MemoryUpgradeBatch != 10 {
		t.Errorf("the batch size is ten pages: %d", MemoryUpgradeBatch)
	}
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i := 1; i <= 11; i++ {
		if _, err := b.Ingest(draft(seed(t, b, i), fmt.Sprintf("Upgrade rule %02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Total != 11 || u.Reviewed != 0 {
		t.Fatal("eleven waiting pages enter the upgrade", u, err)
	}
	if len(u.Tasks) != 10 {
		t.Fatalf("one call returns ten pages when eleven wait, got %d", len(u.Tasks))
	}
	first := map[string]bool{}
	for _, task := range u.Tasks {
		if first[task.PageID] {
			t.Fatal("a page appears once in a batch", task.PageID)
		}
		first[task.PageID] = true
	}
	// Reviewing nine of the ten leaves the tenth in the batch, and the eleventh joins it: the batch refills from what is not reviewed.
	for _, task := range u.Tasks[:9] {
		if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked the sources and current behavior."}); err != nil {
			t.Fatal(err)
		}
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 9 || len(u.Tasks) != 2 {
		t.Fatalf("nine reviewed, the tenth and the eleventh remain: reviewed %d, %d tasks, %v", u.Reviewed, len(u.Tasks), err)
	}
	for _, task := range u.Tasks {
		if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked the remaining pages."}); err != nil {
			t.Fatal(err)
		}
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status != "complete" || u.Reviewed != 11 || len(u.Tasks) != 0 {
		t.Fatal("complete once every page is reviewed", u, err)
	}
}

// The eleventh page is the next call's, not the first's: review the whole first batch, then the next call holds exactly the page left.
func TestMemoryUpgradeNextCallHoldsOnlyThePagesLeftAfterAFullBatch(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i := 1; i <= 11; i++ {
		if _, err := b.Ingest(draft(seed(t, b, i), fmt.Sprintf("Upgrade rule %02d", i))); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.MemoryUpgrade()
	if err != nil || len(u.Tasks) != 10 {
		t.Fatalf("the first call returns ten pages: %d tasks, %v", len(u.Tasks), err)
	}
	first := map[string]bool{}
	for _, task := range u.Tasks {
		first[task.PageID] = true
		if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked the sources and current behavior."}); err != nil {
			t.Fatal(err)
		}
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 10 || len(u.Tasks) != 1 || first[u.Tasks[0].PageID] {
		t.Fatalf("the next call holds the one page left, not one reviewed: reviewed %d, %d tasks, %v", u.Reviewed, len(u.Tasks), err)
	}
}
