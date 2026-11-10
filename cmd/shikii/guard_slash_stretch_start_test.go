package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const commitWithBody = "docs: the storage layout\n\nProblem: the layout was not described.\nVerification: read the README."

func twoGuardRepo(t *testing.T, initial bool) (repo, root, readme string) {
	t.Helper()
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo = gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root = filepath.Join(repo, ".shikii")
	readme = filepath.Join(repo, "README.md")
	if initial {
		if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommitAll(t, repo, "init")
	}
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	writeGuard(t, root, guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "the body names the problem and the verification", Required: []string{"Problem", "Verification"}})
	return repo, root, readme
}

func organizeTurn(t *testing.T, session, turn string) {
	t.Helper()
	if _, err := planningCall(t, "submit", map[string]any{"session_id": session, "turn_id": turn, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, repo string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func cleanRequestTurn(t *testing.T, repo, root, readme, session, commit string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "add a paragraph about the storage layout to the README"})
	organizeTurn(t, session, loadSession(root, session).TurnID)
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if commit != "" {
		gitCommitAll(t, repo, commit)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "added"}); strings.Contains(out, `"decision":"block"`) || strings.Contains(out, `"continue":false`) {
		t.Fatalf("the request's clean turn is not stopped: %s", out)
	}
}

func stoppedOrEnded(out string) bool {
	return strings.Contains(out, `"decision":"block"`) || strings.Contains(out, `"continue":false`)
}

// The Stop of a slash command's turn fails a guard while records/guards.jsonl cannot be written (a folder stands in its place).
// It must not pass in silence: like a request's Stop, it ends the continuation explicitly.
func TestStopOfASlashCommandTurnEndsIncompleteWhenItsRecordsCannotBeWritten(t *testing.T) {
	repo, root, readme := twoGuardRepo(t, true)
	cleanRequestTurn(t, repo, root, readme, "a", "")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "a", "cwd": repo, "prompt": "/commit"})
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\nFORBIDDENWORD here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: subject only")
	p := filepath.Join(root, "records", "guards.jsonl")
	if _, err := os.Stat(p); err == nil {
		if err := os.Rename(p, p+".aside"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "a", "cwd": repo, "last_assistant_message": "committed"})
	if !stoppedOrEnded(out) {
		t.Fatalf("a failing guard whose record cannot be written neither stopped the turn nor ended it incomplete: %q", out)
	}
}

// The request came before the repository's first commit, so it kept no HEAD. A commit without a body made in the slash
// command's turn afterwards is still read and stopped.
func TestSlashCommandTurnChecksItsCommitAfterARequestBeforeTheFirstCommit(t *testing.T) {
	for name, initial := range map[string]bool{"control: the repository had a commit at the request": true, "the request came before the first commit": false} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := twoGuardRepo(t, initial)
			cleanRequestTurn(t, repo, root, readme, "b", commitWithBody)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "b", "cwd": repo, "prompt": "/commit"})
			if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("notes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, "docs: subject only")
			out := runHook(t, "stop", map[string]any{"session_id": "b", "cwd": repo, "last_assistant_message": "committed"})
			if !strings.Contains(out, "- commit-body:") {
				t.Fatalf("the commit without a body made in the slash command's turn is not stopped: %q; records %+v", out, guardRecordsFor(root, "commit-body"))
			}
		})
	}
}

// After the request's turn ended, the person switches to a branch that does not contain the request's HEAD. The slash command's
// turn is compared with its own start, so its commit without a body is read and stopped (not recorded as "not verified").
func TestSlashCommandTurnChecksItsCommitAfterThePersonSwitchedBranches(t *testing.T) {
	for name, switched := range map[string]bool{"control: the same branch": false, "the person switched to a diverged branch before the slash command": true} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := twoGuardRepo(t, true)
			gitIn(t, repo, "branch", "other")
			if err := os.WriteFile(filepath.Join(repo, "SECOND.md"), []byte("second\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, commitWithBody)
			cleanRequestTurn(t, repo, root, readme, "c", "")
			if switched {
				gitIn(t, repo, "checkout", "-q", "-f", "other") // the request's uncommitted line is not carried over
			}
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "/commit"})
			if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("notes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, "docs: subject only")
			out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "committed"})
			if !strings.Contains(out, "- commit-body:") {
				t.Fatalf("the commit without a body made in the slash command's turn is not stopped: %q; records %+v", out, guardRecordsFor(root, "commit-body"))
			}
		})
	}
}

// An overtaken Stop keeps nothing, not even an empty records/guards.jsonl from checking that the file can be written.
func TestOvertakenStopLeavesNoRecordsFileBehind(t *testing.T) {
	repo, root, readme := twoGuardRepo(t, true)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": "/commit"})
	if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: subject only")
	p := guardRecordsPath(root)
	if _, err := os.Stat(p); err == nil {
		t.Fatalf("no records file before the Stop")
	}
	ran := atTheStopsLock(t, root, func() {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": "now add a section about the license to the README as well"})
	})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "committed"})
	if !ran() {
		t.Fatal("the Stop found the session lock held")
	}
	requireSilentStop(t, out)
	if info, err := os.Stat(p); err == nil {
		t.Fatalf("the overtaken Stop left records/guards.jsonl behind (%d bytes)", info.Size())
	}
}
