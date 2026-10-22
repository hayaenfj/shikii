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
		t.Fatal("意味の見直しを最大2件、版と依存する版付きで提示する", string(data))
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
		t.Fatal("同じ版を毎回見直さない")
	}
	d.ID = z.Page.ID
	d.Lesson += " Keep the current version."
	d.ChangeReason = "The inspected result changes applicability."
	updated, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Page.ChangeReason != d.ChangeReason {
		t.Fatal("編集の理由を版とともに残す")
	}
	if _, err := b.Maintain(stale); err == nil {
		t.Fatal("関連する版が変わった後に古い見直しを記録しない")
	}
	tasks, err = b.Maintenance()
	if err != nil || len(tasks) != 2 {
		t.Fatal("変更した記事と参照先を再確認する", tasks, err)
	}
	for _, task := range tasks {
		if !strings.Contains(strings.Join(task.Reasons, ","), "changed") {
			t.Fatal("変更による再確認と分かる")
		}
	}
}
