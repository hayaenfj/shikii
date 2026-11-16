package main

import (
	"os"
	"strings"
	"testing"
)

// A blocked upgrade's reply gives the reason of each blocked page, because the instruction asks the agent to report them.
func TestBlockedUpgradeReplyCarriesEachReason(t *testing.T) {
	repo, root, b, _ := blockedUpgradeFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Continue the memory upgrade."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := v["memory_upgrade"].(map[string]any)
	ids, _ := u["unavailable_sources"].([]any)
	reasons, _ := u["unavailable_reasons"].(map[string]any)
	if len(ids) != 2 {
		t.Fatalf("fixture: want 2 blocked pages, got %v", ids)
	}
	for _, id := range ids {
		if reasons[id.(string)] != "The cited source file does not exist." {
			t.Fatalf("the blocked page %v has no reason in the reply: unavailable_reasons=%v", id, reasons)
		}
	}
}

// A blocked upgrade's hook notice must not say the upgrade is running.
func TestBlockedUpgradeHookDoesNotSayUpgrading(t *testing.T) {
	repo, root, _, _ := blockedUpgradeFixture(t)
	out, _, err := memoryUpgradeHook(root, feedbackScope(repo), &hookSession{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.System, "Upgrading memory") {
		t.Fatalf("a blocked upgrade says it is still upgrading: system=%q", out.System)
	}
}

// The optimization is started by query memory-optimize (it reviews declared pages again). The documents must name it,
// or the documented steps never reach the reopening.
func TestSkillDocumentsNameMemoryOptimizeQuery(t *testing.T) {
	for _, path := range []string{"../../skills/shikii/references/recording.md", "../../skills/shikii/SKILL.md"} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "memory-optimize") {
			t.Fatalf("%s does not name query memory-optimize, so the optimization path is not reachable from the documented steps", path)
		}
	}
}
