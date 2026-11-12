package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestAuditDeliveryHistoryKeepsSpeaker(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "HUMAN_FIRST compare export formats before choosing."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "ASSISTANT_FIRST I propose public export."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "HUMAN_SECOND compare the proposed formats only."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "ASSISTANT_SECOND I propose publishing."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "HUMAN_CURRENT continue comparing the original formats."})
	turn := loadSession(root, "planning").TurnID
	direct, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": turn, "query": "formats"})
	if err != nil {
		t.Fatal(err)
	}
	materials := direct["source_material"].(map[string]any)
	control := map[string]any{}
	for key, value := range materials {
		if strings.HasPrefix(key, "history:") {
			control[key] = value
		}
	}
	t.Logf("control=%s", mustJSONAuditRound2(control))
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	got, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "formats"})
	if err != nil {
		t.Fatal(err)
	}
	conversation := got["conversation"].(map[string]any)
	t.Logf("prepared=%s", mustJSONAuditRound2(conversation))
	sawAssistant := false
	assistantRef := ""
	for ref, value := range conversation {
		if !strings.HasPrefix(ref, "history") {
			continue
		}
		text, ok := value.(string)
		if ok && strings.Contains(text, "ASSISTANT_FIRST") {
			sawAssistant = true
			assistantRef = ref
			metadata, _ := got["conversation_context"].(map[string]any)
			entry, _ := metadata[ref].(map[string]any)
			if entry["role"] != "assistant" || entry["turn_id"] == nil || entry["order_known"] != true || entry["source_key"] != "history:"+entry["turn_id"].(string)+":assistant" {
				t.Error("historical AI text must retain its speaker, turn and recorded order", entry)
			}
		}
	}
	if !sawAssistant {
		t.Fatal("\u5408\u6210\u6761\u4ef6\u306b\u904e\u53bb\u306eAI\u6587\u304c\u7121\u3044")
	}
	record, err := workCall(t, b, "record", map[string]any{"receipt": got["receipt"], "agreed": map[string]any{"summary": "Publish the export.", "quotes": []any{map[string]any{"ref": assistantRef, "text": "ASSISTANT_FIRST I propose public export."}}}})
	t.Logf("agreement_rejection=%s err=%v", mustJSONAuditRound2(record), err)
	if err == nil && record["complete"] == true {
		t.Fatal("\u904e\u53bb\u306eAI\u6587\u304c\u5408\u610f\u306b\u306a\u3063\u305f")
	}
	t.Run("recorded order and missing assistant", func(t *testing.T) {
		older, newer, legacy := "ffffffffffffffff", "0000000000000000", "legacy-turn"
		session := &hookSession{Planning: &planningFrame{Sources: map[string]string{
			"current_user": "Current request", "history:" + older + ":user": "Older human", "history:" + older + ":assistant": "Older AI", "history:" + newer + ":user": "Newer human without a reply", "history:" + legacy + ":assistant": "Legacy AI",
		}, Recent: []planningTurn{{ID: older, At: time.Now().Add(-time.Hour)}, {ID: newer, At: time.Now()}}}}
		state := &workState{Keys: map[string]string{}}
		texts, metadata := preparedWorkConversation(session, state, "current-turn")
		for i, want := range []string{"Older human", "Older AI", "Newer human without a reply"} {
			ref := "history" + strconv.Itoa(i+1)
			item := metadata[ref].(map[string]any)
			if texts[ref] != want || item["order"] != i || item["order_known"] != true {
				t.Fatal("history order must follow recorded turns, with user before assistant", texts, metadata)
			}
		}
		if metadata["history4"].(map[string]any)["order_known"] != false || metadata["history4"].(map[string]any)["turn_known"] != false {
			t.Fatal("legacy history must not invent chronology", metadata)
		}
	})
}

func TestAuditDeliveryCurrentStoredTextBoundary(t *testing.T) {
	repo, root, _ := planningFixture(t)
	const marker = "CURRENT_END_CONDITION never publish this report."
	prompt := "Compare the export formats. " + strings.Repeat("x", 4100) + marker
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt})
	s := loadSession(root, "planning")
	if !strings.Contains(s.RequestText, marker) {
		t.Fatal("\u4fdd\u5b58\u539f\u6587\u306b\u7d42\u7aef\u6761\u4ef6\u304c\u7121\u3044")
	}
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	got, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": s.TurnID, "query": "formats"})
	if err != nil {
		t.Fatal(err)
	}
	conversation := got["conversation"].(map[string]any)
	t.Logf("saved_request_bytes=%d prepared_current_bytes=%d saved_has_tail=%v prepared_has_tail=%v", len(s.RequestText), len(conversation["current"].(string)), strings.Contains(s.RequestText, marker), strings.Contains(got["task"].(string), marker))
	if !strings.Contains(got["task"].(string), marker) {
		t.Error("\u4fdd\u5b58\u6e08\u307f\u306e\u73fe\u4f9d\u983c\u306e\u7d42\u7aef\u6761\u4ef6\u304ctask\u306b\u7121\u304f\u4e0d\u8db3\u306e\u72b6\u614b\u3082\u7121\u3044")
	}
	metadata := got["task_context"].(map[string]any)
	if metadata["text_status"] != "stored_request" || metadata["role"] != "user" {
		t.Fatal("task text must retain its source and completeness", metadata)
	}
	t.Run("legacy and stored truncation remain explicit", func(t *testing.T) {
		state := &workState{Current: "Legacy planning excerpt"}
		text, info, e := preparedWorkTask(&hookSession{}, state, workRequest{})
		if e != nil || text != state.Current || info["text_status"] != "legacy_excerpt_only" || info["next_action"] == nil {
			t.Fatal(text, info, e)
		}
		text, info, e = preparedWorkTask(&hookSession{RequestText: strings.Repeat("x", (64<<10)+4)}, state, workRequest{})
		if e != nil || len(text) != 64<<10 || info["text_status"] != "stored_request_truncated" || info["next_action"] == nil {
			t.Fatal(len(text), info, e)
		}
	})
	t.Run("encoded task budget keeps prior receipt", func(t *testing.T) {
		token, e := workDecode(got["receipt"].(string))
		if e != nil {
			t.Fatal(e)
		}
		if e = updateSession(root, "planning", func(session *hookSession) { session.RequestText = strings.Repeat("<", 48<<10) }); e != nil {
			t.Fatal(e)
		}
		_, e = workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": s.TurnID, "query": "formats"})
		if e == nil || !strings.Contains(e.Error(), "exceed max_bytes") {
			t.Fatal("oversized encoded task must fail explicitly", e)
		}
		dir, e := openWorkSessions(root)
		if e != nil {
			t.Fatal(e)
		}
		defer dir.Close()
		if _, _, e = loadWorkState(dir, b.Scope, token); e != nil {
			t.Fatal("a failed preparation must retain the prior receipt", e)
		}
	})

}
