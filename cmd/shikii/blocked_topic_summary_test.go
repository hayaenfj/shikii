package main

import (
	"strings"
	"testing"
)

// Audit round 2, F3: a memory upgrade that stopped with every page blocked must not send the agent back to review in a topic summary (a query other than memory-upgrade).
func TestBlockedUpgradeTopicSummaryDoesNotSendReviewOn(t *testing.T) {
	repo, root, b, _ := blockedUpgradeFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Look at the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "routing"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := v["memory_upgrade"].(map[string]any)
	if u["status"] != "blocked" {
		t.Fatalf("fixture must be blocked: status=%v", u["status"])
	}
	next, _ := u["next_action"].(string)
	if strings.Contains(next, "Review one batch") || strings.Contains(next, "prepare this receipt with query memory-upgrade") {
		t.Fatalf("a blocked upgrade must not send the agent back to review: next_action=%q", next)
	}
}
