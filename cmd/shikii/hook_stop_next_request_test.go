package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	overtakenFirst = "explain what the README says about the storage layout"
	overtakenNext  = "now add a section about the wiki to the README"
)

// overtakenTurn prepares a turn that could end clean: a committed README, the first request and its saved organizing.
func overtakenTurn(t *testing.T, p func(state string) float64) (repo, root, first string) {
	t.Helper()
	model := fakeAgentModel(t, p)
	hookEnv(t, model)
	repo = gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root = filepath.Join(repo, ".shikii")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenFirst})
	first = loadSession(root, "s").TurnID
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "s", "turn_id": first, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	return repo, root, first
}

// atTheStopsLock holds the session lock as another hook would, and runs other at the moment the Stop first finds it held
// (sessionLockBusy): the other hook's read, change and write finish before the Stop gets the lock. One Stop, one fixed order.
func atTheStopsLock(t *testing.T, root string, other func()) (ran func() bool) {
	t.Helper()
	unlock, ok := lockSessionWithin(root, "s", 0)
	if !ok {
		t.Fatal("the other hook's lock must be taken")
	}
	done := false
	sessionLockBusy = func() {
		if done {
			return
		}
		done = true
		unlock()
		other()
	}
	t.Cleanup(func() {
		sessionLockBusy = nil
		if !done {
			unlock()
		}
	})
	return func() bool { return done }
}

func requireSilentStop(t *testing.T, out string) {
	t.Helper()
	for _, said := range []string{`"continue"`, "stopReason", `"decision"`} {
		if strings.Contains(out, said) {
			t.Errorf("a Stop whose turn the next request already replaced has nothing to verify and must say nothing: %s", out)
			return
		}
	}
}

func requireNextRequestKept(t *testing.T, root, first string) {
	t.Helper()
	if s := loadSession(root, "s"); s.TurnID == first || s.Request != overtakenNext || s.Judged {
		t.Errorf("the next request's record must be left as its hook wrote it: turn=%q (first %q) request=%q judged=%v", s.TurnID, first, s.Request, s.Judged)
	}
}

// The person's next request can reach its hook while the Stop of the turn before is still running. That Stop read the old turn;
// by the time it takes the session lock the record belongs to the next request. It lost what to judge, so it stays silent (the next
// request's own Stop judges that turn). It must not end the session's work with "required work could not be verified", and must
// not send the agent back to "save reasoning finish" for a turn that is over.
func TestStopOvertakenByTheNextRequestStaysSilent(t *testing.T) {
	t.Run("at the first lock of an unjudged turn", func(t *testing.T) {
		repo, root, first := overtakenTurn(t, func(string) float64 { return .1 })
		ran := atTheStopsLock(t, root, func() {
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenNext})
		})
		out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
		if !ran() {
			t.Fatal("premise: the Stop found the session lock held")
		}
		requireSilentStop(t, out)
		requireNextRequestKept(t, root, first)
	})
	t.Run("at the last save of a turn that was already judged", func(t *testing.T) {
		repo, root, first := overtakenTurn(t, func(string) float64 { return .1 })
		if out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"}); out != "" || !loadSession(root, "s").Judged {
			t.Fatalf("premise: the first Stop judges the turn and says nothing: %s", out)
		}
		ran := atTheStopsLock(t, root, func() {
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenNext})
		})
		out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained again"})
		if !ran() {
			t.Fatal("premise: the Stop found the session lock held")
		}
		requireSilentStop(t, out)
		requireNextRequestKept(t, root, first)
	})
	t.Run("while the Stop asks the model", func(t *testing.T) {
		var repo string
		armed := false
		repo, root, first := overtakenTurn(t, func(string) float64 {
			if armed { // the Stop's own question: the next request's hook runs to its end before the answer comes back
				armed = false
				runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": overtakenNext})
			}
			return .1
		})
		armed = true
		out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
		if armed {
			t.Fatal("premise: the Stop asked the model")
		}
		requireSilentStop(t, out)
		requireNextRequestKept(t, root, first)
		for _, id := range []string{"done-as-asked", "changes-in-scope"} {
			if b, _ := os.ReadFile(filepath.Join(root, "records", id+".jsonl")); strings.TrimSpace(string(b)) != "" {
				t.Errorf("the overtaken Stop must not keep a %s judgment: %s", id, b)
			}
		}
	})
}

// The halt stays for what it was written for: the record this Stop read is gone when it takes the lock (no later request wrote a
// turn of its own), so the required work cannot be verified and the turn ends as incomplete.
func TestStopStillHaltsWhenItsRecordIsLostAtTheLock(t *testing.T) {
	repo, root, _ := overtakenTurn(t, func(string) float64 { return .1 })
	ran := atTheStopsLock(t, root, func() {
		if err := os.Remove(sessionPath(root, "s")); err != nil {
			t.Error(err)
		}
	})
	out := runHook(t, "stop", map[string]any{"session_id": "s", "cwd": repo, "last_assistant_message": "explained"})
	if !ran() {
		t.Fatal("premise: the Stop found the session lock held")
	}
	requireRecoveryHalt(t, out)
}
