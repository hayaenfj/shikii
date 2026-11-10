package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
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

const interruptedRequest = "add a paragraph about the storage layout to the README"

// interruptedTurn is a request whose turn changed the README (and committed, when commit is given) and then got no Stop: the
// person interrupted it, so it was never judged.
func interruptedTurn(t *testing.T, repo, root, readme, session, content, commit string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": interruptedRequest})
	organizeTurn(t, session, loadSession(root, session).TurnID)
	if err := os.WriteFile(readme, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if commit != "" {
		gitCommitAll(t, repo, commit)
	}
	// no Stop: the person interrupted the turn
}

// An interrupted request that came before the repository's first commit kept no HEAD. The slash command's turn after it is
// checked from the request's listing, and its commits are read from the slash command's HEAD.
func TestSlashCommandTurnAfterAnInterruptedRequestBeforeTheFirstCommit(t *testing.T) {
	repo, root, readme := twoGuardRepo(t, false)
	interruptedTurn(t, repo, root, readme, "b", "# project\n\na clean line\n", commitWithBody)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "b", "cwd": repo, "prompt": "/commit"})
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: subject only")
	out := runHook(t, "stop", map[string]any{"session_id": "b", "cwd": repo, "last_assistant_message": "committed"})
	if !strings.Contains(out, "- commit-body:") {
		t.Fatalf("not stopped: %q; records %+v", out, guardRecordsFor(root, "commit-body"))
	}
}

// After an interrupted request the person switches to a branch that does not contain the request's HEAD. The slash command's own
// commit without a body is still read (from the slash command's HEAD) and stopped, not recorded as "not verified".
func TestSlashCommandTurnAfterAnInterruptedRequestAndABranchSwitch(t *testing.T) {
	repo, root, readme := twoGuardRepo(t, true)
	gitIn(t, repo, "branch", "other")
	if err := os.WriteFile(filepath.Join(repo, "SECOND.md"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, commitWithBody)
	interruptedTurn(t, repo, root, readme, "c", "# project\n\na clean line\n", "")
	gitIn(t, repo, "checkout", "-q", "-f", "other")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "/commit"})
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: subject only")
	out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "committed"})
	if !strings.Contains(out, "- commit-body:") {
		t.Errorf("the slash command's own commit without a body is not stopped after an interrupted request and a branch switch: %q %+v", out, guardRecordsFor(root, "commit-body"))
	}
}

// A slash command whose turn changes nothing (/model) stops nothing, also after an interrupted request that left a forbidden
// line: the line is stopped once a slash command's turn changes or commits something, or at the next request's Stop.
func TestIdleSlashCommandAfterAnInterruptedRequestStopsNothing(t *testing.T) {
	repo, root, readme := twoGuardRepo(t, true)
	interruptedTurn(t, repo, root, readme, "d", "# project\n\nFORBIDDENWORD here\n", "")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "d", "cwd": repo, "prompt": "/model"})
	if out := runHook(t, "stop", map[string]any{"session_id": "d", "cwd": repo, "last_assistant_message": "ok"}); out != "" {
		t.Fatalf("a slash command that changed nothing stops nothing: %q", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "d", "cwd": repo, "prompt": "/commit"})
	gitCommitAll(t, repo, commitWithBody)
	if out := runHook(t, "stop", map[string]any{"session_id": "d", "cwd": repo, "last_assistant_message": "committed"}); !strings.Contains(out, "- no-word:") {
		t.Fatalf("the slash command that commits the interrupted request's line is stopped: %q", out)
	}
}

// An overtaken Stop says nothing at all: the one-time notice about a question's line is not shown by it and not used up, so a
// later Stop still tells the person.
func TestOvertakenStopLeavesTheOneTimeNoticesForALaterStop(t *testing.T) {
	repo, root, first := overtakenTurn(t, func(string) float64 { return .1 })
	place := mustLoad(t, filepath.Join(root, "decisions", "wiki-place.yaml"))
	var labels []store.Label
	for i := 0; i < place.Eval.MinN; i++ {
		id := fmt.Sprintf("wiki-place-m1-no-%03d", i)
		labels = append(labels, store.Label{TraceID: id, SpecID: place.ID, SpecHash: place.Hash(), Type: string(place.Type), Model: "m1", InputsHash: id, Truth: "no", Weight: 1, Source: "outcome", At: time.Now()})
	}
	if err := (store.Store{Dir: root}).AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	ran := atTheStopsLock(t, root, func() {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenNext})
	})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
	if !ran() {
		t.Fatal("the Stop found the session lock held")
	}
	requireSilentStop(t, out)
	requireNextRequestKept(t, root, first)
	if strings.TrimSpace(out) != "" {
		t.Errorf("an overtaken Stop outputs nothing, also no notice: %q", out)
	}
	if line := testLineSummaries(root); !strings.Contains(line, "Where a wiki lesson goes") {
		t.Errorf("the notice the overtaken Stop did not show is still to come: %q", line)
	}
}

// The check before a publication in a slash command's turn, after an interrupted request that came before the repository's
// first commit (it kept no HEAD): the commits are read from the slash command's HEAD, so a push of a commit without a body is
// refused.
func TestPushInASlashCommandTurnAfterARequestBeforeTheFirstCommit(t *testing.T) {
	repo, root, readme := twoGuardRepo(t, false)
	interruptedTurn(t, repo, root, readme, "p", "# project\n\na clean line\n", commitWithBody)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "p", "cwd": repo, "prompt": "/push"})
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: subject only")
	out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "p", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "git push origin HEAD"}})
	if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "commit-body") {
		t.Fatalf("the push of the slash command's commit without a body is refused: %q; records %+v", out, guardRecordsFor(root, "commit-body"))
	}
}
