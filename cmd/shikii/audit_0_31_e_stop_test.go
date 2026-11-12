package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// audit E (round 3): the Stop of a turn runs while the agent's shell stands in ANOTHER registered project (the agent ran cd into
// a second project of the same person). The hook finds a project there, so endTurnElsewhere is not used; that project has no record
// of the session, and the record in the first project keeps no end of the turn.
func TestAuditEStopInAnotherRegisteredProject(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	other := gitRepo(t)
	initTestProject(t, other)
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "Done."})
	if reason, blocked := stopReason(t, out); blocked {
		t.Fatalf("precondition: the Stop in the other project passes: %s", reason)
	}
	s := loadSession(f.root, "planning")
	t.Logf("stop in another registered project: out=%q ended_turn=%q turn=%s", out, s.EndedTurn, s.TurnID)
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] != nil {
		t.Errorf("INVARIANT 9: the turn ended (its Stop ran in another registered project and stopped nothing), and the next preparation took its references over: %.200s", auditJSON(second["carried"]))
	}
}

// control: the same flow with the Stop in a folder outside every project hands nothing over (the fix of round 2).
func TestAuditEStopOutsideEveryProjectControl(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	s := loadSession(f.root, "planning")
	t.Logf("stop outside: out=%q ended_turn=%q turn=%s", out, s.EndedTurn, s.TurnID)
	if second := f.prepare(f.ask("Now rename the export button, a new task."), "export button"); second["carried"] != nil {
		t.Errorf("control: a Stop outside every project ends the turn: %.200s", auditJSON(second["carried"]))
	}
}

// A Stop outside every project when things are broken: a registry file that is not JSON, a project whose mark is gone, a session
// record that is not JSON, and a session lock another process holds. The hook must not fail, and must write nothing it should not.
func TestAuditEStopOutsideWithBrokenState(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	common, _ := agentHome()
	// 1) a registry file that is not JSON, and one that names a folder that is gone
	if err := os.WriteFile(filepath.Join(common, "projects", "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "projects", "gone.json"), []byte(`{"id":"x","name":"x","path":"/nonexistent/project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// 2) the session lock is held by someone else
	lock, err := os.OpenFile(sessionPath(f.root, "planning")+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	oldWait := hookLockWait
	hookLockWait = 0
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	hookLockWait = oldWait
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	lock.Close()
	s := loadSession(f.root, "planning")
	logged, _ := os.ReadFile(filepath.Join(common, "hook.log"))
	t.Logf("locked: out=%q ended=%q log tail=%.200s", out, s.EndedTurn, auditELastLine(string(logged)))
	if s.EndedTurn != "" {
		t.Errorf("the lock was held, yet the record changed")
	}
	// 3) another session's Stop does not touch this session; a subagent-looking id neither
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning2", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	if s := loadSession(f.root, "planning"); s.EndedTurn != "" {
		t.Errorf("another session's Stop ended this session's turn")
	}
	if _, err := os.Stat(sessionPath(f.root, "planning2")); err == nil {
		t.Errorf("a Stop outside every project left a session record in a project that had none for that session")
	}
	// 4) the project's mark is gone: the project is not active, nothing is written
	mark := filepath.Join(f.root, "project.json")
	kept, err := os.ReadFile(mark)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mark); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(sessionPath(f.root, "planning"))
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	after, _ := os.ReadFile(sessionPath(f.root, "planning"))
	if string(before) != string(after) {
		t.Errorf("a project without its mark had its session record rewritten by a Stop outside it")
	}
	if err := os.WriteFile(mark, kept, 0o600); err != nil {
		t.Fatal(err)
	}
	// 5) now it is written, once; a second Stop leaves the record's bytes alone
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	s = loadSession(f.root, "planning")
	if s.EndedTurn == "" || s.EndedTurn != s.TurnID {
		t.Errorf("control: the Stop outside ends the turn: ended=%q turn=%q", s.EndedTurn, s.TurnID)
	}
	before, _ = os.ReadFile(sessionPath(f.root, "planning"))
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	after, _ = os.ReadFile(sessionPath(f.root, "planning"))
	if string(before) != string(after) {
		t.Errorf("a repeated Stop outside rewrote the record")
	}
	// 6) a session record that is not JSON
	if err := os.WriteFile(sessionPath(f.root, "broken"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "broken", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	if b, _ := os.ReadFile(sessionPath(f.root, "broken")); string(b) != "{not json" {
		t.Errorf("an unreadable session record was rewritten: %q", b)
	}
}

func auditELastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// A Stop that carries a subagent's id (agent_id). What it writes to the parent's record.
func TestAuditEStopWithAnAgentID(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "agent_id": "sub-1", "agent_type": "Explore", "cwd": t.TempDir(), "last_assistant_message": "Subagent done."})
	s := loadSession(f.root, "planning")
	t.Logf("a Stop with agent_id outside the project: out=%q ended=%q turn=%s", out, s.EndedTurn, s.TurnID)
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	t.Logf("carried after it: %.120s", auditJSON(second["carried"]))
	if s.EndedTurn != "" {
		t.Errorf("a Stop that carries a subagent's id ended the parent's turn (the parent agent is still working): ended=%q", s.EndedTurn)
	}
}

// A Stop that carries a subagent's id, in the project itself.
func TestAuditEStopWithAnAgentIDInTheProject(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	done, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}, "result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"new1"}}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: %v", err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "agent_id": "sub-1", "agent_type": "Explore", "cwd": f.repo, "last_assistant_message": "Subagent done."})
	s := loadSession(f.root, "planning")
	t.Logf("a Stop with agent_id in the project: out=%q ended=%v judged=%v previous=%q", out, s.EndedTurn == s.TurnID, s.Judged, s.Previous)
	if s.EndedTurn == s.TurnID {
		t.Errorf("a Stop that carries a subagent's id ended the parent's turn and was judged as the parent's Stop (judged=%v, the kept reply is %q)", s.Judged, s.Previous)
	}
}

