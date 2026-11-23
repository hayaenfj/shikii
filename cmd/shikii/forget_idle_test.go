package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A session whose agent is still open is not always at work (forget_idle.go): forget takes it when its last turn has ended and no
// process under the agent that started after the session was noted still runs. The table of processes is the test's own; the machine's
// is never read and no process is started.

const (
	fxPidSelf  = 910100 // the process that runs forget
	fxPidShell = 910101 // the shell that started it
	fxPidOther = 910102 // the agent of the session that runs it, not the agent of the session forgotten
	fxPidPS    = 910103 // what forget started to read the list
	fxPidJob   = 910200
	fxPidDeep  = 910201
)

// useThisPID makes the pid of the process that runs forget the given one.
func useThisPID(t *testing.T, pid int) {
	t.Helper()
	old := thisPID
	thisPID = func() int { return pid }
	t.Cleanup(func() { thisPID = old })
}

// idle makes the agent of the session open and its last turn ended, and gives the list the agents of the world and the process that
// runs forget (under another agent, as a person's own command is), with the processes in more.
func (w *forgetWorld) idle(id string, more ...processEntry) {
	w.t.Helper()
	w.runs(id)
	w.endsRequest(id)
	useThisPID(w.t, fxPidSelf)
	long := w.old.Add(-time.Hour)
	w.table.list = append([]processEntry{
		{PID: fxPidA, Parent: 1, Started: long, Name: "claude"},
		{PID: fxPidB, Parent: 1, Started: long, Name: "claude"},
		{PID: fxPidOther, Parent: 1, Started: long, Name: "claude"},
		{PID: fxPidShell, Parent: fxPidOther, Started: long, Name: "zsh"},
		{PID: fxPidSelf, Parent: fxPidShell, Started: time.Now(), Name: "shikii"},
	}, more...)
}

// takes runs forget on the session and says what it did: the project must be left as a forget leaves it.
func (w *forgetWorld) takes(id string) {
	w.t.Helper()
	before := w.snapshot()
	out, err := forgetCommand(w.t, id, "--apply")
	if err != nil {
		w.t.Fatalf("an idle session was refused: %v\n%s", err, out)
	}
	if problems := w.problems(before); len(problems) > 0 {
		w.t.Errorf("forget left the project wrong:\n  %s", strings.Join(problems, "\n  "))
	}
	if w.fileExists("sessions/" + id + ".json") {
		w.t.Errorf("the record of the session is still there")
	}
}

// refuses runs forget on the session and wants a refusal that holds every text of want, with the project and the home left as they were.
func (w *forgetWorld) refuses(id string, want ...string) {
	w.t.Helper()
	before := w.snapshot()
	out, err := forgetCommand(w.t, id, "--apply")
	if err == nil {
		w.t.Fatalf("the session was taken:\n%s", out)
	}
	for _, text := range want {
		if !strings.Contains(err.Error(), text) {
			w.t.Errorf("the refusal does not say %q: %v", text, err)
		}
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		w.t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
	}
}

// The agent is open and its last turn has ended: nothing else says the session works, so forget takes it.
func TestForgetTakesAnIdleSessionWhoseAgentIsStillOpen(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	w.takes(w.A)
}

// The agent is open and the last turn has not ended (no Stop came after it began): the session is thinking, whatever its records say.
func TestForgetRefusesASessionWhoseLastRequestHasNotEnded(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	noteSessionRequest(w.A, w.root, w.tA) // a request began: its Stop has not come
	w.refuses(w.A, "ses_fx_a1 is still running: its agent process (claude, pid 910001) is alive and its last turn has not ended")
	if w.table.listed != 0 {
		t.Errorf("the list of processes was read for a session whose request is still under way: %d times", w.table.listed)
	}
}

