package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.12 item 3: one reflection is one round trip. The query lists every pending conversation (the raw text of the oldest two, the rest
// as heads with their deliveries to assess), how_to_submit says where the deliveries to assess are, a maintenance review survives the
// agent's own save of that page in the same submit, and what is left names the items, not only a count. Measured before: a reflection
// of 1 conversation and 17 deliveries took 10 round trips; 33 of 156 reflections were done only after a stop.

// pendingConversations makes n completed conversations in one project, each with a lesson delivered before the request (so each waits
// for the reflection's assessment).
func pendingConversations(t *testing.T, st store.Store, scope string, n int) []string {
	t.Helper()
	var turns []string
	for i := 0; i < n; i++ {
		session := "conv" + string(rune('a'+i))
		memoryEffectPrompt(t, scope, session)
		turns = append(turns, loadSession(st.Dir, session).TurnID)
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": scope, "last_assistant_message": "Updated the README install steps and checked an uninitialized folder records nothing."})
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "now adjust the colours of the page"})
	}
	return turns
}

func TestReflectQueryListsEveryPendingConversationWithTheRawTextOfTheOldestTwo(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	pendingConversations(t, st, scope, 3)
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	sources, _ := q["sources"].([]any)
	if len(sources) < 3 {
		t.Fatalf("every pending conversation is listed (got %d): %v", len(sources), q["sources"])
	}
	for i, s := range sources {
		src := s.(map[string]any)
		material, _ := src["source_material"].(map[string]any)
		memories, _ := src["memories"].([]any)
		head, _ := src["head"].(bool)
		if i < 2 && (head || len(material["request"].(string)) == 0) {
			t.Fatalf("the oldest two carry their raw text: %v", src)
		}
		if i >= 2 && !head {
			t.Fatalf("the rest are heads (head: true), their deliveries still listed: %v", src)
		}
		targets, _ := src["targets"].([]any)
		if len(memories) == 0 && len(targets) == 0 && src["wiki_pending"] != true {
			t.Fatalf("each listed conversation carries what is left to do on it (deliveries to assess, scores or wiki): %v", src)
		}
	}
	how, _ := q["how_to_submit"].(string)
	if !strings.Contains(how, "memories") || !strings.Contains(how, "head") {
		t.Fatalf("how_to_submit says where the deliveries to assess are and what a head is:\n%s", how)
	}
}

func TestAMaintenanceReviewSurvivesTheAgentsOwnSaveInTheSameSubmit(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	tasks, _ := q["maintenance"].([]any)
	var task map[string]any
	for _, x := range tasks {
		if m := x.(map[string]any); m["page_id"] == draft.ID {
			task = m
		}
	}
	if task == nil {
		t.Fatalf("precondition: the fixture page waits for its first review: %v", tasks)
	}
	body := "## Note\nThe check also covers an uninitialized folder."
	result := reflectCall(t, h, map[string]any{"operation": "submit",
		"wiki":        []map[string]any{{"id": draft.ID, "title": draft.Title, "lesson": draft.Lesson, "applies_to": draft.AppliesTo, "kind": draft.Kind, "scene": draft.Scene, "sources": draft.Sources, "body": body}},
		"maintenance": []map[string]any{{"page_id": draft.ID, "revision": task["revision"], "key": task["key"], "reason": "read the body and the sources; added the note"}},
	})
	receipts, _ := result["maintenance"].([]any)
	if len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil || result["complete"] != true {
		t.Fatalf("the review of the page the same submit updated is recorded (the agent read it, then changed it): %v", result)
	}
	page, err := (notebook.Book{Root: st.Dir, Scope: scope}).Query("lesson:" + draft.ID)
	if err != nil || len(page.Lessons) != 1 || !strings.Contains(page.Lessons[0].Body, "uninitialized folder") {
		t.Fatalf("the update itself was saved: %v %v", err, page)
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	after, _ := q["maintenance"].([]any) // absent when nothing waits
	for _, x := range after {
		if x.(map[string]any)["page_id"] == draft.ID {
			t.Fatalf("the page no longer waits for a review: %v", x)
		}
	}
}

func TestWhatIsLeftNamesTheDeliveriesToAssess(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	turn := pendingConversations(t, st, scope, 1)[0]
	result := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{turn}}}})
	left, _ := result["left_memory"].([]any)
	if len(left) != 1 {
		t.Fatalf("left names the deliveries still to assess as items: %v", result)
	}
	item := left[0].(map[string]any)
	if item["source_id"] != turn || item["key"] != "lesson:"+draft.ID+":"+revision {
		t.Fatalf("each item is the source_id and key to send in memory: %v", item)
	}
}
