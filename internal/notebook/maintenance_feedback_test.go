package notebook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

func feedbackPage(t *testing.T, b Book) Page {
	t.Helper()
	r, err := b.Ingest(draft(seed(t, b, 1), "Installation instructions"))
	if err != nil {
		t.Fatal(err)
	}
	reviewFeedbackPage(t, b, r.Page.ID)
	return *r.Page
}

func reviewFeedbackPage(t *testing.T, b Book, id string) MaintenanceTask {
	t.Helper()
	keys, err := b.MaintenanceKeys()
	if err != nil {
		t.Fatal(err)
	}
	task := keys[id]
	if _, err := b.Maintain(MaintenanceReview{task.PageID, task.Revision, task.Key, "Read the sources and applicability; no supported edit."}); err != nil {
		t.Fatal(err)
	}
	return task
}

func assessLesson(t *testing.T, b Book, p Page, n int, phrase, result string) {
	t.Helper()
	st := store.Store{Dir: b.Root}
	id := seed(t, b, n)
	key := "lesson:" + p.ID + ":" + p.Revision
	d := store.MemoryDelivery{ID: id, Scope: b.Scope, Request: "Update the installation guide", Phrase: phrase,
		Quotes: []store.MemoryQuote{{Key: key, ID: p.ID, Revision: p.Revision, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}}}}
	if err := st.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMemoryAssessment(b.Scope, store.MemoryAssessment{SourceID: id, Key: key, Result: result,
		Decision: "Changed the documentation only", Evidence: fmt.Sprintf("Inspection %d: the lesson described a binary release", n)}); err != nil {
		t.Fatal(err)
	}
}

func feedbackTasks(t *testing.T, b Book) []MaintenanceTask {
	t.Helper()
	tasks, err := b.Maintenance()
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestMaintenanceReopensLessonsFromActualDeliveryFeedback(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	p := feedbackPage(t, b)
	old := reviewFeedbackPage(t, b, p.ID)
	assessLesson(t, b, p, 10, "git diff", "unused")
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatal("one unused delivery is not a pattern", tasks)
	}
	assessLesson(t, b, p, 11, "git diff", "unused")
	tasks := feedbackTasks(t, b)
	if len(tasks) != 1 || tasks[0].PageID != p.ID || !slices.Contains(tasks[0].Reasons, "delivery-feedback") {
		t.Fatal("repeated unused deliveries reopen the reviewed lesson", tasks)
	}
	if tasks[0].Key == old.Key {
		t.Fatal("the review must bind the new evidence")
	}
	if _, err := b.Maintain(MaintenanceReview{old.PageID, old.Revision, old.Key, "old evidence"}); err == nil {
		t.Fatal("a review made before the new evidence must be refused")
	}
	data, _ := json.Marshal(tasks[0])
	var body struct {
		Feedback []store.MemoryAssessment `json:"feedback"`
	}
	if err := json.Unmarshal(data, &body); err != nil || len(body.Feedback) != 2 || body.Feedback[0].Evidence == "" || body.Feedback[0].Decision == "" {
		t.Fatal("the agent receives the decision and evidence, not just a count", string(data), err)
	}
	reviewFeedbackPage(t, b, p.ID)
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatal("reviewing without rewriting also settles the evidence", tasks)
	}
	assessLesson(t, b, p, 12, "git diff", "unknown")
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatal("unknown adds no negative evidence", tasks)
	}
	assessLesson(t, b, p, 13, "git diff", "unused")
	if tasks := feedbackTasks(t, b); len(tasks) != 1 {
		t.Fatal("new evidence can reopen the lesson", tasks)
	}
	reviewFeedbackPage(t, b, p.ID)
	assessLesson(t, b, p, 14, "git diff", "helpful")
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatal("helpful evidence alone does not reopen a settled review", tasks)
	}
}