// A slash command is no request, but the agent works on it until its Stop: a session whose agent is running one is thinking, though
// its last request ended before. After the Stop of the command it is idle again.
func TestForgetRefusesASessionWhoseAgentIsRunningASlashCommand(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	sessionTurnEvent("user-prompt-submit", &hookInput{SessionID: w.A, Prompt: "/commit fix the docs"}, "", false)
	if latest, _, _ := readSessionLatest(w.A); !latest.Ended {
		t.Fatalf("precondition: the request before the command had ended: %+v", latest)
	}
	w.refuses(w.A, "ses_fx_a1 is still running: its agent process (claude, pid 910001) is alive and its last turn has not ended")
	if w.table.listed != 0 {
		t.Errorf("the list of processes was read for a session that is in a turn: %d times", w.table.listed)
	}
	w.endsRequest(w.A) // the Stop of the command
	w.takes(w.A)
}

// What the person did not end with a Stop (an interrupted request) and what an older shikii kept are not "ended".
func TestForgetRefusesWhenTheEndOfTheLastRequestIsNotKnown(t *testing.T) {
	latest := func(id string) string {
		home, _ := os.UserHomeDir() // the home of the test's world, which is set once it is built
		return filepath.Join(home, ".shikii", "agent", "latest", id+".json")
	}
	for _, c := range []struct {
		name   string
		setup  func(t *testing.T, w *forgetWorld)
		reason string
	}{
		{"a state an older shikii wrote, with neither field", func(t *testing.T, w *forgetWorld) {
			w.must(os.WriteFile(latest(w.A), []byte(`{"root":`+strconv.Quote(w.root)+`,"turn":"`+w.tA+`"}`), 0o600))
		}, "its last turn was kept by an older shikii, which did not note whether it ended"},
		{"a state with the end of the request but not whether a turn is open", func(t *testing.T, w *forgetWorld) {
			w.must(os.WriteFile(latest(w.A), []byte(`{"root":`+strconv.Quote(w.root)+`,"turn":"`+w.tA+`","ended":true}`), 0o600))
		}, "its last turn was kept by an older shikii, which did not note whether it ended"},
		{"no state of the turn at all", func(t *testing.T, w *forgetWorld) {
			w.must(os.Remove(latest(w.A)))
		}, "no state of its last turn is kept: it is from an older shikii"},
		{"a state that cannot be read", func(t *testing.T, w *forgetWorld) {
			w.must(os.WriteFile(latest(w.A), []byte(`{"root":`), 0o600))
		}, "the state of its last turn could not be read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			w.idle(w.A)
			c.setup(t, w)
			w.refuses(w.A, "ses_fx_a1 ", "its agent process (claude, pid 910001) is alive", c.reason)
			if w.table.listed != 0 {
				t.Errorf("the list of processes was read for a session whose request is not known to have ended: %d times", w.table.listed)
			}
		})
	}
}

// A record that names the process without the time it was noted at (an earlier shikii wrote it) cannot tell the agent's own processes
// from the session's: forget refuses, and does not read the list.
func TestForgetRefusesWhenTheRecordDoesNotSayWhenTheAgentWasNoted(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	p := map[string]any{}
	for k, v := range w.process[w.A] {
		p[k] = v
	}
	delete(p, "noted")
	w.setProcess(w.A, p)
	w.refuses(w.A, "its agent process (claude, pid 910001) is alive", "does not say when the agent was first seen running it")
	if w.table.listed != 0 {
		t.Errorf("the list of processes was read with no time to compare it with: %d times", w.table.listed)
	}
}

