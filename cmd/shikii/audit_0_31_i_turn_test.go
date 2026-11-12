package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit I of 0.31.0 (fifth round): which request a completion notice reopens, and which Stop ends the turn elsewhere.

const auditINotice = "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"

// auditIRequestInB runs request 1 in A to its passing Stop, then request 2 in project B (the session's latest request), and
// returns the flow, B's folder and B's .shikii.
func auditIRequestInB(t *testing.T) (*auditFlow, string, string) {
	t.Helper()
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop of request 1 passes in A: %s", reason)
	}
	if a := loadSession(f.root, "planning"); a.goesOnForTest() {
		t.Fatalf("precondition: request 1 ended in A")
	}
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	supplyReasoningFixture(otherRoot, "planning")
	return f, other, otherRoot
}

// INVARIANT 9: "only the request the session worked on last goes on after a completion notice; a request that ended before it is
// not reopened". The fix applies that rule to the other projects only. A notice that comes while the shell stands in A (the
// agent went back to A's folder) is handled by hookUserPrompt in A itself, which takes back the end of A's own request 1, ended
// before B's request 2 started. The person's next message in A takes over the references of request 1.
func TestAuditINoticeInAReopensItsOwnRequestThatEndedBeforeB(t *testing.T) {
	f, other, otherRoot := auditIRequestInB(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop of request 2 passes in B: %s", reason)
	}
	a, b := loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	if a.goesOnForTest() || b.goesOnForTest() || !b.At.After(a.At) {
		t.Fatalf("precondition: both requests ended, B's record is the newer (A %v, B %v)", a.At, b.At)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": auditINotice})
	a, b = loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("after the notice in A: A ended==turn %v (request 1, ended before request 2), B ended==turn %v (request 2, the latest)", !a.goesOnForTest(), !b.goesOnForTest())
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := second["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: a notice that came in A reopened A's request 1, which ended before request 2 in B; the next request in A takes over its references: %.300s", auditJSON(carried))
	}
}

// INVARIANT 9: the request "the session worked on last" is chosen by the time of the record (At), and At is the time of the
// record's last write, not of its request. A Stop of request 2 that runs while the shell stands in A (the agent looked at A's files)
// ends B's turn and saves A's record after B's: A's record is now the newer. A notice that then comes in B reopens A's request 1,
// ended long before, and the person's next message in A takes over its references.
func TestAuditIAStopInAMakesItsRecordTheLatest(t *testing.T) {
	f, other, otherRoot := auditIRequestInB(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop of request 2 passes while the shell stands in A: %s", reason)
	}
	// A's record is written once more after B's (a lesson delivered while the shell stands in A writes it the same way)
	if err := updateSession(f.root, "planning", func(s *hookSession) {}); err != nil {
		t.Fatal(err)
	}
	a, b := loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("after the Stop in A: A ended==turn %v at %v; B (request 2) ended==turn %v at %v", !a.goesOnForTest(), a.At.Format("15:04:05.000000"), !b.goesOnForTest(), b.At.Format("15:04:05.000000"))
	if b.goesOnForTest() || !a.At.After(b.At) || !b.HeadAt.After(a.HeadAt) {
		t.Fatalf("precondition: the Stop in A ended B's request 2, A's record is the newer and B's request the later")
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": auditINotice})
	a, b = loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("after the notice in B: A ended==turn %v (request 1), B ended==turn %v (request 2)", !a.goesOnForTest(), !b.goesOnForTest())
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := second["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: a notice of request 2 (in B) reopened A's request 1, because a Stop in A had written A's record last; the next request in A takes over its references: %.300s", auditJSON(carried))
	}
}

// INVARIANT 9: "a Stop that stopped nothing is the end ... a Stop that cannot read its own record and ends incomplete also writes
// the end in the other projects". A Stop in B that a check stops, whose own record then cannot be saved, ends the turn
// incomplete (continue:false), yet turnElsewhere was told it blocked: A's request stays open and the next request in A takes over
// its references.
func TestAuditIStopInBThatCannotSaveItsRecord(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project."})
	// B's record is not written again (a full disk there); A's is
	working := writeSessionRecord
	writeSessionRecord = func(p string, b []byte) error {
		if strings.HasPrefix(p, otherRoot+string(filepath.Separator)) {
			return errors.New("no space left on device")
		}
		return working(p, b)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "Done."})
	writeSessionRecord = working
	_, blocked := stopReason(t, out)
	a := loadSession(f.root, "planning")
	t.Logf("the Stop in B: blocked=%v out=%.200s; A ended==turn %v", blocked, out, !a.goesOnForTest())
	if blocked || !strings.Contains(out, `"continue":false`) {
		t.Fatalf("precondition: the Stop ends the turn incomplete: %s", out)
	}
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := second["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: the Stop in B ended the turn (continue:false, its record could not be saved), yet A keeps the request open and the next request takes over its references: %.300s", auditJSON(carried))
	}
}

// The same choice when the latest record cannot be read: B's record (request 2, the latest) is cut, and a notice that comes outside
// every project reopens A's request 1, ended before request 2 started.
func TestAuditINoticeWhenTheLatestRecordIsUnreadable(t *testing.T) {
	f, other, otherRoot := auditIRequestInB(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop of request 2 passes in B: %s", reason)
	}
	if err := os.WriteFile(sessionPath(otherRoot, "planning"), []byte("{cut"), 0o600); err != nil {
		t.Fatal(err)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "prompt": auditINotice})
	a := loadSession(f.root, "planning")
	t.Logf("after the notice outside, B's record unreadable: A ended==turn %v (request 1)", !a.goesOnForTest())
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := second["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: with the record of the latest request unreadable, a notice reopened A's request 1, ended before it; the next request in A takes over its references: %.200s", auditJSON(carried))
	}
}
