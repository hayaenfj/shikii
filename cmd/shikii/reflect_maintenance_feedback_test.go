package main

import (
	"fmt"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestReflectionReturnsLessonFeedbackForAgentMaintenance(t *testing.T) {
	st, scope, h, d, revision := memoryEffectFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	keys, err := b.MaintenanceKeys()
	if err != nil {
		t.Fatal(err)
	}
	task := keys[d.ID]
	if _, err := b.Maintain(notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Reason: "Read the existing lesson and its sources."}); err != nil {
		t.Fatal(err)
	}
	key := "lesson:" + d.ID + ":" + revision
	assessments := []map[string]any{}
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("%016x", 900+i)
		if err := st.AppendFeedback(scope, store.Feedback{ID: id, Request: "Update the README", Summary: "Updated the instructions", Reply: "Next, explain startup"}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: id, Scope: scope, Request: "Update the README", Phrase: "git diff",
			Quotes: []store.MemoryQuote{{Key: key, ID: d.ID, Revision: revision, Lesson: d.Lesson, AppliesTo: d.AppliesTo, Sources: d.Sources}}}); err != nil {
			t.Fatal(err)
		}
		assessments = append(assessments, map[string]any{"source_id": id, "key": key, "result": "unused", "decision": "Changed documentation", "evidence": "The lesson applied only to installing a binary"})
	}
	result := reflectCall(t, h, map[string]any{"operation": "submit", "memory": assessments})
	if result["complete"] != true {
		t.Fatal(result)
	}
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	tasks, _ := q["maintenance"].([]any)
	for _, raw := range tasks {
		x := raw.(map[string]any)
		if x["page_id"] == d.ID {
			if feedback, ok := x["feedback"].([]any); !ok || len(feedback) != 2 {
				t.Fatal("the usual reflection carries the assessment evidence", x)
			}
			return
		}
	}
	t.Fatal("the agent's own saved assessments reopen the lesson without a new human answer", q["maintenance"])
}
