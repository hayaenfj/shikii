package main

import (
	"strings"
	"testing"
)

// Behaviour check 0.9.2 (Claude Code): a submit that sorted the wiki but sent no provisional scores came back complete:true,
// so the agent stopped while the same two conversations stayed pending and the Stop hook blocked every turn.
// The receipt now says what is still left for the conversations it named (complete still only says every item was saved).
func TestReflectSubmitSaysWhatIsStillLeft(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	out := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{id}}}})
	left, _ := out["left"].([]any)
	if out["complete"] != true || len(left) != 1 || !strings.Contains(left[0].(string), id) || !strings.Contains(left[0].(string), "provisional scores 1") {
		t.Fatalf("a submit that leaves provisional scores says so in left: %v", out)
	}
	out = reflectCall(t, h, map[string]any{"operation": "submit", "reviews": []map[string]string{{"source_id": id, "trace_id": trace, "truth": "yes", "reason": "checked"}}})
	if out["complete"] != true || out["left"] != nil {
		t.Fatalf("once nothing is left, left is gone: %v", out)
	}
}
