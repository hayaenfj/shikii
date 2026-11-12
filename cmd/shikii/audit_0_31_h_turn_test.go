package main

import (
	"testing"
)

// Audit H of 0.31.0 (fourth round): what turnElsewhere (new in this round) does to a turn that ended long before.

// H-6 [INVARIANT 9]: a request in project A ended at its Stop. The session went on to a new request in project B, and a
// completion notice of background work came there. turnElsewhere takes the end back in every other project that holds a record of
// the session, so A's finished request counts as unfinished again. A message that comes while the agent's shell stands in A then
// takes over the references of that finished request, and carried says that the earlier request "was still being worked on".
func TestAuditHNoticeInAnotherProjectReopensAFinishedTurn(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop of the first request passes: %s", reason)
	}
	if a := loadSession(f.root, "planning"); a.EndedTurn != a.TurnID {
		t.Fatalf("precondition: the first request ended at its Stop")
	}
	// the control: a new request in A after the finished one takes nothing over
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project."})
	a := loadSession(f.root, "planning")
	t.Logf("after the request in B: A ended==turn %v", a.EndedTurn == a.TurnID)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	a, b := loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("after the notice in B: A ended==turn %v (A's turn is the finished first request); B turn %s ended %q", a.EndedTurn == a.TurnID, b.TurnID, b.EndedTurn)
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := second["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: the request in A ended at its Stop; a completion notice that came during another request in project B took that end back, and the next request in A takes over the references of the finished request: %.400s", auditJSON(carried))
	}
}
