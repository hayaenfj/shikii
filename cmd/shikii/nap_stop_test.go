package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real Codex 0.172.0 is told at the session start to start a background subagent that runs `shikii nap`, and its own rules forbid it to
// start one unless the person or the project's instructions ask for one (2026-11-28): in 2 of 3 real sessions the notice came and no nap
// ran. The Stop that blocks anyway (the saved work it asks for) is the one text Codex always obeyed, so it carries the ask once more while
// the notice is still not followed: no `shikii nap` has run since it. It is only for Codex: Claude Code starts the subagent.

// napPendingStopReason runs the request hook and a Stop that blocks (a changed file, a plan not sorted out) as agent in a project whose
// .shikii/nap holds mark, and returns the Stop's reason.
func napPendingStopReason(t *testing.T, agent, mark string) string {
	t.Helper()
	repo, root, _ := planningFixture(t)
	actAs(t, agent)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if err := os.WriteFile(filepath.Join(root, napStateName), []byte(mark), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
		t.Fatalf("precondition: the Stop is blocked: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	return reason
}

func TestTheStopThatBlocksAsksCodexForTheNapThatWasNotRun(t *testing.T) {
	pending := formatNapMark(time.Now().Add(-time.Minute), time.Time{}, time.Time{})
	reason := napPendingStopReason(t, "codex", pending)
	if !strings.Contains(reason, "A memory nap is due and no `shikii nap` has run since it was asked for at the session start") || !strings.Contains(reason, " nap in this project and follow its next_action") {
		t.Fatalf("the block asks Codex to run the nap:\n%s", reason)
	}
	if !strings.Contains(reason, "required work is not saved yet") || !strings.Contains(reason, "hook plan query") {
		t.Fatalf("the block keeps the work it asked for:\n%s", reason)
	}

	// Claude Code starts the subagent the notice asked for: its Stop is the same as it was
	if reason := napPendingStopReason(t, "claude", pending); strings.Contains(reason, "memory nap") {
		t.Fatalf("Claude Code's block does not carry the ask:\n%s", reason)
	}
	// a nap ran after the notice (the third line of the file is the time of the first), and no notice was ever given
	ran := formatNapMark(time.Now().Add(-time.Minute), time.Time{}, time.Now())
	if reason := napPendingStopReason(t, "codex", ran); strings.Contains(reason, "memory nap") {
		t.Fatalf("a nap that ran is not asked for again:\n%s", reason)
	}
	if reason := napPendingStopReason(t, "codex", ""); strings.Contains(reason, "memory nap") {
		t.Fatalf("with no notice there is nothing to ask for:\n%s", reason)
	}
}
