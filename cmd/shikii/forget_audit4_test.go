package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The fourth audit of 0.41.0: forget took sessions that were at work. The agent is open, no process of the session runs under it, and the
// state of the turn says nothing is open, yet the session works: a subagent in the background thinks inside the agent's process, a turn
// goes on after a Stop, and a request can begin while forget runs.

// turnEnds ends the session's turn the way the hook does at a Stop that lets it end.
func (w *forgetWorld) turnEnds(id string) {
	w.t.Helper()
	sessionTurnEvent("stop", &hookInput{SessionID: id}, sessionTurnAt(id), false)
}

// hears gives the hook of the session an event of the agent (agent is the id of the subagent the event is of, "" for the agent itself).
func (w *forgetWorld) hears(id, event, agent string) {
	w.t.Helper()
	sessionTurnEvent(event, &hookInput{SessionID: id, AgentID: agent}, "", false)
}

// A subagent that works in the background goes on after the Stop of the parent's turn, in the agent's own process: no process under the
// agent says so. The session is at work while one is open.
func TestForgetRefusesASessionWhileASubagentItStartedIsOpen(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	noteSessionRequest(w.A, w.root, "t2")
	w.hears(w.A, "subagent-start", "agent-x")
	w.turnEnds(w.A) // the turn ends; agent-x goes on
	w.refuses(w.A, "ses_fx_a1 is still running: its agent process (claude, pid 910001) is alive and a subagent it started is still open (agent-x, begun ")
	if w.table.listed != 0 {
		t.Errorf("the list of processes was read for a session whose subagent is open: %d times", w.table.listed)
	}
	// the subagent ends: its report wakes the agent for a turn (the end reopens the turn), and the Stop of that turn closes it
	w.hears(w.A, "subagent-stop", "agent-x")
	w.refuses(w.A, "ses_fx_a1 is still running", "its last turn has not ended")
	w.turnEnds(w.A)
	w.takes(w.A)
}

func TestForgetLooksAtTheSubagentsOfTheSession(t *testing.T) {
	t.Run("a subagent that began before the agent was noted belongs to an earlier process", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		updateSessionLatest(w.A, func(l *sessionLatest, _ bool) bool {
			l.Subagents = map[string]time.Time{"agent-x": w.old.Add(-time.Hour)}
			return true
		})
		w.takes(w.A)
	})
	t.Run("a subagent that began after the agent was noted counts", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		updateSessionLatest(w.A, func(l *sessionLatest, _ bool) bool {
			l.Subagents = map[string]time.Time{"agent-x": w.old.Add(time.Minute)}
			return true
		})
		w.refuses(w.A, "a subagent it started is still open (agent-x, begun ")
	})
	t.Run("the oldest of several is named", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		updateSessionLatest(w.A, func(l *sessionLatest, _ bool) bool {
			l.Subagents = map[string]time.Time{"agent-late": w.old.Add(2 * time.Hour), "agent-first": w.old.Add(time.Hour)}
			return true
		})
		w.refuses(w.A, "(agent-first, begun ")
	})
	t.Run("a new request leaves the subagent open", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		w.hears(w.A, "subagent-start", "agent-x")
		noteSessionRequest(w.A, w.root, "t2")
		w.turnEnds(w.A)
		w.refuses(w.A, "a subagent it started is still open (agent-x")
	})
	t.Run("the id of a subagent is judged by the session it ran under", func(t *testing.T) {
		w := newForgetWorld(t)
		w.idle(w.A)
		w.hears(w.A, "subagent-start", "agent-x")
		w.turnEnds(w.A)
		w.refuses(w.childA, w.childA+" (a subagent that ran under the session "+w.A+") is still running", "a subagent it started is still open (agent-x")
	})
}

