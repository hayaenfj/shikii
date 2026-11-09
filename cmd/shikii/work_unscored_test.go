package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func round4DeliveryConversation(t *testing.T, request string) (notebook.Book, map[string]any, store.Feedback) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": request})
	hookLocalURL = "http://127.0.0.1:1"
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Synthetic comparison completed."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Keep the report local and explain the result."})
	st := store.Store{Dir: root}
	recent, err := st.RecentFeedback(feedbackScope(repo))
	if err != nil || len(recent) != 1 {
		t.Fatal("fixture", recent, err)
	}
	if len(recent[0].Targets) != 0 {
		t.Fatal("fixture unexpectedly scored", recent[0])
	}
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "local formats"})
	if err != nil {
		t.Fatal(err)
	}
	return b, prepared, recent[0]
}

func TestRound4DeliveryUnscoredLongConversation(t *testing.T) {
	const marker = "AUDIT_R4_TAIL_CONDITION never publish"
	request := "Compare the local formats. " + strings.Repeat("x", 5100) + marker
	b, prepared, feedback := round4DeliveryConversation(t, request)
	pending := prepared["pending"].([]any)
	if len(pending) != 1 {
		t.Fatal("fixture", pending)
	}
	source := pending[0].(map[string]any)
	raw, _ := json.Marshal(source)
	t.Logf("original_bytes=%d stored_feedback_bytes=%d targets=%d marker_delivered=%t incomplete=%v ref=%v source_query=%v", len(request), len(feedback.Request), len(feedback.Targets), strings.Contains(string(raw), marker), source["incomplete"], source["ref"], source["source_query"])
	if source["ref"] != nil {
		saved, e := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "reviewed": []string{source["ref"].(string)}})
		if e != nil {
			t.Fatal(e)
		}
		left, e := b.PendingSources()
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("reviewed_complete=%v remaining_sources=%d", saved["complete"], len(left))
	}
	if !strings.Contains(string(raw), marker) && (source["incomplete"] != true || source["ref"] != nil) {
		t.Fatal("missing original tail was exposed as complete writable material")
	}

	if source["ref"] == nil {
		blocked, e := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "reviewed": []string{"source1"}})
		if e == nil && blocked["complete"] == true {
			t.Fatal("an incomplete source must not gain a writable alias", blocked)
		}
		left, e := b.PendingSources()
		if e != nil || len(left) != 1 {
			t.Fatal("the incomplete conversation must remain pending", left, e)
		}
	}
	t.Run("short control", workUnscoredShortConversationControl)
}

func workUnscoredShortConversationControl(t *testing.T) {
	const request = "Compare local formats and keep the report local."
	b, prepared, _ := round4DeliveryConversation(t, request)
	source := prepared["pending"].([]any)[0].(map[string]any)
	raw, _ := json.Marshal(source)
	if !strings.Contains(string(raw), request) || source["ref"] == nil {
		t.Fatal("complete short source missing", source)
	}
	saved, e := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "reviewed": []string{source["ref"].(string)}})
	if e != nil || saved["complete"] != true {
		t.Fatal(saved, e)
	}
	t.Log("short source retained and reviewed successfully")
}
