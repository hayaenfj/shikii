package main

import (
	"errors"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The one state of the session's turn (sessionLatest): a Stop ends the request it saw when it came, so a Stop that comes after the
// next request was noted leaves that request open (the hook reads the turn when the Stop comes and ends it only if it is still the
// session's last request).
func TestAStopThatCameBeforeTheNextRequestLeavesItOpen(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
	seen := sessionTurnAt("s1") // the Stop of turn 1 comes
	noteSessionRequest("s1", "/p/a/.shikii", "turn-2")
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, seen, false)
	if latest, known, err := readSessionLatest("s1"); err != nil || !known || latest.Turn != "turn-2" || latest.Ended {
		t.Errorf("the Stop of turn 1 ended turn 2, which came after it: %+v %v %v", latest, known, err)
	}
	// control: the Stop of the last request ends it; a Stop a check stopped, or a subagent's, does not
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), true)
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", AgentID: "child"}, sessionTurnAt("s1"), false)
	if latest, _, _ := readSessionLatest("s1"); latest.Ended {
		t.Errorf("a Stop a check stopped, or a subagent's Stop, ended the turn")
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	if latest, _, _ := readSessionLatest("s1"); !latest.Ended {
		t.Errorf("control: the Stop of the last request did not end it")
	}
}

// A completion notice means the agent works on: the session's last request is reopened wherever the agent's shell stands (in its
// project, in another project, outside every project), and a message the person sends next in that project takes over its
// references. A session without the state reopens nothing.
func TestANoticeReopensTheLastRequestWhereverTheShellStands(t *testing.T) {
	for _, where := range []string{"its project", "another project", "outside every project"} {
		t.Run(where, func(t *testing.T) {
			f := auditCFinishedFirstRequest(t) // a request whose work is recorded, so its Stop passes
			if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The check runs in the background."})); blocked {
				t.Fatalf("precondition: the Stop passes: %s", reason)
			}
			if s := loadSession(f.root, "planning"); s.goesOnForTest() {
				t.Fatalf("precondition: the request ended")
			}
			cwd := f.repo
			switch where {
			case "another project":
				cwd = gitRepo(t)
				initTestProject(t, cwd)
			case "outside every project":
				cwd = t.TempDir()
			}
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": cwd, "prompt": auditINotice})
			if s := loadSession(f.root, "planning"); !s.goesOnForTest() {
				t.Errorf("a notice that came in %s did not reopen the session's last request", where)
			}
			second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
			if second["carried"] == nil {
				t.Errorf("after a notice in %s, the next message does not take over the references of the request the agent works on", where)
			}
		})
	}
}

// A result whose save was interrupted once (after the source was written) is finished by the next record that closes the work:
// the capture is saved again as the kind its memo kept (a result shikii writes), noted as a report, and the record is complete. A
// capture finished as an observation would be refused under the title of a result, and the work could not close.
func TestAnInterruptedResultIsFinishedAsAReport(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "observation" && !failed {
			failed = true
			return errors.New("synthetic: the write was interrupted")
		}
		return nil
	}
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
	workAfterWrite = old
	if !failed || err != nil || a["complete"] == true {
		t.Fatalf("precondition: the first result's save is interrupted once: %v %.300s", err, auditJSON(a))
	}
	b, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
	if err != nil || b["complete"] != true {
		t.Errorf("the next record does not close the work: the interrupted result is not finished: %v %.400s", err, auditJSON(b["errors"]))
	}
	w := loadSession(f.root, "planning").Planning.Work
	for key, capture := range w.Captures {
		if capture.ID == "" {
			t.Errorf("a capture is left unsaved: %s %+v", key, capture)
		}
		if capture.Source == notebook.WorkResult && !w.Reports[capture.ID] {
			t.Errorf("the interrupted result %s is not noted as a report", capture.ID)
		}
	}
}
