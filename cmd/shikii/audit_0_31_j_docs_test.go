package main

import (
	"strings"
	"testing"
)

// The skill's references say what an agent that holds no receipt does first (planning.md: prepare with the hook's ids, then
// record), and that a first conversation over the bound even alone comes as a head (review.md). Both are what 0.31.0 does, and no
// test held the sentences (audit J of 0.31.0: taking either out passed every test).
func TestTheReferencesSayWhatAReceiptlessAgentDoesFirst(t *testing.T) {
	for path, words := range map[string]string{
		"../../skills/shikii/references/planning.md": "prepare first with the current hook\u2019s IDs when you hold no receipt",
		"../../skills/shikii/references/review.md":   "A first conversation over the bound even alone comes as a head from the start.",
	} {
		if !strings.Contains(string(mustRead(t, path)), words) {
			t.Errorf("%s does not say %q", path, words)
		}
	}
}
