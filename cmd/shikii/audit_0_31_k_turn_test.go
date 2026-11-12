package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Audit K of 0.31.0 (sixth round): which request a completion notice reopens, when the session's latest request left no record
// that sessionTurns can see.
//
// INVARIANT 9: "a completion notice keeps working only the request this session worked on last (HeadAt newest); a request that
// ended before it is not reopened, neither in the project the notice came in nor elsewhere". noticeGoesOnHere and turnElsewhere
// pick the latest among the records they can read in the registered projects. A request that came while the shell stood outside
// every project leaves no record at all, and the record of a project that is no longer registered (its folder removed or moved,
// its index entry unreadable) is not read: the older request in A is then "the latest", and a notice in A reopens it.

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

// Request 2 comes while the shell stands outside every project (a folder without .shikii), ends there, and its background work's
// notice comes after the agent went back to A.
func TestAuditKNoticeAfterARequestOutsideEveryProject(t *testing.T) {
	f, first := auditKEndedInA(t)
	outside := t.TempDir()
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": outside, "prompt": "Now read the logs in this folder and run the long check in the background."})
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": outside, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop of request 2 passes outside: %s", reason)
	}
	auditKNoticeInA(t, f, first, "request 2 outside every project")
}

// Request 2 comes in project B and ends there; then B's folder is removed (a scratch clone the agent deleted), so B is no longer
// registered and its record (the latest request) is not read.
func TestAuditKNoticeAfterTheLatestProjectWasRemoved(t *testing.T) {
	f, other, otherRoot := auditIRequestInB(t)
	first := ""
	if a := loadSession(f.root, "planning"); !a.goesOnForTest() {
		first = a.TurnID
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop of request 2 passes in B: %s", reason)
	}
	if b := loadSession(otherRoot, "planning"); b.goesOnForTest() || first == "" {
		t.Fatalf("precondition: both requests ended")
	}
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	auditKNoticeInA(t, f, first, "B removed after request 2")
}

// The same with B in place and its entry of the project index unreadable: registeredProjects skips the entry without a word, and
// readable stays true (only an unreadable session record makes it false).
func TestAuditKNoticeWhenTheIndexEntryOfTheLatestProjectIsUnreadable(t *testing.T) {
	f, other, otherRoot := auditIRequestInB(t)
	first := ""
	if a := loadSession(f.root, "planning"); !a.goesOnForTest() {
		first = a.TurnID
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop of request 2 passes in B: %s", reason)
	}
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	cut := 0
	for _, path := range entries {
		if p, ok := readEntry(path); ok && p.Path == feedbackScope(other) {
			if err := os.WriteFile(path, []byte("{cut"), 0o600); err != nil {
				t.Fatal(err)
			}
			cut++
		}
	}
	if cut != 1 || first == "" {
		t.Fatalf("precondition: B's index entry is cut (%d), request 1 ended", cut)
	}
	_ = otherRoot
	auditKNoticeInA(t, f, first, "B's index entry unreadable")
}

// A guard for noticeGoesOnHere's "readable": with B's record (request 2, the latest) unreadable, a notice that comes in A itself
// does not reopen A's request 1 (INVARIANT 9: "with a record of the session that cannot be read, none is reopened"). The shipped
// tests check this for a notice outside every project (turnElsewhere) only: without "readable &&" in noticeGoesOnHere they all pass
// (audit K, mutation K-M05). GREEN on the candidate.
func TestAuditKNoticeInAWithTheLatestRecordUnreadable(t *testing.T) {
	f, other, otherRoot := auditIRequestInB(t)
	first := ""
	if a := loadSession(f.root, "planning"); !a.goesOnForTest() {
		first = a.TurnID
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})); blocked || first == "" {
		t.Fatalf("precondition: both requests ended: %s", reason)
	}
	if err := os.WriteFile(sessionPath(otherRoot, "planning"), []byte("{cut"), 0o600); err != nil {
		t.Fatal(err)
	}
	auditKNoticeInA(t, f, first, "B's record unreadable")
}
