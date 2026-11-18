package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

// Every git call is a process (about 15 ms through the stub that macOS puts at /usr/bin/git, and the hooks run before every request and
// at every Stop). One event used to ask git four times for the top folder of the repository, and once each for its object folder and
// its exclude file; one question now gives all three. This lists the git calls an event may make, in order, so a call added to the
// hook's path shows up here and has to be argued for.
func TestAHookEventAsksGitWhereTheRepositoryIsOnce(t *testing.T) {
	const place = "rev-parse --show-toplevel --git-path objects --git-path info/exclude"
	snapshot := []string{"add -A -- .", "ls-files -z --cached --ignored --exclude-standard", "rm --cached -r -f --ignore-unmatch -- .shikii", "write-tree", "ls-files --stage --full-name"}
	repo, _, _ := planningFixture(t)
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	calls := gitCallLog(t)
	for _, cwd := range []string{repo, sub} {
		session := "calls-" + filepath.Base(cwd)
		runManualHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": cwd, "prompt": planningTestRequest})
		want := append(append([]string{place}, snapshot...), "rev-parse HEAD") // HEAD: where this turn's commits start
		if got := calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("a request from %s made these git calls:\n%s\nwant:\n%s", cwd, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed from "+filepath.Base(cwd)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": cwd, "last_assistant_message": "Done."})
		want = append([]string{place}, snapshot...)
		if got := calls(); !reflect.DeepEqual(got, want) {
			t.Errorf("a Stop from %s made these git calls:\n%s\nwant:\n%s", cwd, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// Outside git the answer is no, and the hook asks once: it used to ask once for each step that needs the top folder.
func TestAHookEventOutsideGitAsksGitOnce(t *testing.T) {
	folder := t.TempDir()
	hookEnv(t, nil, folder)
	calls := gitCallLog(t)
	runManualHook(t, "user-prompt-submit", map[string]any{"session_id": "outside", "cwd": folder, "prompt": planningTestRequest})
	// the one question, the exclude file (asked alone: a bare repository has no top folder but does have the file), and HEAD
	want := []string{"rev-parse --show-toplevel --git-path objects --git-path info/exclude", "rev-parse --git-path info/exclude", "rev-parse HEAD"}
	if got := calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("a request outside git made these git calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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
