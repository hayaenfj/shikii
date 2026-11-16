package main

import "testing"

// A go-ahead that asks nothing new (continues_previous: true) is not a new request for the needs-plan judgment: the decision model is not
// called for it, so the question about a decision is not asked. A new request (continues_previous: false) with an open choice is still judged.
func TestGoAheadThatAsksNothingNewIsNotJudgedForNeedsPlan(t *testing.T) {
	for _, c := range []struct {
		name      string
		continues any
		judged    bool
	}{
		{name: "go-ahead asking nothing new", continues: true, judged: false},
		{name: "new request with an open choice", continues: false, judged: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, root, calls := planningFixture(t)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			if _, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID}); err != nil {
				t.Fatal(err)
			}
			payload := planningPayload(root)
			payload["continues_previous"] = c.continues
			before := calls.Load()
			if _, err := planningCall(t, "submit", payload); err != nil {
				t.Fatal(err)
			}
			got := calls.Load() > before
			if got != c.judged {
				t.Fatalf("needs-plan judged = %v, want %v (model calls %d -> %d)", got, c.judged, before, calls.Load())
			}
		})
	}
}
