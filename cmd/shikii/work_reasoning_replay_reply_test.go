package main

import (
	"strings"
	"testing"
)

// An explicit finish sent again word for word after later evidence stays a retry of the old one: it does not certify the later
// evidence. The reply looks like a success (complete, the reasoning item saved), so the place the reader looks for the next step
// (reasoning.next, which the Stop check repeats) must say that the repeated finish was not applied and what to send instead.
func TestReplayedReasoningFinishReplySaysItWasNotApplied(t *testing.T) {
	repo, _, b, v := reasoningFixture(t)
	saveReasoning(t, b, v["receipt"], reasoningInput("start", "current"), nil)
	finish := reasoningInput("finish", "current")
	saveReasoning(t, b, v["receipt"], finish, nil)
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Checked."})
	// The same request continues after a background notification, and new evidence is recorded.
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "<task-notification>synthetic continuation</task-notification>"})
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Later evidence", "content": "Another condition is still unresolved."}}}); err != nil {
		t.Fatal(err)
	}
	again := saveReasoning(t, b, v["receipt"], finish, nil) // the retry itself is accepted: complete stays true
	reasoning := loopObject(t, again["reasoning"])
	next, _ := reasoning["next"].(string)
	t.Logf("the same finish sent again: complete=%v saved=%v reasoning.complete=%v\n  reasoning.next=%s", again["complete"], again["saved"], reasoning["complete"], next)
	if reasoning["complete"] != false {
		t.Fatalf("precondition: an old finish is not applied to later evidence: %v", again)
	}
	for _, want := range []string{"Work changed after the saved ending review", "repeated word for word", "not applied"} {
		if !strings.Contains(next, want) {
			t.Errorf("reasoning.next says the repeated finish was not applied (missing %q): %s", want, next)
		}
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Another condition remains."})
	if !strings.Contains(out, "reasoning-before-end") || !strings.Contains(out, "repeated word for word") {
		t.Errorf("the Stop check repeats why the ending review does not count: %s", out)
	}
	// a finish whose text reflects the later evidence is applied, and the notice goes away
	changed := reasoningInput("finish", "current")
	changed["result"] = "The synthetic export retained the owner condition; the later evidence leaves one condition unresolved."
	updated := loopObject(t, saveReasoning(t, b, v["receipt"], changed, nil)["reasoning"])
	if next, _ := updated["next"].(string); updated["complete"] != true || strings.Contains(next, "not applied") {
		t.Errorf("a changed finish completes the review without the notice: %v", updated)
	}
}
