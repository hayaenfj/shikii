package main

import (
	"strings"
	"testing"
)

// Behaviour check 0.9.2 re-check (claude-ja, claude-en): after a wiki-only submit the receipt said
// left: ["<id> (provisional scores 1)"]. Claude read the target question "Was the requested work finished?" as the person's
// question and refused to answer it for them three turns in a row, so the conversations never left the queue.
// The left line has to say how to clear it: send reviews with the agent's own provisional truth, unknown when it cannot tell.
func TestLeftSaysHowToClearProvisionalScores(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, _ := reviewSeed(t, st, scope, 1)
	out := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{id}}}})
	left, _ := out["left"].([]any)
	if len(left) != 1 {
		t.Fatalf("expected one left line: %v", out)
	}
	l := left[0].(string)
	for _, w := range []string{"reviews", "unknown"} {
		if !strings.Contains(l, w) {
			t.Fatalf("the left line does not say how to clear the provisional scores (missing %q): %q", w, l)
		}
	}
}
