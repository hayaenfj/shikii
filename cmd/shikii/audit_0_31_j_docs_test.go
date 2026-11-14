package main

import (
	"strings"
	"testing"
)

// Reference guidance distinguishes missing preparation from a supplied hook preparation, and preserves bounded-source recovery.
func TestTheReferencesSayWhatAReceiptlessAgentDoesFirst(t *testing.T) {
	for path, words := range map[string][]string{
		"../../skills/shikii/references/planning.md": {"When the hook supplies prepared work, read it directly", "prepare first only when no usable preparation remains", "After explicit replacement preparation, use `receipt` instead of those IDs."},
		"../../skills/shikii/references/review.md":   {"A first conversation over the bound even alone comes as a head from the start."},
	} {
		for _, word := range words {
			if !strings.Contains(string(mustRead(t, path)), word) {
				t.Errorf("%s does not say %q", path, word)
			}
		}
	}
}
