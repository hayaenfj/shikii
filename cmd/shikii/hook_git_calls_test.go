package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// gitCallLog puts a git in front of the real one that writes down the arguments of each call (one line per call), and returns a
// function that gives the calls made since it was last asked, in order.
func gitCallLog(t *testing.T) func() []string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	bin, seen := t.TempDir(), filepath.Join(t.TempDir(), "calls.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + seen + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		t.Helper()
		b, _ := os.ReadFile(seen)
		if err := os.WriteFile(seen, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if len(b) == 0 {
			return nil
		}
		return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	}
}

// slowFirstPlaceGit puts a git in front of the real one whose first question about where the repository is (rev-parse --show-toplevel) takes
// `delay` before it goes on: a machine so loaded that git is slow. Every other call goes straight through. Call it before gitCallLog, which
// puts its own git in front of this one, so the slow call is written down at once, as it is when it is started and then runs out of time.
func slowFirstPlaceGit(t *testing.T, delay time.Duration) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	bin, mark := t.TempDir(), filepath.Join(t.TempDir(), "slowed")
	script := "#!/bin/sh\nif [ \"$1\" = rev-parse ] && [ \"$2\" = --show-toplevel ] && [ ! -e '" + mark + "' ]; then\n  : > '" + mark + "'\n  sleep " + fmt.Sprintf("%.3f", delay.Seconds()) + "\nfi\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Found on 2026-11-21: under make test (eight groups of cmd/shikii tests at the same time) a request in a linked worktree once made two