// After a Stop the turn may go on (another Stop hook asked for more, a subagent's report woke the agent, the agent works on a notice):
// the events of the session say so, and forget refuses until the Stop of that turn.
func TestForgetRefusesASessionWhoseTurnWentOnAfterItsStop(t *testing.T) {
	for _, c := range []struct {
		name, event, agent string
		opensASubagent     bool // the event carries the id of a subagent no end was told of: it is open until its end
	}{
		{"a tool call", "tool-lesson", "", false},
		{"a tool call of a subagent", "tool-lesson", "agent-x", true},
		{"a subagent ending", "subagent-stop", "agent-x", false},
		{"the end of a subagent no id is told of", "subagent-stop", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			w.idle(w.A)
			w.hears(w.A, c.event, c.agent)
			w.refuses(w.A, "ses_fx_a1 is still running: its agent process (claude, pid 910001) is alive and its last turn has not ended")
			w.turnEnds(w.A)
			if c.opensASubagent {
				w.refuses(w.A, "a subagent it started is still open ("+c.agent+", begun ")
				w.hears(w.A, "subagent-stop", c.agent)
				w.turnEnds(w.A)
			}
			w.takes(w.A)
		})
	}
}

// A state whose turn is closed but that holds an event newer than the Stop that closed it is not one a Stop ended: forget refuses it,
// whatever wrote it.
func TestForgetRefusesWhenAnEventCameAfterTheStopThatClosedTheTurn(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	if l := latestOfSession(t, w.A); l.ClosedAt.IsZero() || *l.InTurn {
		t.Fatalf("precondition: the Stop closed the turn at a time: %+v", l)
	}
	updateSessionLatest(w.A, func(l *sessionLatest, _ bool) bool { l.EventAt = l.ClosedAt.Add(time.Second); return true })
	w.refuses(w.A, "ses_fx_a1 is still running: its agent process (claude, pid 910001) is alive and an event of the session came after the Stop that ended its last turn")
	if w.table.listed != 0 {
		t.Errorf("the list of processes was read for a session that had an event after its Stop: %d times", w.table.listed)
	}
}

// What forget plans is judged once, and the request that begins after it is not what the person agreed to remove. Each step looks at
// the state of the sessions again before it runs and stops when it is not what the plan went by, and the last step (which moves the
// record and the state of the turn) looks again under the lock that holds the state.
func TestForgetStopsWhenTheSessionBeginsAgainWhileItRuns(t *testing.T) {
	begins := map[string]func(w *forgetWorld){
		"a request":                func(w *forgetWorld) { noteSessionRequest(w.A, w.root, "t-new") },
		"a subagent beginning":     func(w *forgetWorld) { w.hears(w.A, "subagent-start", "agent-x") },
		"a tool call of the agent": func(w *forgetWorld) { w.hears(w.A, "tool-lesson", "") },
	}
	for name, begin := range begins {
		for _, at := range []string{"guards", "agent"} { // a step that is run before the last one, and the step right before it
			t.Run(name+" after the step "+at, func(t *testing.T) {
				w := newForgetWorld(t)
				w.idle(w.A)
				old := forgetAfterStep
				forgetAfterStep = func(done string) error {
					if done == at {
						begin(w)
					}
					return nil
				}
				t.Cleanup(func() { forgetAfterStep = old })
				out, err := forgetCommand(t, w.A, "--apply")
				var stopped *forgetStopped
				if !errors.As(err, &stopped) || !errors.Is(err, errForgetTurnChanged) {
					t.Fatalf("forget went on after the session began again: %v\n%s", err, out)
				}
				// the stop is at the step after the one the session began something in (a step is not run on a state it was not planned for)
				want := map[string][]string{"guards": {"agent", "sessions"}, "agent": {"sessions"}}[at]
				if !stopped.moved || !slices.Equal(stopped.left, want) {
					t.Errorf("the steps before the stop moved things, and the steps from %v on were left: %+v", want, stopped)
				}
				if !strings.Contains(err.Error(), "When the session is idle again, running the same command again removes the rest") {
					t.Errorf("a run that moved something says the rest is for a time the session is idle: %v", err)
				}
				if !w.fileExists("sessions/" + w.A + ".json") {
					t.Errorf("the record of the session went though it began again")
				}
				if _, known, _ := readSessionLatest(w.A); !known {
					t.Errorf("the state of the session's turn went though it began again")
				}
			})
		}
	}
}

