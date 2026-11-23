package main

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// What decides whether forget takes a session is the agent process its record names, not the time of its records. These tests give
// the records any age and any freshness, and change only the process.

// useRealProcessTable makes forget and the hook read this machine's process table, as in production (TestMain replaces it with a table
// that cannot say), and gives the test's own process: the one process that is certain to be alive.
func useRealProcessTable(t *testing.T) (agentProcess, processRow) {
	t.Helper()
	old := readProcess
	readProcess = readProcessRow
	t.Cleanup(func() { readProcess = old })
	ctx, cancel := context.WithTimeout(context.Background(), hookProcessTimeout)
	defer cancel()
	row, state := readProcessRow(ctx, os.Getpid())
	if state != processFound || row.Start == "" {
		t.Fatalf("premise: the table of this machine has the test's own process with its start time: %v %+v", state, row)
	}
	p := ownProcess(t)
	p.Start, p.Name = row.Start, processName(row.Name)
	return p, row
}

func processDoc(p agentProcess) map[string]any {
	return map[string]any{"pid": p.PID, "start": p.Start, "host": p.Host, "name": p.Name}
}

// freshRecord writes a line of a session's guards at the time, as a session that works leaves.
func (w *forgetWorld) freshRecord(session string, at time.Time) {
	w.t.Helper()
	w.must(appendGuardRecords(w.root, []guardRecord{{At: at, Session: session, Turn: w.tA, Guard: "g", Pass: true}}))
}

// A session whose agent process is still the one its record names and whose last turn has not ended is running, whatever the age of its
// records: they were written two days ago here. (When the request has ended, forget_idle_test.go says what decides.)
func TestForgetRefusesASessionWhoseAgentProcessIsStillAlive(t *testing.T) {
	w := newForgetWorld(t)
	p, _ := useRealProcessTable(t)
	w.setProcess(w.A, processDoc(p))
	before := w.snapshot()
	out, err := forgetCommand(t, w.A, "--apply")
	if err == nil || !strings.Contains(err.Error(), "is still running: its agent process") || !strings.Contains(err.Error(), "its last turn has not ended") {
		t.Fatalf("a session whose agent process is alive, with records two days old, was not refused: %v\n%s", err, out)
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
	}
	if listed, err := forgetCommand(t, w.A); err != nil || !strings.Contains(listed, "Would be refused with --apply: ") {
		t.Errorf("the listing does not say it would be refused: %v\n%s", err, listed)
	}
}

// A process that is not there has ended, and a pid another process uses now is not the agent: forget takes the session even when its
// records were written a moment ago.
func TestForgetTakesASessionWhoseAgentProcessHasEnded(t *testing.T) {
	cases := []struct {
		name  string
		setup func(w *forgetWorld)
	}{
		{"the pid is not in the table", func(w *forgetWorld) {}},
		{"the pid is another process that started later", func(w *forgetWorld) {
			w.table.set(fxPidA, processRow{Parent: 1, Start: "started-later", Name: "claude"})
		}},
		{"the pid of this very process with another start time", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			if c.setup != nil {
				c.setup(w)
			} else {
				p, _ := useRealProcessTable(t)
				p.Start += "-an-earlier-process"
				w.setProcess(w.A, processDoc(p))
			}
			w.freshRecord(w.A, time.Now().UTC())
			before := w.snapshot()
			out, err := forgetCommand(t, w.A, "--apply")
			if err != nil {
				t.Fatalf("a session whose agent has ended was refused: %v\n%s", err, out)
			}
			if problems := w.problems(before); len(problems) > 0 {
				t.Errorf("forget left the project wrong:\n  %s", strings.Join(problems, "\n  "))
			}
			if w.fileExists("sessions/" + w.A + ".json") {
				t.Errorf("the record of the session is still there")
			}
		})
	}
}

// A process noted on another machine cannot be looked for in this one's table (the pid may be anything here): the table is not asked,
// and the time of the records decides, as with a record that names no process.
func TestForgetGoesByTimeForAProcessNotedOnAnotherMachine(t *testing.T) {
	for _, c := range []struct {
		name   string
		last   time.Duration // how long ago the newest record of the session was written
		refuse bool
	}{{"written 23 hours ago", 23 * time.Hour, true}, {"written 25 hours ago", 25 * time.Hour, false}} {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			w.table.set(fxPidA, processRow{Parent: 1, Start: "started-a", Name: "claude"}) // alive here, if it were asked
			w.setProcess(w.A, map[string]any{"pid": fxPidA, "start": "started-a", "host": "another-machine", "name": "claude"})
			w.freshRecord(w.A, time.Now().UTC().Add(-c.last))
			out, err := forgetCommand(t, w.A, "--apply")
			if c.refuse {
				if err == nil || !strings.Contains(err.Error(), "shikii cannot tell whether the session is still running (its agent process was noted on another machine (another-machine))") {
					t.Fatalf("a session of another machine written 23 hours ago was not refused with the reason: %v\n%s", err, out)
				}
			} else if err != nil {
				t.Fatalf("a session of another machine quiet for 25 hours was refused: %v\n%s", err, out)
			}
			if len(w.table.asked) > 0 {
				t.Errorf("the table of this machine was asked about the pid of another machine's process: %v", w.table.asked)
			}
		})
	}
}

