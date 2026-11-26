package main

import "testing"

// Helpers of audit K of 0.31.0 (sixth round), which asked which request a completion notice reopens when the session's latest
// request left no record in the project: request 1 ends in A, request 2 comes elsewhere, and the notice comes in A. INVARIANT 9: only
// the request the session worked on last goes on, and a request that ended before it is not reopened. The cases of audit K itself
// (the latest request outside every project, in a removed project, with its index entry or its record unreadable) became one case
// once the session's turn was kept in one state (sessionLatest): TestAuditGNoticeInBReopensAFinishedRequestInA reads that state.

// auditKEndedInA runs request 1 in A to its passing Stop and returns the flow and request 1's turn.
func auditKEndedInA(t *testing.T) (*auditFlow, string) {
	t.Helper()
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop of request 1 passes in A: %s", reason)
	}
	a := loadSession(f.root, "planning")
	if a.goesOnForTest() || a.TurnID == "" {
		t.Fatalf("precondition: request 1 ended in A")
	}
	return f, a.TurnID
}

// auditKNoticeInA sends the completion notice from A and reports what INVARIANT 9 forbids: request 1 open again in A, and the
// next request in A taking over its references.
func auditKNoticeInA(t *testing.T, f *auditFlow, first, why string) {
	t.Helper()
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": auditINotice})
	a := loadSession(f.root, "planning")
	t.Logf("%s: after the notice in A: A turn=%s (request 1 is %s) ended=%v judged=%v", why, a.TurnID, first, !a.goesOnForTest(), a.Judged)
	if a.TurnID == first && a.goesOnForTest() {
		t.Errorf("INVARIANT 9 (%s): the notice reopened request 1 in A, which ended before request 2 came", why)
	}
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := next["carried"]; carried != nil {
		t.Errorf("INVARIANT 9 (%s): the next request in A takes over the references of request 1, which had ended: %.240s", why, auditJSON(carried))
	}
}
