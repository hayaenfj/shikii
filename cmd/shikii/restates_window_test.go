package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 0.9.3 re-check (5ca36c5): the real sequence of the behaviour check has five turns between request 1 and its restatement
// ("continue", "OK", "let's go with that", "also check the totals", "write it in the README too"). restates must still reach request 1.
func TestRestatesReachesRequestSixTurnsBack(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "planning"
	turn, done := "", ""
	var group []string // done-as-asked of request 1 and of the acknowledgements attached to it
	prompts := []string{restateFirst, "\u7d9a\u3051\u3066", "OK\u3067\u3059", "\u305d\u308c\u3067\u3044\u3053\u3046", restateSecond, restateThird}
	for i, prompt := range prompts {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		if i > 0 {
			payload := map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": i <= 3}
			if _, err := planningCall(t, "submit", payload); err != nil {
				t.Fatalf("plan submit: %v", err)
			}
		}
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
		if i <= 3 {
			s := loadSession(root, session)
			if i == 0 {
				turn = s.TurnID
			}
			for _, r := range s.Reviews {
				if r.Spec == "done-as-asked" {
					if i == 0 {
						done = r.Trace
					}
					group = append(group, r.Trace)
				}
			}
		}
	}
	if turn == "" || done == "" {
		t.Fatal("request 1 must have a turn and a done-as-asked decision")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": restateAgain})
	out, err := planningCall(t, "query", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID})
	if err != nil {
		t.Fatalf("plan query: %v", err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), "history:"+turn+":user") {
		t.Errorf("plan query must still list request 1 (history:%s:user) so the agent can name it in restates; got %.600s", turn, raw)
	}
	restateSubmit(t, root, session, turn)
	found := false
	for _, tr := range group {
		if l, ok := doneLabel(t, root, tr); ok && l.Truth == "no" && l.Source == "outcome" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a restatement of request 1 five turns later, named by restates, must give request 1 (or the acknowledgements joined to it) \"no\": traces %v", group)
	}
}
