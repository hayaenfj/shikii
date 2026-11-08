package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

func relatedReviewBook(t *testing.T) (Book, Page, Draft) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	a, err := b.Ingest(draft(source, "Storage condition"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(source, "Related retrieval condition")
	d.Related = []string{a.Page.ID}
	body := "Keep this exact condition, including its exceptions."
	d.Body = &body
	z, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = z.Page.ID
	return b, *a.Page, d
}

func reviewRelatedTasks(t *testing.T, b Book, tasks []MaintenanceTask) {
	t.Helper()
	for _, task := range tasks {
		if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Checked exact content, conditions and original sources."}); err != nil {
			t.Fatal(err)
		}
	}
}

func restoreRelatedContent(t *testing.T, b Book, d Draft) Page {
	t.Helper()
	changed := "An intermediate condition that must still be reviewed when present."
	edit := d
	edit.Body = &changed
	if _, err := b.Ingest(edit); err != nil {
		t.Fatal(err)
	}
	p, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	return *p.Page
}

func TestRelatedMaintenanceContentReuse(t *testing.T) {
	b, a, d := relatedReviewBook(t)
	tasks, err := b.Maintenance()
	if err != nil || len(tasks) != 2 {
		t.Fatal(tasks, err)
	}
	reviewRelatedTasks(t, b, tasks)
	restored := restoreRelatedContent(t, b, d)
	pending, err := b.Maintenance()
	if err != nil || len(pending) != 1 || pending[0].PageID != d.ID {
		t.Fatal("only the edited page needs a review when its related content is exactly restored", pending, err)
	}
	keys, err := b.MaintenanceKeys()
	if err != nil || keys[a.ID].Dependencies[d.ID] != restored.Revision {
		t.Fatal("dependency metadata must retain current revisions", keys, err)
	}
	for _, old := range tasks {
		if old.PageID == a.ID {
			if old.Key == keys[a.ID].Key {
				t.Fatal("the concurrency key must still change")
			}
			if _, err := b.Maintain(MaintenanceReview{old.PageID, old.Revision, old.Key, "Stale review."}); err == nil {
				t.Fatal("content reuse must not accept a stale submission")
			}
		}
	}
	// A genuinely new source must reopen the neighbor even if its prose is unchanged.
	d.Sources = append(d.Sources, seed(t, b, 2))
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	pending, err = b.Maintenance()
	if err != nil || len(pending) != 2 {
		t.Fatal("new evidence must reopen both reviews", pending, err)
	}
}

func TestRelatedMaintenancePreservesAllContent(t *testing.T) {
	base := Page{ID: "0000000000000001", Revision: "0000000000000002", Title: "Condition", Lesson: "Keep original evidence.", AppliesTo: "Related work", Status: "hypothesis", Kind: "rule", Scene: "when reviewing", Body: "Exact original text.", Sources: []Source{{ID: "0000000000000003", Hash: "source-a", TextHash: "text-a"}}}
	neighbor := Page{ID: "0000000000000004", Revision: "0000000000000005", Related: []string{base.ID}}
	key := func(p Page) string {
		for _, task := range maintenanceTasks([]Page{p, neighbor}) {
			if task.PageID == neighbor.ID {
				return task.BasisKey
			}
		}
		t.Fatal("missing neighbor")
		return ""
	}
	original := key(base)
	for name, change := range map[string]func(*Page){
		"title":      func(p *Page) { p.Title += " changed" },
		"lesson":     func(p *Page) { p.Lesson += " changed" },
		"conditions": func(p *Page) { p.AppliesTo += " changed" },
		"body":       func(p *Page) { p.Body += " changed" },
		"reason":     func(p *Page) { p.ChangeReason = "New explanation" },
		"rationale":  func(p *Page) { p.Rationale = "New rationale" },
		"source":     func(p *Page) { p.Sources = []Source{{ID: base.Sources[0].ID, Hash: "source-b", TextHash: "text-b"}} },
		"correction": func(p *Page) {
			p.Correction = &store.Correction{SourceID: base.Sources[0].ID, Quote: "Keep the exception.", Action: "Retain it."}
		},
		"relation": func(p *Page) { p.Related = []string{neighbor.ID} },
		"status":   func(p *Page) { p.Status = "deleted" },
		"kind":     func(p *Page) { p.Kind = "history" },
		"scene":    func(p *Page) { p.Scene = "when testing" },
		"premise":  func(p *Page) { p.Premises = []Premise{{Path: "context.txt", Hash: "new-baseline"}} },
		"passages": func(p *Page) {
			p.Passages = []KnowledgePassage{{Role: "counterexample", Quote: p.Body, Conditions: "Exception", Reason: "Preserve this exception."}}
		},
		"learning": func(p *Page) { p.Learning = &Learning{Probe: &KnowledgeProbe{Question: "Which condition changed?"}} },
		"replaces": func(p *Page) { p.Replaces = []string{"0000000000000007"} },
	} {
		t.Run(name, func(t *testing.T) {
			p := base
			change(&p)
			if key(p) == original {
				t.Fatal("changed meaning, provenance or applicability must not reuse review")
			}
		})
	}
	p := base
	p.Revision = "0000000000000006"
	p.Previous = base.Revision
	if key(p) != original {
		t.Fatal("only version lineage may be ignored for related content")
	}
}

func TestMemoryUpgradeReusesRelatedContent(t *testing.T) {
	b, a, d := relatedReviewBook(t)
	u, err := b.MemoryUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range u.Tasks {
		if task.PageID == a.ID {
			reviewRelatedTasks(t, b, []MaintenanceTask{task})
		}
	}
	restoreRelatedContent(t, b, d)
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 1 || len(u.Tasks) != 1 || u.Tasks[0].PageID != d.ID {
		t.Fatal("retain actual reviewed progress for identical related content", u, err)
	}
	d.Sources = append(d.Sources, seed(t, b, 2))
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Reviewed != 0 || len(u.Tasks) != 2 {
		t.Fatal("new related evidence must invalidate reused upgrade progress", u, err)
	}
}

func TestMaintenanceLegacyContentCheckpoints(t *testing.T) {
	b, _, d := relatedReviewBook(t)
	tasks, err := b.Maintenance()
	if err != nil {
		t.Fatal(err)
	}
	reviewRelatedTasks(t, b, tasks)
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".maintenance.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]map[string]any
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	for _, checkpoint := range old {
		delete(checkpoint, "basis_key")
	}
	data, _ = json.Marshal(old)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if pending, err := b.Maintenance(); err != nil || len(pending) != 0 {
		t.Fatal("unchanged legacy reviews must stay valid", pending, err)
	}
	restoreRelatedContent(t, b, d)
	if pending, err := b.Maintenance(); err != nil || len(pending) != 2 {
		t.Fatal("legacy records without comparison evidence must conservatively recheck", pending, err)
	}
}
