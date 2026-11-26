package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hangingGit puts a git in front of the real one that never answers: every call is written down by the stub, then becomes a `sleep`
// that only the limit of the caller ends. A call whose arguments contain `answered` (when it is not empty) goes to the real git instead.
// It returns the function that gives the processes of those calls that are still running (the test ends with none left: a call that ran
// out of time is killed, not abandoned).
// shippedHookGitTimeout is the limit a real hook gives git (hookGitTimeout before any test changes it).
var shippedHookGitTimeout = hookGitTimeout

// hungGitLimit is the limit the tests of a git that never answers give it: a third of the limit a real hook has (1 s of 3 s). What they
// judge is how many calls git got, so the limit only has to outlast the start of the stub that writes a call down: a shell script that
// writeStub has already run once (the slow first run of a new file is paid there, outside any limit), which starts in about 3 ms alone and
// about 10 ms on a machine that is running other tests. A third of the real limit is a hundred times the slower of the two, and a
// git that never answers is waited for this long once per event (a real hook's 3 s made the test take 15 s).
var hungGitLimit = shippedHookGitTimeout / 3

func hangingGit(t *testing.T, answered string) (stillRunning func() []int) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	bin, pids := filepath.Join(dir, "bin"), filepath.Join(dir, "pids")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// $$ is the pid of the sleep too: exec replaces the shell, so the process the hook kills is the one that is written down
	script := ""
	if answered != "" {
		script += "case \"$*\" in *'" + answered + "'*) exec '" + real + "' \"$@\";; esac\n"
	}
	script += "echo $$ >> '" + pids + "'\nexec sleep 60\n"
	writeStub(t, bin, "git", script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	stillRunning = func() []int {
		raw, _ := os.ReadFile(pids)
		var alive []int
		for _, f := range strings.Fields(string(raw)) {
			if pid, err := strconv.Atoi(f); err == nil && syscall.Kill(pid, 0) == nil {
				alive = append(alive, pid)
			}
		}
		return alive
	}
	t.Cleanup(func() {
		for _, pid := range stillRunning() {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return stillRunning
}

// A git that never answers used to be waited for by every step of an event that needed the place of the repository, one after another,
// each with a limit of its own (found on 2026-11-21: 15 s for a request, as long as the time the agents give the hook), and then by the
// two questions asked alone when the place is not known (the exclude file, HEAD). The time an event spends waiting for questions that get
// no answer is now hookGitTimeout in all, so git is started once in all: for the question about where the repository is. The limit is
// the subject here, so it is set after hookEnv made git patient. It is hungGitLimit, a third of the limit a real hook has, which is still
// a hundred times what the stub that writes the call down needs to start: a limit of a few milliseconds killed that shell before it wrote
// anything on a loaded machine (the other test groups of make test), so a call the hook made was not counted. What is judged is the
// number of calls git got, not how long they took.
func TestAGitThatNeverAnswersIsWaitedForOnceInAnEvent(t *testing.T) {
	const place = "rev-parse --show-toplevel --git-path objects"
	// The two stubs are written once for all the events (a new file is slow to run the first time on macOS, which writeStub pays when it
	// writes one) and are on PATH only while an event runs: the fixture of an event asks the real git. The log of calls is emptied by every
	// reading, and a process of an earlier event that was left running would be found by the check at the end of any later one.
	cleanPath := os.Getenv("PATH")
	stillRunning := hangingGit(t, "")
	calls := gitCallLog(t)
	stubbedPath := os.Getenv("PATH")
	t.Setenv("PATH", cleanPath)
	for _, event := range []string{"user-prompt-submit", "stop", "session-start", "tool-lesson", "subagent-start"} {
		t.Run(event, func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			hookGitTimeout = hungGitLimit // patientGit's cleanup puts the real value back
			t.Chdir(repo)
			forgetKeptPlaces(t)
			t.Setenv("PATH", stubbedPath)
			in := map[string]any{"session_id": "never-" + event, "cwd": repo, "prompt": planningTestRequest, "last_assistant_message": "Done.",
				"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}
			if event == "subagent-start" {
				in["agent_id"], in["agent_type"] = "a1", "general-purpose"
			}
			if event == "stop" {
				runUnreviewedHook(t, event, in)
			} else {
				runManualHook(t, event, in)
			}
			if got := calls(); len(got) != 1 || !strings.HasPrefix(got[0], place) {
				t.Errorf("%s started git %d times while git never answered, want once, for where the repository is; the git calls:\n%s", event, len(got), strings.Join(got, "\n"))
			}
			if left := stillRunning(); len(left) != 0 {
				t.Errorf("%s left git running: %v", event, left)
			}
		})
	}
}

// What an event may wait for is what is left of hookGitTimeout, not what a first question without an answer was given: a question that
// failed at once (git could not be started, or the caller's own limit was over) used up nothing, so the next step still asks.
func TestAQuestionThatFailedAtOnceLeavesTheTimeForTheNextOne(t *testing.T) {
	repo := gitRepo(t)
	calls := gitCallLog(t)
	defer beginEventGit()()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := gitPlaceAt(expired, repo); ok {
		t.Fatal("premise: a question given a limit that is over gets no answer")
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("premise: no git is started for a limit that is over, started: %v", got)
	}
	place, ok := gitPlaceAt(context.Background(), repo)
	if !ok || feedbackScope(place.top) != feedbackScope(repo) {
		t.Fatalf("the next question was not asked: top %q, ok %v (want %s)", place.top, ok, repo)
	}
	if got := calls(); len(got) != 1 {
		t.Errorf("the next question started %d git calls, want 1: %v", len(got), got)
	}
}

// The time of a question that was answered is not counted: a later question about another folder is not refused because the event has been
// running for long. The time of a question without an answer is, and the last tenth of the limit is not left to a question: nothing is
// heard from git in it, and what remains after a caller's own limit ran out (it started a little before the question) must not start
// another process.
func TestOnlyQuestionsWithoutAnAnswerUseUpTheTimeAnEventMayWait(t *testing.T) {
	m := &placeMemo{at: map[string]placeAnswer{}}
	limit := hookGitTimeout
	if left, ok := m.timeLeft(); !ok || left != limit {
		t.Fatalf("a new event may wait %v (ok %v), want %v", left, ok, limit)
	}
	// an answer that took some time (2 ms, which no clock reads as none; "no" is an answer too, run says only that git answered): nothing is spent
	if !m.ask(context.Background(), func(context.Context) bool { time.Sleep(2 * time.Millisecond); return true }) {
		t.Fatal("a new event was refused a question")
	}
	if left, ok := m.timeLeft(); !ok || left != limit {
		t.Errorf("after answers the event may wait %v (ok %v), want all of %v", left, ok, limit)
	}
	m.spend(limit / 2) // half the limit waited for nothing
	if left, ok := m.timeLeft(); !ok || left != limit/2 {
		t.Errorf("after half the limit waited for nothing the event may wait %v (ok %v), want %v", left, ok, limit/2)
	}
	var cut time.Duration // the limit the next question is given: what is left
	if !m.ask(context.Background(), func(ctx context.Context) bool {
		d, _ := ctx.Deadline()
		cut = time.Until(d)
		return true
	}) || cut > limit/2 {
		t.Errorf("the question was given %v, want at most what is left, %v", cut, limit/2)
	}
	m.spend(limit/2 - limit/100) // a hundredth of the limit is left
	started := false
	if m.ask(context.Background(), func(context.Context) bool { started = true; return true }) || started {
		t.Errorf("with %v left of %v the event may still ask git", limit/100, limit)
	}
}

// Where the working tree is not known because git said there is none (a bare repository: the answer of the place question is "no"), the
// two questions that ask alone are asked as before: that is an answer, and it costs the event no time. A git that does not answer them
// is waited for in the time of the event, once: the second of the two is not asked when the first waited the whole of it. (The place is
// asked first by itself, so that the limit HEAD is given starts whole; a limit that began before the place was answered is shorter by what
// that took, and the exclude file may then be asked in the rest.) The place question is not what is judged, and the speed of a real git is
// not either: its answer is waited for with patientGit's limit, and the stub that gives it comes after more than the whole limit these
// tests give a git that never answers (hungGitLimit; found on 2026-11-23: under make test the real git was slow, the place got no answer
// in the three seconds a real hook gives it, that used up the whole time of the event, and HEAD was not asked).
func TestWhereGitSaysThereIsNoWorkingTreeTheTwoQuestionsAreAskedAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	lateAnswer := hungGitLimit + 500*time.Millisecond // a git so slow that the limit set below is over before it says where the repository is
	base := t.TempDir()
	src, bare := filepath.Join(base, "src"), filepath.Join(base, "bare.git")
	newRepoAt(t, src)
	commitIn(t, src, "a")
	gitAt(t, base, "clone", "-q", "--bare", src, bare)
	head := realHead(t, bare)
	if head == "" {
		t.Fatal("premise: the bare repository has a commit")
	}
	const excluded = "\n/.shikii/\n"
	t.Run("answered", func(t *testing.T) {
		patientGit(t)
		slowFirstPlaceGit(t, lateAnswer)
		calls := gitCallLog(t)
		var got string
		var err error
		event(func() {
			got = gitHead(bare)
			err = excludeProjectStore(bare)
		})
		if err != nil || got != head {
			t.Errorf("HEAD of the bare repository is %q (error %v), git says %q", got, err, head)
		}
		if raw, _ := os.ReadFile(filepath.Join(bare, "info", "exclude")); !strings.Contains("\n"+string(raw), excluded) {
			t.Errorf("the exclude file of the bare repository was not written: %q", raw)
		}
		if c := calls(); len(c) != 3 || c[0] != askedPlace || c[1] != "rev-parse HEAD" || c[2] != "rev-parse --git-path info/exclude" {
			t.Errorf("want the place, then HEAD, then the exclude file asked alone; the git calls:\n%s", strings.Join(c, "\n"))
		}
	})
	t.Run("not answered", func(t *testing.T) {
		patientGit(t) // its cleanup puts the real value back after the limit is set below
		slowFirstPlaceGit(t, lateAnswer)
		stillRunning := hangingGit(t, "--show-toplevel") // the place is answered (no), nothing asked alone is
		calls := gitCallLog(t)
		event(func() {
			// the place is answered, "no", before the limit is set (the real git is slow at times, and the stub above is slower than
			// that limit): the limit of the questions after it then starts whole
			gitPlaceAt(context.Background(), bare)
			hookGitTimeout = hungGitLimit
			gitHead(bare)
			excludeProjectStore(bare)
		})
		if c := calls(); len(c) != 2 || c[0] != askedPlace || c[1] != "rev-parse HEAD" {
			t.Errorf("want the place and HEAD asked, and the exclude file not (HEAD got no answer in all the time there is); the git calls:\n%s", strings.Join(c, "\n"))
		}
		if left := stillRunning(); len(left) != 0 {
			t.Errorf("git was left running: %v", left)
		}
	})
}
