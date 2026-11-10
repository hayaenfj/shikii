package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// routineStoppedTurn starts a turn in a repository with the given forbidden-words guards and runs it up to the first Stop, which
// only the built-in routine check stops (the organizing was not saved; every guard passes there). It returns the repository, the
// store, the README the turn writes to and the turn id.
func routineStoppedTurn(t *testing.T, session string, guards map[string]string) (repo, root, readme, turn string) {
	t.Helper()
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo = gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root = filepath.Join(repo, ".shikii")
	readme = filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	for id, word := range guards {
		writeGuard(t, root, guard{ID: id, Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{word}})
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "add a section about the storage layout to the README"})
	turn = loadSession(root, session).TurnID
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || strings.Contains(out, "no-word") {
		t.Fatalf("premise: the first Stop is stopped by the routine check only: %s", out)
	}
	for id := range guards {
		if recs := guardRecordsFor(root, id); len(recs) != 1 || !recs[0].Pass || recs[0].Recheck {
			t.Fatalf("premise: the guard %s passed at the first Stop: %+v", id, recs)
		}
	}
	return repo, root, readme, turn
}

func submitOrganizing(t *testing.T, session, turn string) {
	t.Helper()
	if _, err := planningCall(t, "submit", map[string]any{"session_id": session, "turn_id": turn, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
}

func guardsStatusLine(t *testing.T, root string) string {
	t.Helper()
	for _, line := range strings.Split(capture(t, func() error { return cmdStatus([]string{"--store", root}) }), "\n") {
		if strings.HasPrefix(line, "guards:") {
			return line
		}
	}
	t.Fatal("status has no guards: line")
	return ""
}

// A guard that fails for the first time after the built-in routine check stopped the turn still stops the turn once: the Stop
// carries stop_hook_active, but this guard has not stopped this turn yet. status counts the turn as stopped. After its own stop
// the guard only records whether the work was fixed, as before.
func TestGuardStopsOnceAfterTheRoutineCheckStoppedTheTurn(t *testing.T) {
	repo, root, readme, turn := routineStoppedTurn(t, "r", map[string]string{"no-word": "FORBIDDENWORD"})
	submitOrganizing(t, "r", turn)
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\nFORBIDDENWORD added after the first Stop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the Stop after the routine's stop: the guard fails for the first time in this turn, and stops it
	out := runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": "saved and added", "stop_hook_active": true})
	recs := guardRecordsFor(root, "no-word")
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") || !strings.Contains(out, "FORBIDDENWORD") {
		t.Fatalf("the guard stops the Stop after the routine's stop: %q", out)
	}
	if len(recs) != 2 || recs[1].Pass || recs[1].Recheck || recs[1].Turn != turn {
		t.Fatalf("the stop is recorded as a stop of this turn, not as a recheck: %+v", recs)
	}
	if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 0") {
		t.Fatalf("status counts the turn as stopped: %s", line)
	}
	// the Stop after the guard's own stop, not fixed: the same guard does not stop the same turn twice
	out = runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": "not yet", "stop_hook_active": true})
	recs = guardRecordsFor(root, "no-word")
	if strings.Contains(out, `"decision":"block"`) || strings.Contains(out, "no-word") {
		t.Fatalf("after its own stop the guard does not stop the turn again: %q", out)
	}
	if len(recs) != 3 || recs[2].Pass || !recs[2].Recheck {
		t.Fatalf("the failure after the guard's own stop is kept as a recheck: %+v", recs)
	}
	if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 0") {
		t.Fatalf("a recheck is not another stop: %s", line)
	}
	// fixed: the recheck passes and status counts the fix
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": "fixed", "stop_hook_active": true})
	recs = guardRecordsFor(root, "no-word")
	if strings.Contains(out, "no-word") || len(recs) != 4 || !recs[3].Pass || !recs[3].Recheck {
		t.Fatalf("the fix is recorded as a passing recheck: %q %+v", out, recs)
	}
	if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 1") {
		t.Fatalf("status counts the fix: %s", line)
	}
}

// The number of stops in one turn is capped by the number of guards: each guard stops the turn at most once, a later stop names
// only the guard that has not stopped yet, and when every guard has stopped once no Stop is stopped any more, however long the
// work stays unfixed.
func TestGuardsStopATurnAtMostOnceEach(t *testing.T) {
	repo, root, readme, turn := routineStoppedTurn(t, "r", map[string]string{"no-word-a": "WORDAAA", "no-word-b": "WORDBBB"})
	submitOrganizing(t, "r", turn)
	stop := func(reply string) string {
		return runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": reply, "stop_hook_active": true})
	}
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\nWORDAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := stop("added one"); !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "- no-word-a:") || strings.Contains(out, "- no-word-b:") {
		t.Fatalf("the first guard to fail stops the turn: %q", out)
	}
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\nWORDAAA\nWORDBBB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := stop("added another"); !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "- no-word-b:") || strings.Contains(out, "- no-word-a:") {
		t.Fatalf("the second guard stops the turn once, and the guard that already stopped it is not named again: %q", out)
	}
	// both guards have stopped this turn once: nothing is fixed, and no further Stop is stopped
	for _, reply := range []string{"still there", "still there, again", "and again"} {
		if out := stop(reply); strings.Contains(out, `"decision":"block"`) || strings.Contains(out, "no-word") {
			t.Fatalf("a guard stopped the turn a second time (%s): %q", reply, out)
		}
	}
	stops := 0
	for _, id := range []string{"no-word-a", "no-word-b"} {
		for _, r := range guardRecordsFor(root, id) {
			if !r.Pass && !r.Recheck {
				stops++
			}
		}
	}
	if stops != 2 {
		t.Fatalf("two guards stop one turn twice in all, got %d stops", stops)
	}
	if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 0") {
		t.Fatalf("it is one stopped turn, not fixed: %s", line)
	}
}

// The routine check's recovery count is not moved by a guard: when the routine's remaining work did not shrink, the turn ends
// explicitly incomplete even if a guard fails for the first time at that Stop. The guard did not stop anything there, so its
// failure is kept as a recheck and status does not count a stop.
func TestGuardDoesNotContinueATurnTheRoutineCheckGaveUpOn(t *testing.T) {
	repo, root, readme, _ := routineStoppedTurn(t, "r", map[string]string{"no-word": "FORBIDDENWORD"})
	// the organizing is still not saved, and a forbidden line is added
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\nFORBIDDENWORD added after the first Stop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "r", "cwd": repo, "last_assistant_message": "added", "stop_hook_active": true})
	if !strings.Contains(out, `"continue":false`) || strings.Contains(out, `"decision":"block"`) || strings.Contains(out, "no-word") {
		t.Fatalf("the routine check ends the turn incomplete and the guard does not continue it: %q", out)
	}
	if recs := guardRecordsFor(root, "no-word"); len(recs) != 2 || recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("the guard did not stop this Stop, so its failure is not recorded as a stop: %+v", recs)
	}
	if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 0 turns") {
		t.Fatalf("status does not count a stop that was not delivered: %s", line)
	}
}
