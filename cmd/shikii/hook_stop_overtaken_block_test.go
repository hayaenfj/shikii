package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Stop that fails one of the person's guards while the next request replaces its turn says nothing and keeps nothing: the
// block would be read as a judgment of the new request, and the failed check would count as a stop of a turn that is over.
// (Only the Stops that passed were silent before.)
func TestStopOvertakenWhileAGuardFailsStaysSilent(t *testing.T) {
	repo, root, first := overtakenTurn(t, func(string) float64 { return .1 })
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# project\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := atTheStopsLock(t, root, func() {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenNext})
	})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
	if !ran() {
		t.Fatal("premise: the Stop found the session lock held")
	}
	requireSilentStop(t, out)
	requireNextRequestKept(t, root, first)
	for _, r := range readGuardRecords(root) {
		if r.Session == "s" && r.Turn == first && !r.Pass {
			t.Errorf("the overtaken Stop kept a failed check of the turn that is over: %+v", r)
		}
	}
	if shikiiStoppedSession(root, "s") {
		t.Errorf("the session counts as stopped by shikii, although the Stop that failed the check was overtaken")
	}
}

// The same for the built-in routine check (files changed while the work was not prepared).
func TestStopOvertakenWhileTheRoutineCheckFailsStaysSilent(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenFirst})
	first := loadSession(root, "s").TurnID
	if err := os.WriteFile(readme, []byte("# project\n\na new line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := atTheStopsLock(t, root, func() {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenNext})
	})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
	if !ran() {
		t.Fatal("premise: the Stop found the session lock held")
	}
	requireSilentStop(t, out)
	requireNextRequestKept(t, root, first)
	for _, r := range readGuardRecords(root) {
		if r.Session == "s" && r.Turn == first && !r.Pass {
			t.Errorf("the overtaken Stop kept a failed check of the turn that is over: %+v", r)
		}
	}
}

// A Stop that was not overtaken still stops the turn and keeps its failed check (the records wait only for the commit of the
// session record).
func TestStopThatFailsAGuardStillBlocksAndKeepsItsRecord(t *testing.T) {
	repo, root, first := overtakenTurn(t, func(string) float64 { return .1 })
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# project\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") {
		t.Fatalf("the guard stops the turn: %s", out)
	}
	kept := false
	for _, r := range readGuardRecords(root) {
		kept = kept || r.Session == "s" && r.Turn == first && r.Guard == "no-word" && !r.Pass && !r.Recheck
	}
	if !kept || !shikiiStoppedSession(root, "s") {
		t.Errorf("the failed check is kept as a stop of this turn: %+v", readGuardRecords(root))
	}
}

// shikii's own block for changed completion evidence is a stop by shikii: when the session record is gone at the next Stop, the
// continuation is ended explicitly instead of passing silently. (This block left no record, so the Stop after it said nothing.)
func TestStopAfterTheCompletionBlockHaltsWhenTheRecordIsGone(t *testing.T) {
	var repo, root string
	armed := false
	repo, root, _ = overtakenTurn(t, func(string) float64 {
		if armed { // while the Stop asks the model, the record of the same turn changes (not a next request: the turn id stays)
			armed = false
			if err := updateSession(root, "s", func(s *hookSession) { s.Scope += "-changed" }); err != nil {
				t.Error(err)
			}
		}
		return .1
	})
	armed = true
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
	if armed {
		t.Fatal("premise: the Stop asked the model")
	}
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "completion evidence changed") {
		t.Fatalf("premise: shikii itself blocked this Stop: %s", out)
	}
	if !shikiiStoppedSession(root, "s") {
		t.Errorf("the completion block is kept as a stop by shikii")
	}
	if err := os.Remove(sessionPath(root, "s")); err != nil {
		t.Fatal(err)
	}
	after := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "stop_hook_active": true, "last_assistant_message": "explained"})
	requireRecoveryHalt(t, after)
}

// A file whose name ends with a space, on the last line of the listing: the changed file and the guard see its real name.
// (The whole listing was trimmed, which cut the last name.)
func TestChangedFileWhoseNameEndsWithASpaceKeepsItsName(t *testing.T) {
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	_, before, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: listed")
	}
	const name = "zz notes " // ends with a space, and sorts last in the listing
	if err := os.WriteFile(filepath.Join(repo, name), []byte("a new file with FORBIDDENWORD\n"), 0o644); err != nil {
		t.Skipf("the file system does not keep this name: %v", err)
	}
	_, after, _, _, _ := treeStateWithScope(repo)
	changed := changedFiles(before, after)
	if len(changed) != 1 || changed[0] != name {
		t.Errorf("the changed file is listed by its real name %q, got %q", name, changed)
	}
	in := guardTurnInput(repo, listingHeader("r")+before, changed, true, "", time.Time{})
	if got := strings.Join(in.Added[name], "|"); !strings.Contains(got, "FORBIDDENWORD") {
		t.Errorf("the guard reads the file by its real name, added lines: %v", in.Added)
	}
}

// guard try names a guard file that does not load, as an error, while the Stop skips that file and runs the others.
func TestGuardTryReportsAGuardFileThatDoesNotLoad(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	if err := os.WriteFile(guardPath(root, "half-edited"), []byte(h4BrokenGuard), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guardTry(root); err == nil || !strings.Contains(err.Error(), "half-edited.yaml") {
		t.Fatalf("guard try names the file that does not load: %v", err)
	}
}