// of the questions about where the repository is (TestAHookEventInAWorktreeOrSubmoduleAsksGitWhereTheRepositoryIsOnce). The first one is
// asked with a limit of its own, a second (hookPlaceTimeout), which patientGit did not stretch; a git slower than that (here 1.2 s) ran
// out of time, was not kept, and the next step asked again. A test that counts git calls must see the same calls on a slow machine: the
// answer is waited for, and asked once.
func TestASlowAnswerOfGitToTheFirstQuestionIsStillAskedOnce(t *testing.T) {
	const place = "rev-parse --show-toplevel --git-path objects --git-path info/exclude --verify -q HEAD"
	snapshot := []string{"add -A -- .", "ls-files -z --cached --ignored --exclude-standard", "ls-files --stage --full-name -- . :(exclude).shikii"}
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	forgetKeptPlaces(t)
	slowFirstPlaceGit(t, 1200*time.Millisecond)
	calls := gitCallLog(t)
	runManualHook(t, "user-prompt-submit", map[string]any{"session_id": "slow-first", "cwd": repo, "prompt": planningTestRequest})
	want := append([]string{place}, snapshot...)
	if got := calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("a request with a slow git made these git calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Every git call is a process (about 15 ms through the stub that macOS puts at /usr/bin/git, and the hooks run before every request, at
// every Stop and before every tool call). One event used to ask git four times for the top folder of the repository, and once each for its
// object folder and its exclude file; one question now gives all three, and the next events use what it said while the folders still say
// the same (hook_git_place.go). This lists the git calls an event may make, in order, so a call added to the hook's path shows up here
// and has to be argued for.
func TestAHookEventAsksGitWhereTheRepositoryIsOnce(t *testing.T) {
	const place = "rev-parse --show-toplevel --git-path objects --git-path info/exclude --verify -q HEAD" // HEAD is in the same answer
	// the listing of the temporary index leaves the internal records out itself (an exclude pathspec), and its hash is the marker of
	// the tree: no `rm --cached` of the records and no `write-tree` for an id the listing already says
	snapshot := []string{"add -A -- .", "ls-files -z --cached --ignored --exclude-standard", "ls-files --stage --full-name -- . :(exclude).shikii"}
	repo, _, _ := planningFixture(t)
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	forgetKeptPlaces(t) // nothing is known about the repository yet: the first event has to ask
	calls := gitCallLog(t)
	for i, cwd := range []string{repo, sub} {
		session := "calls-" + filepath.Base(cwd)
		runManualHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": cwd, "prompt": planningTestRequest})
		// the first event of the repository asks git where it is (HEAD comes in the same answer); a later one uses what git said (also
		// from a subfolder) and reads HEAD, which every commit changes, from the files of the repository
		want := append([]string{place}, snapshot...)
		if i > 0 {
			want = snapshot
		}
		if got := calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("a request from %s made these git calls:\n%s\nwant:\n%s", cwd, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		// a tool call needs the project and nothing from git
		runManualHook(t, "tool-lesson", map[string]any{"session_id": session, "cwd": cwd, "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}})
		if got := calls(); len(got) != 0 {
			t.Errorf("a tool call from %s made git calls:\n%s", cwd, strings.Join(got, "\n"))
		}
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed from "+filepath.Base(cwd)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": cwd, "last_assistant_message": "Done."})
		if got := calls(); !reflect.DeepEqual(got, snapshot) {
			t.Errorf("a Stop from %s made these git calls:\n%s\nwant:\n%s", cwd, strings.Join(got, "\n"), strings.Join(snapshot, "\n"))
		}
	}
}

// Outside git (a folder with no .git in it or above it) every event of the project used to start 2 to 6 git processes to hear "not a
// repository"; the folders say it without one.
func TestAHookEventOutsideGitStartsNoGit(t *testing.T) {
	folder := t.TempDir()
	hookEnv(t, nil, folder)
	calls := gitCallLog(t)
	for _, c := range []struct {
		event string
		in    map[string]any
	}{
		{"session-start", map[string]any{"session_id": "outside", "cwd": folder}},
		{"user-prompt-submit", map[string]any{"session_id": "outside", "cwd": folder, "prompt": planningTestRequest}},
		{"tool-lesson", map[string]any{"session_id": "outside", "cwd": folder, "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}},
		{"subagent-start", map[string]any{"session_id": "outside", "cwd": folder, "agent_id": "a1", "agent_type": "general-purpose"}},
		{"stop", map[string]any{"session_id": "outside", "cwd": folder, "last_assistant_message": "Done."}},
	} {
		runManualHook(t, c.event, c.in)
		if got := calls(); len(got) != 0 {
			t.Errorf("%s outside git made these git calls:\n%s", c.event, strings.Join(got, "\n"))
		}
	}
}

// An answer that did not come (the time ran out, or git could not start) is not an answer: the next step that needs it asks again with
// its own time limit, as before the answers were kept. Without this, one slow git under load would make the rest of the event see
// "outside git".
func TestAGitCallThatTimedOutIsNotKeptForTheRestOfTheEvent(t *testing.T) {
	repo := gitRepo(t)
	defer beginEventGit()()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := gitPlaceAt(expired, repo); ok {
		t.Fatal("premise: git does not answer with the time already out")
	}
	place, ok := gitPlaceAt(context.Background(), repo)
	if !ok || feedbackScope(place.top) != feedbackScope(repo) {
		t.Fatalf("the failed call was kept: top %q, ok %v (want %s)", place.top, ok, repo)
	}
	if _, ok := gitPlaceAt(expired, repo); !ok {
		t.Error("the answer git gave is kept for the event: asked again with the time out, it still answers")
	}
}

// HEAD, where this turn's commits start, comes with the answer about the repository, in a repository that has a commit and in one
// that does not (an unborn HEAD is an empty answer, not a reason to ask again).
func TestTheHeadComesWithTheAnswerAboutTheRepository(t *testing.T) {
	committed := gitRepo(t)
	if err := os.WriteFile(filepath.Join(committed, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, committed, "one")
	unborn := gitRepo(t)
	calls := gitCallLog(t)
	for _, c := range []struct{ dir, head string }{
		{committed, strings.TrimSpace(run(t, "git", "-C", committed, "rev-parse", "HEAD"))},
		{unborn, ""},
	} {
		calls()
		end := beginEventGit()
		if _, ok := gitPlaceAt(context.Background(), c.dir); !ok {
			t.Fatalf("premise: git answers about %s", c.dir)
		}
		if got := gitHead(c.dir); got != c.head {
			t.Errorf("HEAD of %s is %q, want %q", c.dir, got, c.head)
		}
		end()
		if got := calls(); len(got) != 1 {
			t.Errorf("the place of %s and its HEAD took %d git calls, want 1:\n%s", c.dir, len(got), strings.Join(got, "\n"))
		}
	}
}

// The brief at the start of a session asks git where the repository is once, as every other event does (it asked twice: the project, then
// the exclude file).
func TestTheSessionStartAsksGitWhereTheRepositoryIsOnce(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	forgetKeptPlaces(t)
	calls := gitCallLog(t)
	for i, want := range [][]string{{"rev-parse --show-toplevel --git-path objects --git-path info/exclude --verify -q HEAD"}, nil} {
		runManualHook(t, "session-start", map[string]any{"session_id": "start-once", "cwd": repo})
		if got := calls(); !reflect.DeepEqual(got, want) { // the second start finds the answer kept by the first
			t.Errorf("session start %d made these git calls:\n%s\nwant:\n%s", i+1, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// forgetKeptPlaces drops what the tests so far kept of git's answers about repositories (the test home's folder of them).
func forgetKeptPlaces(t *testing.T) {
	t.Helper()
	agent, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(agent, "places")); err != nil {
		t.Fatal(err)
	}
}

// Codex's memory folder (~/.codex/memories) makes a request read the files there that name the project, and a project is named also
// by the repository name of its git origin. That question to git was asked at every request whenever the folder existed: also outside
// git, where the folders already say there is nothing to ask. It is now asked only when a file that is not moved to the wiki yet has to
// be compared with the names, and never outside git.
func TestACodexMemoryFolderDoesNotMakeEveryRequestAskGit(t *testing.T) {
	memory := func(t *testing.T, files map[string]string) {
		t.Helper()
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(home, ".codex", "memories")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	unmoved := map[string]string{"other.md": "---\nname: other\n---\nA note about another project.\n"}
	moved := map[string]string{"done.md": "---\nname: done\n---\ncanonical: .shikii/wiki 0123456789abcdef\n"}
	asked := func(calls []string) []string {
		var out []string
		for _, c := range calls {
			if strings.HasPrefix(c, "remote ") {
				out = append(out, c)
			}
		}
		return out
	}

	t.Run("outside git, a file not moved yet", func(t *testing.T) {
		folder := t.TempDir()
		hookEnv(t, nil, folder)
		memory(t, unmoved)
		calls := gitCallLog(t)
		runManualHook(t, "user-prompt-submit", map[string]any{"session_id": "codex-memory-outside", "cwd": folder, "prompt": planningTestRequest})
		if got := calls(); len(got) != 0 {
			t.Errorf("a request outside git made these git calls:\n%s", strings.Join(got, "\n"))
		}
	})
	t.Run("in git, every file already moved", func(t *testing.T) {
		repo, _, _ := planningFixture(t)
		t.Chdir(repo)
		memory(t, moved)
		calls := gitCallLog(t)
		runManualHook(t, "user-prompt-submit", map[string]any{"session_id": "codex-memory-moved", "cwd": repo, "prompt": planningTestRequest})
		if got := asked(calls()); len(got) != 0 {
			t.Errorf("nothing in Codex's memory had to be compared with the names, yet git was asked: %v", got)
		}
	})
	t.Run("in git, a file not moved yet is compared with the names", func(t *testing.T) {
		repo, _, _ := planningFixture(t)
		t.Chdir(repo)
		memory(t, unmoved)
		calls := gitCallLog(t)
		runManualHook(t, "user-prompt-submit", map[string]any{"session_id": "codex-memory-unmoved", "cwd": repo, "prompt": planningTestRequest})
		if got := asked(calls()); len(got) != 1 {
			t.Errorf("a file that may name the project needs the origin once: asked %v", got)
		}
	})
}