// The turn ended with the shell outside the project; a completion notice arrives there too and the agent works on (back in the
// project, with the same receipt); then the person sends a message.
func TestAuditENoticeOutsideTheProjectAfterAStopOutside(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": elsewhere, "last_assistant_message": "Started; I will report when the background run ends."})
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": elsewhere, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Background run", "content": "The background export wrote 40 rows."}}}); err != nil {
		t.Fatalf("precondition: the receipt still works: %v", err)
	}
	s := loadSession(f.root, "planning")
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	t.Logf("ended==turn before the message: %v; carried: %.80s", s.EndedTurn == s.TurnID, auditJSON(second["carried"]))
	if second["carried"] == nil {
		t.Errorf("the agent worked on after a completion notice that came while its shell stood outside the project, the person sent a message, and the next preparation took nothing over")
	}
}

// The request of a session stays unfinished in project A; the person's next message comes while the agent's shell stands in
// project B (its hook starts a turn there); the Stop of that turn passes in B. Back in A, the next message.
func TestAuditEAnUnfinishedTurnLeftInAnotherProject(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project."})
	supplyReasoningFixture(otherRoot, "planning")
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "Done."})
	_, blocked := stopReason(t, out)
	a, b := loadSession(f.root, "planning"), loadSession(otherRoot, "planning")
	t.Logf("the Stop in B: blocked=%v; A: ended==turn %v; B: ended==turn %v", blocked, a.EndedTurn == a.TurnID, b.EndedTurn == b.TurnID)
	if blocked {
		t.Skip("the Stop in the other project is blocked in this fixture")
	}
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] != nil {
		t.Errorf("INVARIANT 9: the session went on to another request in another project and that turn ended at its Stop; back in the first project, the next preparation takes over the references of the request that was left there: %.160s", auditJSON(second["carried"]))
	}
}

// A completion notice outside every project, for a session with no record in a registered project, makes no record there (the
// end of a turn is taken back only where the session has a turn).
func TestAuditENoticeOutsideMakesNoRecordForAnotherSession(t *testing.T) {
	f := newAuditFlow(t)
	f.prepare(f.ask(planningTestRequest), "CSV export filters")
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "another-session", "cwd": t.TempDir(), "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	if _, err := os.Stat(sessionPath(f.root, "another-session")); !os.IsNotExist(err) {
		t.Errorf("a completion notice of another session, outside every project, made a record in this project: %v", err)
	}
}