// Whether a process under the agent counts is decided by when it started against when the agent was noted, and by where it stands.
func TestForgetLooksAtWhatStartedUnderTheAgentAfterTheSession(t *testing.T) {
	for _, c := range []struct {
		name   string
		more   func(w *forgetWorld, agent int) []processEntry
		refuse string // what the refusal names: empty when forget takes the session
	}{
		{"nothing runs under the agent", func(w *forgetWorld, agent int) []processEntry { return nil }, ""},
		{"a process the agent started before the session was noted", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{{PID: fxPidJob, Parent: agent, Started: w.old.Add(-10 * time.Second), Name: "mcp-server"}}
		}, ""},
		{"a process that started in the second the agent was noted in", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{{PID: fxPidJob, Parent: agent, Started: w.old.Truncate(time.Second), Name: "mcp-server"}}
		}, ""},
		{"a process that started in the next second", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{{PID: fxPidJob, Parent: agent, Started: w.old.Truncate(time.Second).Add(time.Second), Name: "go"}}
		}, "(go, pid 910200, started "},
		{"a test the session started an hour after", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{{PID: fxPidJob, Parent: agent, Started: w.old.Add(time.Hour), Name: "/usr/local/go/bin/go"}}
		}, "(go, pid 910200, started "},
		{"a process below one the agent started before the session", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{
				{PID: fxPidJob, Parent: agent, Started: w.old.Add(-time.Minute), Name: "zsh"},
				{PID: fxPidDeep, Parent: fxPidJob, Started: w.old.Add(time.Hour), Name: "node"},
			}
		}, "(node, pid 910201, started "},
		{"a process after the session, under another agent", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{{PID: fxPidJob, Parent: fxPidOther, Started: w.old.Add(time.Hour), Name: "go"}}
		}, ""},
		{"the oldest of the processes started after is the one named", func(w *forgetWorld, agent int) []processEntry {
			return []processEntry{
				{PID: fxPidDeep, Parent: agent, Started: w.old.Add(2 * time.Hour), Name: "later"},
				{PID: fxPidJob, Parent: agent, Started: w.old.Add(time.Hour), Name: "earlier"},
			}
		}, "(earlier, pid 910200, started "},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			w.idle(w.A, c.more(w, fxPidA)...)
			if c.refuse == "" {
				w.takes(w.A)
				return
			}
			w.refuses(w.A, "ses_fx_a1 is still running: its agent process (claude, pid 910001) is alive and a process under it that started after the session did is running", c.refuse)
		})
	}
}

// forget itself, what it started to read the list, and everything above it (the shell, the agent that runs it) are not the session's
// even when they started after it and stand under its agent: forget runs under the agent when a person's agent runs it, or when it is
// the MCP server the agent started.
func TestForgetDoesNotCountItselfOrWhatIsAboveItUnderTheAgent(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	after := w.old.Add(time.Hour)
	w.table.list = append(w.table.list[:3:3], // the agents; the chain of the world is replaced by one under the agent of the session
		processEntry{PID: fxPidShell, Parent: fxPidA, Started: after, Name: "zsh"},
		processEntry{PID: fxPidSelf, Parent: fxPidShell, Started: after, Name: "shikii"},
		processEntry{PID: fxPidPS, Parent: fxPidSelf, Started: after, Name: "ps"})
	w.takes(w.A)
}

// The same list, with a process that is neither forget nor above it beside forget under the agent: that one counts.
func TestForgetCountsAProcessBesideItselfUnderTheAgent(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	after := w.old.Add(time.Hour)
	w.table.list = append(w.table.list[:3:3],
		processEntry{PID: fxPidShell, Parent: fxPidA, Started: after, Name: "zsh"},
		processEntry{PID: fxPidSelf, Parent: fxPidShell, Started: after, Name: "shikii"},
		processEntry{PID: fxPidJob, Parent: fxPidShell, Started: after, Name: "go"})
	w.refuses(w.A, "(go, pid 910200, started ")
}

// A list that cannot be read says nothing of what runs: forget refuses rather than take a session that may be working.
func TestForgetRefusesWhenTheListOfProcessesCannotBeRead(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	w.table.listErr = os.ErrPermission
	w.refuses(w.A, "ses_fx_a1 may still be running: its agent process (claude, pid 910001) is alive", "the list of processes could not be read")
	w.table.listErr = nil
	w.takes(w.A)
}

