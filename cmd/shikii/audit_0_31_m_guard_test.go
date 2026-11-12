package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Audit M of 0.31.0: guards for lines of the one state of the turn that no shipped test holds (each is GREEN on the candidate and
// RED with the line taken out: _audit_m/mutations.log M9-09, M9-10).

// A Stop the check would stop, whose record cannot be saved, ends the turn incomplete (continue:false, Halt): blocked is set back
// to false, and the state ends the request. The shipped test of this line (TestAuditIStopInBThatCannotSaveItsRecord) runs the Stop
// for a project that is not the latest request's: with the one state it passes whatever blocked is.
func TestAuditMAHaltedStopThatWouldHaveBlockedEndsTheTurn(t *testing.T) {
	t.Run("control: the check stops this Stop when the record is saved", func(t *testing.T) {
		f := newAuditFlow(t)
		f.prepare(f.ask(planningTestRequest), "CSV export filters")
		if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); !blocked {
			t.Fatalf("precondition: the check stops the Stop of a request whose work is not recorded: %s", reason)
		}
	})
	t.Run("the record cannot be saved", func(t *testing.T) {
		f := newAuditFlow(t)
		f.prepare(f.ask(planningTestRequest), "CSV export filters")
		first := loadSession(f.root, "planning").TurnID
		working := writeSessionRecord
		writeSessionRecord = func(p string, b []byte) error {
			if strings.HasPrefix(p, f.root+string(filepath.Separator)) {
				return errors.New("no space left on device")
			}
			return working(p, b)
		}
		out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
		writeSessionRecord = working
		if !strings.Contains(out, `"continue":false`) {
			t.Fatalf("precondition: the Stop ends the turn incomplete: %s", out)
		}
		latest, known, _ := readSessionLatest("planning")
		t.Logf("halted Stop: out=%.120s state=%+v", out, latest)
		if !known || latest.Turn != first || !latest.Ended {
			t.Errorf("INVARIANT 9: the Stop ended the turn (continue:false) and the state keeps the request open: %+v", latest)
		}
	})
}

// The removal of old states: a state older than hookSessionTTL goes, the one just written stays. No shipped test removes one.
func TestAuditMTheStatesOfOldSessionsAreRemoved(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("old-session", "", "")
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	p := sessionLatestPath(common, "old-session")
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	noteSessionRequest("new-session", "", "")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("the state of a session not seen for longer than hookSessionTTL is kept: the folder grows with every session")
	}
	if _, known, _ := readSessionLatest("new-session"); !known {
		t.Errorf("the state just written was removed")
	}
}

// M-A2, the likeliest way: the agent removed the folder its shell stands in (a scratch clone), and the Stop comes with that cwd.
// projectStoreAt cannot verify the folder ("cannot verify the project folder"), runHookEvent returns before sessionTurnEvent, and
// the request that ended stays open: the next request in A takes over its references.
func TestAuditMStopFromAFolderThatWasRemoved(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	first := loadSession(f.root, "planning").TurnID
	gone := filepath.Join(t.TempDir(), "scratch-clone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": gone, "last_assistant_message": "Done; the scratch clone is removed."})
	latest, known, _ := readSessionLatest("planning")
	t.Logf("the Stop from a removed folder: out=%q state=%+v known=%v (request 1 is %s)", out, latest, known, first)
	if out != "" {
		t.Fatalf("precondition: the Stop says nothing, so the agent's turn ends")
	}
	if known && latest.Turn == first && !latest.Ended {
		t.Errorf("INVARIANT 9: the Stop that let the turn end (its cwd removed) did not end request 1")
	}
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := next["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: the next request in A takes over the references of request 1, whose turn had ended: %.200s", auditJSON(carried))
	}
}
