package notebook

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotebookOffersBoundedMaintenanceWork(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for _, title := range []string{"Storage", "Retrieval", "Evaluation"} {
		if _, err := b.Ingest(draft(seed(t, b, len(title)), title)); err != nil {
			t.Fatal(err)
		}
	}
	v, err := b.Query("")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(v)
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(data, &obj)
	var tasks []map[string]any
	_ = json.Unmarshal(obj["maintenance"], &tasks)
	if len(tasks) != 2 || tasks[0]["key"] == nil || tasks[0]["revision"] == nil {
		t.Fatal("offers at most 2 meaning reviews, with revision and dependent revisions", string(data))
	}
}

func TestMaintenanceTracksRelatedVersionsAndRejectsStaleReviews(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	a, err := b.Ingest(draft(source, "Storage"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(source, "Retrieval")
	d.Related = []string{a.Page.ID}
	z, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := b.Maintenance()
	if err != nil || len(tasks) != 2 {
		t.Fatal(tasks, err)
	}
	var stale MaintenanceReview
	for _, task := range tasks {
		r := MaintenanceReview{task.PageID, task.Revision, task.Key, "Reviewed sources and related topic; no change needed."}
		if _, err := b.Maintain(r); err != nil {
			t.Fatal(err)
		}
		if task.PageID == a.Page.ID {
			stale = r
		}
	}
	if tasks, _ := b.Maintenance(); len(tasks) != 0 {
		t.Fatal("does not review the same revision every time")
	}
	d.ID = z.Page.ID
	d.Lesson += " Keep the current version."
	d.ChangeReason = "The inspected result changes applicability."
	updated, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Page.ChangeReason != d.ChangeReason {
		t.Fatal("keeps the reason for an edit with the revision")
	}
	if _, err := b.Maintain(stale); err == nil {
		t.Fatal("does not record an old review after a related revision changed")
	}
	tasks, err = b.Maintenance()
	if err != nil || len(tasks) != 2 {
		t.Fatal("rechecks the changed page and the pages it references", tasks, err)
	}
	for _, task := range tasks {
		if !strings.Contains(strings.Join(task.Reasons, ","), "changed") {
			t.Fatal("shows that the recheck is due to a change")
		}
	}
}
