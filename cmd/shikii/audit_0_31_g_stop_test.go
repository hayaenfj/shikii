package main

import (
	"os"
	"path/filepath"
	"testing"
)

// INVARIANT 9: a Stop that stops nothing ends the turn, and the same session's records in other registered projects get the end
// too. Here the Stop runs in project B whose own record of the session cannot be read (a record cut by a full disk, or written by
// another version). The hook answers with continue:false (the agent's turn is over), and returns before it writes the end
// elsewhere: back in project A, the next request takes over the references of the finished one.
func TestAuditGStopInAProjectWhoseRecordIsUnreadable(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	if err := os.MkdirAll(filepath.Join(otherRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath(otherRoot, "planning"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "Done."})
	_, blocked := stopReason(t, out)
	s := loadSession(f.root, "planning")
	t.Logf("the Stop in B (its record unreadable): blocked=%v out=%.160s; A: ended==turn %v", blocked, out, !s.goesOnForTest())
	if blocked {
		t.Fatal("precondition: the Stop does not block")
	}
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] != nil {
		t.Errorf("INVARIANT 9: the Stop in B did not block (continue:false ends the turn), yet A keeps the turn open and the next request takes over the finished request's references: %.160s", auditJSON(second["carried"]))
	}
}

// A completion notice that comes in project B, for work of B's request, takes back the end of the turn in every other project
// of the session: the request in A that had ended before B's request started is open again until the next Stop. A message of the
// person that comes in A before that Stop takes over the references of A's finished request.
func TestAuditGNoticeInBReopensAFinishedRequestInA(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	a := loadSession(f.root, "planning")
	if a.goesOnForTest() {
		t.Fatalf("precondition: A's request ended")
	}
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	supplyReasoningFixture(otherRoot, "planning")
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})
	if _, blocked := stopReason(t, out); blocked {
		t.Fatal("precondition: the Stop in B passes")
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	a = loadSession(f.root, "planning")
	t.Logf("after the notice in B: A ended==turn %v (A's request had ended before B's started)", !a.goesOnForTest())
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	t.Logf("carried in A: %.200s", auditJSON(second["carried"]))
	if second["carried"] != nil {
		t.Errorf("a notice of B's background work reopened A's finished request; the next request in A takes over its references: %.160s", auditJSON(second["carried"]))
	}
}

// A Stop that a check stops in B writes nothing in A; a Stop that passes ends the session's last request, and writes no project's
// record but the hook's own save.
func TestAuditGTurnElsewhereWritesOnlyWhereItShould(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project."})
	before, _ := os.ReadFile(sessionPath(f.root, "planning"))
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "Done."})
	reason, blocked := stopReason(t, out)
	after, _ := os.ReadFile(sessionPath(f.root, "planning"))
	t.Logf("a Stop in B without B's reasoning: blocked=%v (%.80s); A's record changed: %v", blocked, reason, string(before) != string(after))
	if blocked && string(before) != string(after) {
		t.Errorf("a Stop that a check stopped in B wrote A's record")
	}
	// an event changes the one state of the session's turn and writes no project's record; the Stop ends the session's last request
	before, _ = os.ReadFile(sessionPath(f.root, "planning"))
	beforeB, _ := os.ReadFile(sessionPath(otherRoot, "planning"))
	sessionTurnEvent("stop", &hookInput{SessionID: "planning"}, sessionTurnAt("planning"), false)
	after, _ = os.ReadFile(sessionPath(f.root, "planning"))
	afterB, _ := os.ReadFile(sessionPath(otherRoot, "planning"))
	if string(before) != string(after) || string(beforeB) != string(afterB) {
		t.Errorf("an event of the session's turn wrote a project's record")
	}
	if b := loadSession(otherRoot, "planning"); b.goesOnForTest() {
		t.Errorf("control: the Stop ends the session's last request (B's)")
	}
	_ = otherRoot
}