func TestMaintenanceFeedbackSeparatesDoorsResultsAndVersions(t *testing.T) {
	for _, tc := range []struct {
		name           string
		results, doors []string
		want           bool
	}{
		{"different doors", []string{"unused", "unused"}, []string{"git diff", "go test"}, false},
		{"unknown", []string{"unknown", "unknown"}, []string{"git diff", "git diff"}, false},
		{"helpful at this door", []string{"unused", "unused", "helpful"}, []string{"git diff", "git diff", "git diff"}, false},
		{"helpful elsewhere", []string{"unused", "unused", "helpful"}, []string{"git diff", "git diff", "go test"}, true},
		{"harmful", []string{"helpful", "harmful"}, []string{"git diff", "git diff"}, true},
		{"no recorded door", []string{"unused", "unused"}, []string{"", ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/project"}
			p := feedbackPage(t, b)
			for i, result := range tc.results {
				assessLesson(t, b, p, i+10, tc.doors[i], result)
			}
			if got := len(feedbackTasks(t, b)) > 0; got != tc.want {
				t.Fatalf("review=%v, want %v", got, tc.want)
			}
		})
	}
	b := Book{Root: t.TempDir(), Scope: "/project"}
	p := feedbackPage(t, b)
	assessLesson(t, b, p, 10, "git diff", "harmful")
	d := draft(p.Sources[0].ID, p.Title)
	d.ID, d.Lesson, d.ChangeReason = p.ID, p.Lesson+" Only when releasing a binary.", "Narrowed applicability from inspected evidence."
	r, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	reviewFeedbackPage(t, b, r.Page.ID)
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatal("old revision feedback does not condemn the new revision", tasks)
	}
}

func TestMaintenanceFeedbackSurvivesDeliveryHistoryAndStaysBounded(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	p := feedbackPage(t, b)
	assessLesson(t, b, p, 9, "git diff", "harmful")
	for i := 10; i < 15; i++ {
		assessLesson(t, b, p, i, "git diff", "unused")
	}
	before := feedbackTasks(t, b)
	files, err := filepath.Glob(filepath.Join(b.Root, "records", "memories", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	if err := os.WriteFile(files[0], nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"Unreviewed first", "Unreviewed second"} {
		if _, err := b.Ingest(draft(seed(t, b, i+100), title)); err != nil {
			t.Fatal(err)
		}
	}
	tasks := feedbackTasks(t, b)
	if len(tasks) != 2 || tasks[0].PageID != p.ID || tasks[0].Key != before[0].Key {
		t.Fatal("feedback survives a rotated delivery log and is offered before routine reviews", tasks)
	}
	data, _ := json.Marshal(tasks[0])
	var body struct {
		Feedback []store.MemoryAssessment `json:"feedback"`
		Total    int                      `json:"feedback_total"`
	}
	_ = json.Unmarshal(data, &body)
	if len(body.Feedback) != 3 || body.Total != 6 || body.Feedback[0].Result != "harmful" {
		t.Fatal("bound examples while reporting all evidence", string(data))
	}
}

func TestHelpfulFeedbackDoesNotReopenReviewedMixedEvidence(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	p := feedbackPage(t, b)
	assessLesson(t, b, p, 10, "git diff", "harmful")
	assessLesson(t, b, p, 11, "git diff", "unused")
	assessLesson(t, b, p, 12, "git diff", "unused")
	reviewFeedbackPage(t, b, p.ID)
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatal("settled review remains pending", tasks)
	}
	assessLesson(t, b, p, 13, "git diff", "helpful")
	if tasks := feedbackTasks(t, b); len(tasks) != 0 {
		t.Fatalf("helpful alone reopened %d task(s), feedback_total=%d, reasons=%v", len(tasks), tasks[0].FeedbackTotal, tasks[0].Reasons)
	}
	assessLesson(t, b, p, 14, "git diff", "harmful")
	if tasks := feedbackTasks(t, b); len(tasks) != 1 {
		t.Fatal("a new harmful assessment still reopens the lesson", tasks)
	}
}