// The list is read once for a listing, whatever the number of sessions asked for, and once for each pass of an application (the plan, the
// plan again under the sessions' locks, and the removal).
func TestForgetReadsTheListOfProcessesOncePerPass(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	w.runs(w.B)
	w.endsRequest(w.B)
	if out, err := forgetCommand(t, w.A, w.B); err != nil {
		t.Fatalf("the listing of two idle sessions failed: %v\n%s", err, out)
	}
	if w.table.listed != 1 {
		t.Errorf("the list was read %d times for the listing of two sessions", w.table.listed)
	}
	w.table.listed = 0
	if out, err := forgetCommand(t, w.A, w.B, "--apply"); err != nil {
		t.Fatalf("the application to two idle sessions failed: %v\n%s", err, out)
	}
	if w.table.listed != 3 {
		t.Errorf("the list was read %d times for the three passes of an application to two sessions", w.table.listed)
	}
}

// A subagent's id is judged by the session it ran under: its request, its agent and the processes under that agent.
func TestForgetJudgesASubagentIdByTheIdleStateOfItsSession(t *testing.T) {
	t.Run("the session is idle", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		out, err := forgetCommand(t, w.childA, "--apply")
		if err != nil {
			t.Fatalf("a subagent of an idle session was refused: %v\n%s", err, out)
		}
	})
	t.Run("a process runs under the agent of the session", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A, processEntry{PID: fxPidJob, Parent: fxPidA, Started: w.old.Add(time.Hour), Name: "go"})
		w.refuses(w.childA, w.childA+" (a subagent that ran under the session "+w.A+") is still running: its agent process (claude, pid 910001) is alive", "(go, pid 910200, started ")
	})
	t.Run("the request of the session has not ended", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		noteSessionRequest(w.A, w.root, w.tA)
		w.refuses(w.childA, w.childA+" (a subagent that ran under the session "+w.A+") is still running", "its last turn has not ended")
	})
}

// After a session was forgotten its person may come back to it: the hook finds no record and starts the session again, as for a session it
// never saw. Nothing the forget kept is touched, and the session is noted anew (its agent, and the time that agent was noted at).
func TestAHookOfAForgottenSessionStartsItAgain(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	forgotten := time.Now()
	w.takes(w.A)
	kept, err := filepath.Glob(filepath.Join(w.root, "records", "forgotten", "*", "plan.json"))
	if err != nil || len(kept) != 1 {
		t.Fatalf("precondition: forget kept what it removed: %v %v", kept, err)
	}
	keptBefore := forgetRead(t, kept[0])
	// the agent runs the session again: the hook of its next request
	for pid, row := range agentFamily("claude") {
		w.table.set(pid, row)
	}
	useParent(t, 500)
	in := map[string]any{"session_id": w.A, "cwd": w.repo, "prompt": "Explain how the page lists waiting answers."}
	runHook(t, "user-prompt-submit", in)
	s, err := readSession(w.root, w.A)
	if err != nil {
		t.Fatalf("the hook did not start the session again: %v", err)
	}
	if s.TurnID == "" || s.TurnID == w.tA || s.Previous != "" || s.Request == "" {
		t.Errorf("the record of the session is not a new one: turn %q previous %q request %q", s.TurnID, s.Previous, s.Request)
	}
	if s.Process == nil || s.Process.PID != 200 || s.Process.Noted.Before(forgotten) {
		t.Errorf("the session was not noted with its agent after the forget: %+v", s.Process)
	}
	if latest, known, err := readSessionLatest(w.A); err != nil || !known || latest.Ended || latest.Turn != s.TurnID {
		t.Errorf("the state of the turn is not that of the new request: %+v %v %v", latest, known, err)
	}
	if !reflect.DeepEqual(keptBefore, forgetRead(t, kept[0])) {
		t.Errorf("the hook changed what forget kept")
	}
}