// A record with no process (written before the hook noted one, or by an agent the hook found no process for) gives forget nothing to look
// at: it refuses until the newest record of the session is a day old, and says why.
func TestForgetGoesByTimeWhenNoProcessIsNoted(t *testing.T) {
	for _, c := range []struct {
		name   string
		last   time.Duration
		refuse bool
	}{{"written a moment ago", time.Second, true}, {"written 23 hours ago", 23 * time.Hour, true}, {"written 25 hours ago", 25 * time.Hour, false}} {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			w.noProcess(w.A)
			w.freshRecord(w.A, time.Now().UTC().Add(-c.last))
			before := w.snapshot()
			out, err := forgetCommand(t, w.A, "--apply")
			if c.refuse {
				if err == nil || !strings.Contains(err.Error(), "shikii cannot tell whether the session is still running (no agent process is noted for it") {
					t.Fatalf("a session with no process noted was not refused with the reason: %v\n%s", err, out)
				}
				if after := w.snapshot(); !reflect.DeepEqual(before, after) {
					t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
				}
				return
			}
			if err != nil {
				t.Fatalf("a session with no process noted, quiet for 25 hours, was refused: %v\n%s", err, out)
			}
			if w.fileExists("sessions/" + w.A + ".json") {
				t.Errorf("the record of the session is still there")
			}
		})
	}
}

// When the table cannot be read, the noted process says nothing: the time decides, and the reason names the table.
func TestForgetGoesByTimeWhenTheProcessTableCannotBeRead(t *testing.T) {
	w := newForgetWorld(t)
	w.table.state = processUnknown
	w.freshRecord(w.A, time.Now().UTC())
	out, err := forgetCommand(t, w.A, "--apply")
	if err == nil || !strings.Contains(err.Error(), "(the process table of this machine could not be read)") {
		t.Fatalf("a table that cannot be read took the session, or was not named: %v\n%s", err, out)
	}
	w.table.state = processAbsent
	if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
		t.Fatalf("the table can be read again and the process has ended: %v\n%s", err, out)
	}
}

// A subagent's id is judged by the session it ran under: that session's process, and not the time of the subagent's own deliveries.
func TestForgetJudgesASubagentIdByTheProcessOfItsSession(t *testing.T) {
	t.Run("the session is running, the subagent's deliveries are old", func(t *testing.T) {
		w := newForgetWorld(t)
		w.runs(w.A)
		before := w.snapshot()
		out, err := forgetCommand(t, w.childA, "--apply")
		if err == nil || !strings.Contains(err.Error(), w.childA+" (a subagent that ran under the session "+w.A+") is still running: its agent process") {
			t.Fatalf("a subagent of a running session was not refused: %v\n%s", err, out)
		}
		if after := w.snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
		}
	})
	t.Run("the session has ended, the subagent's delivery is new", func(t *testing.T) {
		w := newForgetWorld(t)
		w.delivery("fresh", "f5f5f5f5f5f5f5f5", w.tA, w.childA, w.A, "", "")
		out, err := forgetCommand(t, w.childA, "--apply")
		if err != nil {
			t.Fatalf("a subagent of an ended session was refused for its delivery's time: %v\n%s", err, out)
		}
	})
	t.Run("the session's record names no process", func(t *testing.T) {
		w := newForgetWorld(t)
		w.noProcess(w.A)
		w.delivery("fresh", "f6f6f6f6f6f6f6f6", w.tA, w.childA, w.A, "", "")
		out, err := forgetCommand(t, w.childA, "--apply")
		if err == nil || !strings.Contains(err.Error(), "shikii cannot tell whether the session is still running (no agent process is noted for it") {
			t.Fatalf("a subagent with a new delivery and no process to go by was not refused: %v\n%s", err, out)
		}
	})
}

// The session this command runs in is still never taken, whatever the table says of it.
func TestForgetNeverTakesTheSessionItRunsInEvenWhenItsProcessHasEnded(t *testing.T) {
	w := newForgetWorld(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", w.A)
	out, err := forgetCommand(t, w.A, "--apply")
	if err == nil || !strings.Contains(err.Error(), "is the session this command runs in (CLAUDE_CODE_SESSION_ID)") {
		t.Fatalf("the session the command runs in was taken: %v\n%s", err, out)
	}
}
