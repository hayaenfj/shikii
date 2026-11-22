package notebook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryUpgradeReviewsEveryExistingPageAndResumes(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i, title := range []string{"Old review rule", "Duplicate procedure", "Valid project knowledge"} {
		if _, err := b.Ingest(draft(seed(t, b, i+1), title)); err != nil {
			t.Fatal(err)
		}
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Total != 3 || u.Reviewed != 0 || len(u.Tasks) != 3 {
		t.Fatal("all existing knowledge must enter the upgrade work (three pages fit one batch)", u, err)
	}
	if u.Message != "\u8a18\u61b6\u3092\u66f4\u65b0\u3057\u3066\u3044\u307e\u3059\u2026" {
		t.Fatal("the progress message the person sees must be Japanese", u.Message)
	}
	first := u.Tasks[0]
	if _, err := b.Maintain(MaintenanceReview{first.PageID, first.Revision, first.Key, "Checked current policy and original evidence; retained applicable knowledge."}); err != nil {
		t.Fatal(err)
	}
	resumed := Book{Root: b.Root, Scope: b.Scope}
	u, err = resumed.MemoryUpgrade()
	if err != nil || u.Reviewed != 1 || len(u.Tasks) != 2 {
		t.Fatal("resume from durable progress: the two pages not reviewed remain", u, err)
	}
	for _, task := range u.Tasks {
		if _, err := resumed.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Reviewed the sources and current policy."}); err != nil {
			t.Fatal(err)
		}
	}
	u, err = resumed.MemoryUpgrade()
	if err != nil || u.Status != "complete" || u.Reviewed != 3 || len(u.Tasks) != 0 {
		t.Fatal("complete only after every target was reviewed", u, err)
	}
	if _, err := b.Ingest(draft(seed(t, b, 9), "New current-version knowledge")); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status != "complete" || u.Total != 3 {
		t.Fatal("ordinary future writes must not restart the completed upgrade", u, err)
	}
}

func TestMemoryUpgradeRejectsStaleReviewsAndKeepsOriginalSources(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	d := draft(source, "Old review rule")
	p, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil || len(u.Tasks) != 1 {
		t.Fatal(u, err)
	}
	old := u.Tasks[0]
	d.ID = p.Page.ID
	d.Lesson += " Updated conditions."
	if _, err = b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Maintain(MaintenanceReview{old.PageID, old.Revision, old.Key, "Old review."}); err == nil {
		t.Fatal("a stale review cannot complete an upgrade")
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 0 || u.Tasks[0].Revision == old.Revision {
		t.Fatal("return the current revision", u, err)
	}
	v, err := b.ReadSources([]string{source})
	if err != nil || v == nil {
		t.Fatal("original evidence must remain available", err)
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, ".memory-upgrade-"+MemoryUpgradeEpoch+".json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.json"), file); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MemoryUpgrade(); err == nil {
		t.Fatal("upgrade metadata must not follow a symlink")
	}
}

func TestMemoryUpgradeRetainsBlockedSourcesAndRechecksDependencies(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	first, err := b.Ingest(draft(seed(t, b, 1), "Current procedure"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(seed(t, b, 2), "Related conditions")
	d.Related = []string{first.Page.ID}
	second, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range u.Tasks {
		if task.PageID == first.Page.ID {
			if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked current and related evidence."}); err != nil {
				t.Fatal(err)
			}
		}
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 1 {
		t.Fatal(u, err)
	}
	d.ID, d.Lesson = second.Page.ID, d.Lesson+" The prerequisite changed."
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 0 {
		t.Fatal("changed dependencies invalidate an in-progress review", u, err)
	}
	sources, _ := b.directory(false, "sources")
	path := filepath.Join(sources, first.Page.Sources[0].ID+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status == "complete" || len(u.Blocked) != 1 {
		t.Fatal("an unavailable source cannot be silently discarded", u, err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || len(u.Blocked) != 0 || len(u.Tasks) != 2 {
		t.Fatal("recover without changing the original source", u, err)
	}
}

// The completion message is shown to the person, so it is Japanese, with and without deleted targets.
func TestUpgradeCompletionMessageIsJapanese(t *testing.T) {
	if got := upgradeCompletionMessage(MemoryUpgrade{Reviewed: 2}); got != "\u8a18\u61b6\u306e\u66f4\u65b0\u304c\u5b8c\u4e86\u3057\u307e\u3057\u305f\u3002" {
		t.Fatal("completion without deleted targets must be Japanese:", got)
	}
	if got := upgradeCompletionMessage(MemoryUpgrade{Withdrawn: 1, Reviewed: 2}); got != "\u8a18\u61b6\u306e\u66f4\u65b0\u304c\u5b8c\u4e86\u3057\u307e\u3057\u305f\u3002\u524a\u9664\u5bfe\u8c61\u3092\u9664\u5916: 1 \u4ef6\u3001\u898b\u76f4\u3057: 2 \u4ef6\u3002" {
		t.Fatal("completion with deleted targets must be Japanese:", got)
	}
}