// A process table that does not show the process that asks is of another place (an empty /proc, another pid namespace): it says "not
// there" of every pid, and that is not a reason to read a session as ended. forget cannot tell, and goes by the time of the records.
func TestForgetDoesNotReadAnEmptyProcessTableAsASessionThatHasEnded(t *testing.T) {
	t.Run("the table of a /proc with nothing in it", func(t *testing.T) {
		w := newForgetWorld(t)
		empty := t.TempDir()
		readProcess = func(_ context.Context, pid int) (processRow, processState) { return readProcStat(empty, pid) }
		w.freshRecord(w.A, time.Now().UTC())
		w.refuses(w.A, "shikii cannot tell whether the session is still running (the process table of this machine does not show this very process")
	})
	t.Run("a table that shows nothing of the process that asks", func(t *testing.T) {
		w := newForgetWorld(t)
		w.table.blind = true
		w.freshRecord(w.A, time.Now().UTC())
		w.refuses(w.A, "(the process table of this machine does not show this very process, so it cannot say that another one has ended)")
		if len(w.table.asked) != 2 || w.table.asked[0] != fxPidA || w.table.asked[1] != os.Getpid() {
			t.Errorf("the table was asked about %v: the agent, and then the process that asks", w.table.asked)
		}
	})
	t.Run("a table that shows it: the process that is not there has ended", func(t *testing.T) {
		w := newForgetWorld(t)
		w.freshRecord(w.A, time.Now().UTC())
		w.takes(w.A)
	})
}

// The same for the list read to look under the agent: a list without this process is of another place, and says nothing of what runs.
func TestForgetRefusesWhenTheListOfProcessesDoesNotShowItself(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	var rest []processEntry
	for _, e := range w.table.list {
		if e.PID != fxPidSelf {
			rest = append(rest, e)
		}
	}
	w.table.list = rest
	w.refuses(w.A, "ses_fx_a1 may still be running", "the list of processes could not be read: the list does not show this very process (pid 910100)")
	w.table.list = append(rest, processEntry{PID: fxPidSelf, Parent: fxPidShell, Name: "shikii"}) // shown, with no start time
	w.refuses(w.A, "the list does not show this very process")
}

// A run that stopped before the last step is run again and judges the session by what it was judged by the first time: what says
// whether a session is running (its record, which names the agent process, and the state of its last turn) goes in the last step, and
// that step takes its lock before it moves anything. The agent is open and idle here, so a record that went early would make the second
// run refuse for 24 hours.
func TestForgetRunAgainAfterAStopJudgesAnIdleSessionAsBefore(t *testing.T) {
	steps := (&forgetRun{}).steps()
	for _, step := range steps[:len(steps)-1] {
		t.Run("a stop after "+step.name, func(t *testing.T) {
			w := newForgetWorld(t)
			w.idle(w.A)
			before := w.snapshot()
			old := forgetAfterStep
			forgetAfterStep = func(done string) error {
				if done == step.name {
					return errors.New("the disk went away")
				}
				return nil
			}
			out, err := forgetCommand(t, w.A, "--apply")
			forgetAfterStep = old
			if err == nil || !strings.Contains(err.Error(), "the disk went away") {
				t.Fatalf("the run was not stopped after %s: %v\n%s", step.name, err, out)
			}
			if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
				t.Fatalf("the second run was refused: %v\n%s", err, out)
			}
			if problems := w.problems(before); len(problems) > 0 {
				t.Errorf("after the stop and a second run the project is wrong:\n  %s", strings.Join(problems, "\n  "))
			}
		})
	}
	t.Run("the lock of the agents' folder of last turns is held by another", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		before := w.snapshot()
		home, _ := os.UserHomeDir()
		unlock, ok := lockPathWithin(filepath.Join(home, ".shikii", "agent", "latest", ".lock"), 0, nil)
		if !ok {
			t.Fatal("precondition: the test takes the lock")
		}
		out, err := forgetCommand(t, w.A, "--apply")
		if err == nil || !strings.Contains(err.Error(), "sessions: the lock of "+filepath.Join(home, ".shikii", "agent", "latest")+" is busy") {
			unlock()
			t.Fatalf("the step that removes the state of the last turn did not stop at its lock: %v\n%s", err, out)
		}
		if !w.fileExists("sessions/" + w.A + ".json") {
			t.Errorf("the record of the session went before the lock of the state of its last turn was taken")
		}
		unlock()
		if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
			t.Fatalf("the second run was refused: %v\n%s", err, out)
		}
		if problems := w.problems(before); len(problems) > 0 {
			t.Errorf("after the stop and a second run the project is wrong:\n  %s", strings.Join(problems, "\n  "))
		}
	})
}
