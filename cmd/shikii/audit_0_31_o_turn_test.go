package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Audit O of 0.31.0 (the check of the fixes of audits M and N): the one state of the session's turn.

// auditOLinkLatest moves ~/.shikii/agent/latest to a folder of the person's and leaves a link in its place (the states kept with
// other notes, a synced folder: the case the fix of M-I1 refuses to write or remove in).
func auditOLinkLatest(t *testing.T) string {
	t.Helper()
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "synced-latest")
	if err := os.Rename(filepath.Join(common, "latest"), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, filepath.Join(common, "latest")); err != nil {
		t.Fatal(err)
	}
	return moved
}

// INVARIANT 9: "the state of a folder that is a link is neither written nor removed" (the fix of M-I1), while the state there is
// still read (readSessionLatest follows the link). Request 1 in A is recorded, the folder of the states becomes a link, and the
// Stop of request 1 lets the turn end: it cannot write the end, the state read keeps request 1 open, and the person's next request
// in A takes over the references of request 1, whose turn had ended. Before the fix the write followed the link and the state was
// kept current.
func TestAuditOALinkedStateFolderIsReadButNeverWritten(t *testing.T) {
	t.Run("control: the same flow with the folder in place hands nothing over", func(t *testing.T) {
		f := auditCFinishedFirstRequest(t)
		if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
			t.Fatalf("precondition: the Stop of request 1 passes: %s", reason)
		}
		next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
		if carried := next["carried"]; carried != nil {
			t.Fatalf("control: a passed Stop hands nothing over: %.200s", auditJSON(carried))
		}
	})
	t.Run("the folder of the states is a link", func(t *testing.T) {
		f := auditCFinishedFirstRequest(t)
		first := loadSession(f.root, "planning").TurnID
		auditOLinkLatest(t)
		if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
			t.Fatalf("precondition: the Stop of request 1 passes: %s", reason)
		}
		latest, known, err := readSessionLatest("planning")
		t.Logf("after the passed Stop, the state read through the link: %+v known=%v err=%v (request 1 is %s)", latest, known, err, first)
		next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
		if carried := next["carried"]; carried != nil {
			t.Errorf("INVARIANT 9: the state of a linked folder is not written, yet it is read: the next request takes over the references of request 1, whose turn had ended: %.240s", auditJSON(carried))
		}
	})
}

// A guard for the branch of sessionTurnOnExit that a completion notice takes when the hook leaves before it changed the state (a
// project whose hook fails): the notice reopens the session's last request, wherever the shell stands (INVARIANT 9). No shipped
// test runs a notice in such a project: with the branch taken out (only a Stop changed on the way out) every shipped test of the
// turn passes (_audit_o/mutations.log, O7). GREEN on the candidate.
func TestAuditOANoticeInAProjectWhoseHookFailsReopens(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "The check runs in the background."})); blocked {
		t.Fatalf("precondition: the Stop passes: %s", reason)
	}
	if s := loadSession(f.root, "planning"); s.goesOnForTest() {
		t.Fatalf("precondition: the request ended")
	}
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	handoff := filepath.Join(otherRoot, "handoff")
	if err := os.RemoveAll(handoff); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), handoff); err != nil {
		t.Fatal(err)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": auditINotice})
	if s := loadSession(f.root, "planning"); !s.goesOnForTest() {
		latest, known, _ := readSessionLatest("planning")
		t.Errorf("INVARIANT 9: a notice that came in a project whose hook fails did not reopen the session's last request: %+v known=%v", latest, known)
	}
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] == nil {
		t.Errorf("INVARIANT 9: after that notice, the next message does not take over the references of the request the agent works on")
	}
}