// The last step looks at the state under its own lock: a request that begins right before it (after every step before it had been
// looked at) still stops it, with the record and the state of the turn in place.
func TestForgetsLastStepLooksAtTheStateUnderItsLock(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	old := forgetBeforeStep
	forgetBeforeStep = func(step string) {
		if step == "sessions" {
			noteSessionRequest(w.A, w.root, "t-new")
		}
	}
	t.Cleanup(func() { forgetBeforeStep = old })
	out, err := forgetCommand(t, w.A, "--apply")
	var stopped *forgetStopped
	if !errors.As(err, &stopped) || !errors.Is(err, errForgetTurnChanged) || !strings.HasPrefix(err.Error(), "sessions: ") {
		t.Fatalf("the last step did not stop at the state it was not planned for: %v\n%s", err, out)
	}
	if !w.fileExists("sessions/"+w.A+".json") || latestOfSession(t, w.A).Turn != "t-new" {
		t.Errorf("the record or the new turn of the session was moved")
	}
}

// A request that begins before the first step has moved nothing: the run says it changed nothing and leaves no folder.
func TestForgetThatStopsBeforeMovingAnythingBecauseTheSessionBeganAgainChangesNothing(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	before := w.snapshot()
	old := forgetBeforeStep
	forgetBeforeStep = func(step string) {
		if step == "decisions" {
			noteSessionRequest(w.A, w.root, "t-new")
		}
	}
	t.Cleanup(func() { forgetBeforeStep = old })
	out, err := forgetCommand(t, w.A, "--apply")
	var stopped *forgetStopped
	if !errors.As(err, &stopped) || !errors.Is(err, errForgetTurnChanged) {
		t.Fatalf("forget went on after the session began again: %v\n%s", err, out)
	}
	if stopped.moved || !strings.Contains(err.Error(), "nothing was removed") {
		t.Errorf("nothing was moved, and the run says so: %v", err)
	}
	if kept, _ := filepath.Glob(filepath.Join(w.root, "records", "forgotten", "*")); len(kept) != 0 {
		t.Errorf("a run that moved nothing left a folder: %v", kept)
	}
	after := w.snapshot()
	delete(before, "home/agent/latest/"+w.A+".json") // the new request wrote it
	delete(after, "home/agent/latest/"+w.A+".json")
	if diff := withoutEmptyLocks(forgetDiff(before, after)); len(diff) != 0 {
		t.Errorf("a run that stopped before moving anything changed files: %v", diff)
	}
	if _, err := os.Stat(filepath.Join(w.root, "sessions", w.A+".json")); err != nil {
		t.Errorf("the record of the session is gone: %v", err)
	}
}

// Over MCP, a session that begins something after the person said yes stops the removal like any other step that cannot go on: the
// reply says partial (what was moved is kept) and that the rest is for a time the session is idle, and the new request is left as it is.
func TestMCPForgetSaysPartialAndWhenToTryAgainWhenTheSessionBeganAgain(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	old := forgetAfterStep
	forgetAfterStep = func(done string) error {
		if done == "guards" {
			noteSessionRequest(w.A, w.root, "t-new")
		}
		return nil
	}
	t.Cleanup(func() { forgetAfterStep = old })
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	c.send(answerTo(c.question(), accept(true)))
	failed, body := c.reply(2)
	message, _ := body["message"].(string)
	if !failed || body["status"] != "partial" || body["changed"] != true {
		t.Fatalf("a removal that the session's new request stopped did not say partial and changed: %v", body)
	}
	for _, want := range []string{"is at work again", "The session is at work again: send the same call again to remove the rest once it is idle"} {
		if !strings.Contains(message, want) {
			t.Errorf("the message does not say %q: %s", want, message)
		}
	}
	c.finish()
	if latestOfSession(t, w.A).Turn != "t-new" || !w.fileExists("sessions/"+w.A+".json") {
		t.Errorf("the new request of the session was taken")
	}
}
