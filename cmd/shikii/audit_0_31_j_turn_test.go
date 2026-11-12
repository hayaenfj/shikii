package main

import (
	"testing"
)

// Audit J of 0.31.0 (fifth round): which request a completion notice reopens.
//
// INVARIANT 9: "a completion notice keeps working only the request this session worked on last (the one whose record has the
// newest time); a request that ended before it is not reopened". turnElsewhere picks the newest by hookSession.At, which
// saveSession sets on every write of the record, not when the request came. The flow below is the one turnElsewhere exists for:
// the agent's shell changed folder. Request 1 in A ends at its Stop. Request 2 comes in B (the shell stands in B). The agent's last
// command of request 2 runs in A, so request 2's Stop comes from A, and the completion notice of request 2's background work comes
// from A too (the shell has not moved since).
func TestAuditJNoticeAfterAStopFromTheOldProject(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop of request 1 passes: %s", reason)
	}
	a := loadSession(f.root, "planning")
	if a.endedTurnForTest() != a.TurnID {
		t.Fatalf("precondition: request 1 in A ended")
	}
	first := a.TurnID
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	b := loadSession(otherRoot, "planning")
	if b.TurnID == "" || b.endedTurnForTest() == b.TurnID {
		t.Fatalf("precondition: request 2 is open in B: turn %q ended %q", b.TurnID, b.endedTurnForTest())
	}
	second := b.TurnID
	// request 2's Stop comes from A (the agent's last command ran there)
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The check runs in the background."})
	if _, blocked := stopReason(t, out); blocked {
		t.Fatal("precondition: the Stop of request 2 passes")
	}
	a, b = loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("after request 2's Stop from A: A turn=%s ended=%v at=%s; B turn=%s ended=%v at=%s", a.TurnID, a.endedTurnForTest() == a.TurnID, a.At.Format("15:04:05.000000"), b.TurnID, b.endedTurnForTest() == b.TurnID, b.At.Format("15:04:05.000000"))
	// the completion notice of request 2's background work, from A
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	a, b = loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("after the notice from A: A turn=%s (request 1 is %s) ended=%v; B turn=%s (request 2 is %s) ended=%v", a.TurnID, first, a.endedTurnForTest() == a.TurnID, b.TurnID, second, b.endedTurnForTest() == b.TurnID)
	if a.TurnID == first && a.endedTurnForTest() != a.TurnID {
		t.Errorf("INVARIANT 9: the notice reopened request 1 in A, which ended before request 2 began")
	}
	if b.TurnID == second && b.endedTurnForTest() == b.TurnID {
		t.Errorf("INVARIANT 9: the notice did not reopen request 2 in B, the request the session worked on last")
	}
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := next["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: the next request in A takes over the references of request 1, which had ended: %.300s", auditJSON(carried))
	}
}

// The same flow with the notice from B (where request 2 came): the notice keeps B open, and turnElsewhere picks the record
// written last. request 2's Stop from A wrote A's record after B's, so A counts as the newest.
func TestAuditJNoticeFromBAfterAStopFromTheOldProject(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop of request 1 passes: %s", reason)
	}
	first := loadSession(f.root, "planning").TurnID
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The check runs in the background."})
	if _, blocked := stopReason(t, out); blocked {
		t.Fatal("precondition: the Stop of request 2 passes")
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	a := loadSession(f.root, "planning")
	t.Logf("after the notice from B: A turn=%s (request 1 is %s) ended=%v", a.TurnID, first, a.endedTurnForTest() == a.TurnID)
	if a.TurnID == first && a.endedTurnForTest() != a.TurnID {
		t.Errorf("INVARIANT 9: a notice from B reopened request 1 in A, which ended before request 2 began (A's record was written last, by request 2's Stop)")
	}
	_ = otherRoot
}

// The same flow as TestAuditJNoticeAfterAStopFromTheOldProject: after the notice from A, request 1's record in A is set to be
// judged again (Judged back to false), and the Stop that follows judges request 1, which had ended, on the reply about request
// 2's background work (Previous becomes that reply).
func TestAuditJRequestOneJudgedAgain(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}, "result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"new1"}}}); err != nil || saved["complete"] != true {
		t.Fatalf("precondition: %v", err)
	}
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	other := gitRepo(t)
	initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The check runs in the background."})
	a := loadSession(f.root, "planning")
	t.Logf("before the notice: A request=%q judged=%v previous=%q", a.Request, a.Judged, a.Previous)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	a = loadSession(f.root, "planning")
	t.Logf("after the notice: A request=%q judged=%v", a.Request, a.Judged)
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The background check of the other project passed."})
	a = loadSession(f.root, "planning")
	t.Logf("after the next Stop from A: out=%.120s A request=%q judged=%v previous=%q", out, a.Request, a.Judged, a.Previous)
	if a.Request == planningTestRequest && a.Judged && a.Previous == "The background check of the other project passed." {
		t.Errorf("INVARIANT 9: request 1 (%q), which ended before request 2 began, was judged again at a Stop whose reply is about request 2's background work", a.Request)
	}
}
