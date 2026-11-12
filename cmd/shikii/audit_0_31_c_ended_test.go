package main

import (
	"fmt"
	"testing"
)

// auditCFinishedFirstRequest runs one request to its recorded end and returns the flow (the Stop is the caller's).
func auditCFinishedFirstRequest(t *testing.T) *auditFlow {
	t.Helper()
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}, "result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"new1"}}})
	if err != nil || saved["complete"] != true {
		t.Fatalf("the first work is recorded: %v %.400s", err, auditJSON(saved))
	}
	return f
}

// audit C, finding A-1 (INVARIANTS 9): the Stop lets the turn end, and the model that judges the completion is not reachable (no
// key and the local model is not installed, still starting, or down). The hook says nothing and the agent's turn ends.
func TestAuditCStopWithoutAModelEndsTheTurn(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	working := hookLocalURL
	hookLocalURL = "http://127.0.0.1:1" // a closed port: the model cannot be asked at this Stop
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	hookLocalURL = working
	reason, blocked := stopReason(t, out)
	t.Logf("stop without a model: blocked=%v out=%q reason=%.200s", blocked, out, reason)
	if blocked {
		t.Fatalf("precondition: the Stop passes: %s", reason)
	}
	s := loadSession(f.root, "planning")
	t.Logf("turn=%s ended_turn=%q judged=%v completed=%v", s.TurnID, s.endedTurnForTest(), s.Judged, s.Completed != nil)
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] != nil {
		t.Errorf("INVARIANT 9: the Stop of the first request let the turn end (the model was not reachable, the hook said nothing), yet the next preparation took its references over: %s", auditJSON(second["carried"]))
	}
}

// control for the test above: the same flow with the model reachable hands nothing over.
func TestAuditCStopWithAModelEndsTheTurn(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	if reason, blocked := stopReason(t, out); blocked {
		t.Fatalf("precondition: the Stop passes: %s", reason)
	}
	s := loadSession(f.root, "planning")
	t.Logf("turn=%s ended_turn=%q", s.TurnID, s.endedTurnForTest())
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] != nil {
		t.Errorf("control: a passed Stop hands nothing over: %s", auditJSON(second["carried"]))
	}
}

// audit C, finding A-1: a machine whose model is not reachable for a whole session: every Stop passes without a word. What the
// third request is handed, and what its result can cite.
func TestAuditCRequestsWithoutAModelHandEverythingOn(t *testing.T) {
	f := newAuditFlow(t)
	working := hookLocalURL
	hookLocalURL = "http://127.0.0.1:1"
	defer func() { hookLocalURL = working }()
	var last map[string]any
	for i, prompt := range []string{planningTestRequest, "Now rename the export button on the reports page, a new task.", "Then change the invoice numbering option to start at 1000, another new task."} {
		prepared := f.prepare(f.ask(prompt), "CSV export")
		t.Logf("request %d: carried=%.60s sources=%s", i+1, auditJSON(prepared["carried"]), func() string {
			c, _ := prepared["carried"].(map[string]any)
			return auditJSON(c["sources"])
		}())
		last = prepared
		saved, err := f.record(prepared, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": fmt.Sprintf("Check %d", i+1), "content": fmt.Sprintf("Request %d was checked.", i+1)}}, "result": map[string]any{"changed": fmt.Sprintf("Did request %d.", i+1), "checked": "Ran it.", "evidence": []any{"new1"}}})
		if err != nil || saved["complete"] != true {
			t.Fatalf("request %d is recorded: %v %.300s", i+1, err, auditJSON(saved))
		}
		if i == 2 {
			break // the third request is the one looked at: its Stop is not part of this check
		}
		if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
			t.Fatalf("request %d: the Stop passes: %s", i+1, reason)
		}
	}
	carried, _ := last["carried"].(map[string]any)
	if carried == nil {
		return
	}
	t.Errorf("INVARIANT 9: every request ended at its Stop, and the third request was handed the sources of the two before it: %s", auditJSON(carried["sources"]))
	// observed1 is the first request's check (the third request's own observation has a later number)
	saved, err := f.record(last, map[string]any{"result": map[string]any{"changed": "Changed the invoice numbering again.", "checked": "See the earlier check.", "evidence": []any{"observed1"}}})
	t.Logf("the third request cites observed1 (the first request's check): err=%v complete=%v errors=%.200s", err, saved["complete"], auditJSON(saved["errors"]))
	if err == nil && saved["complete"] == true {
		t.Errorf("INVARIANT 9: the third request's result is complete with the first request's observation (observed1) as its evidence")
	}
}

// audit C, finding U-1: the turn ended (its Stop passed), a background task's completion notice arrives and the agent works on,
// on the same turn and with the same receipt; then the person sends a message.
func TestAuditCMessageDuringWorkAfterANotification(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}, "result": map[string]any{"changed": "Started the export in the background.", "checked": "Ran it.", "evidence": []any{"new1"}}})
	if err != nil || saved["complete"] != true {
		t.Fatalf("the first work is recorded: %v %.400s", err, auditJSON(saved))
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Started; I will report when the background run ends."})); blocked {
		t.Fatalf("precondition: the Stop passes: %s", reason)
	}
	turn := loadSession(f.root, "planning").TurnID
	// the background work ends: Claude Code sends its notice as a user message, and the agent works on
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"})
	s := loadSession(f.root, "planning")
	t.Logf("after the notice: same turn=%v ended_turn==turn: %v judged=%v", s.TurnID == turn, s.endedTurnForTest() == s.TurnID, s.Judged)
	if s.TurnID != turn {
		t.Fatalf("precondition: a notice keeps the turn")
	}
	more, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Background run", "content": "The background export wrote 40 rows."}}})
	if err != nil {
		t.Fatalf("precondition: the receipt still works after the notice: %v", err)
	}
	t.Logf("record after the notice: %.300s", auditJSON(more["saved"]))
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	t.Logf("carried: %s", auditJSON(second["carried"]))
	if second["carried"] == nil {
		t.Errorf("the agent was working again after a completion notice (same turn, same receipt, a new observation saved), the person sent a message, and the next preparation took nothing over: ended_turn still names the turn the notice reopened")
	}
}

// audit C, finding U-2: the agent's shell was in a folder that is not a shikii project when the turn ended: the Stop hook runs
// there, finds no project and says nothing.
func TestAuditCStopFromAnotherFolder(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	elsewhere := t.TempDir()
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": elsewhere, "last_assistant_message": "Done."})
	s := loadSession(f.root, "planning")
	t.Logf("stop from another folder: out=%q ended_turn=%q turn=%s", out, s.endedTurnForTest(), s.TurnID)
	second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if second["carried"] != nil {
		t.Errorf("the turn ended (its Stop ran in a folder outside the project), and the next preparation took its references over: %.120s", auditJSON(second["carried"]))
	}
}

// negative of the same: a Stop outside every project ends the turn of its own session only.
func TestAuditCStopFromAnotherFolderLeavesOtherSessions(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "another-session", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	if s := loadSession(f.root, "planning"); s.TurnID == "" || s.endedTurnForTest() != "" {
		t.Fatalf("the Stop of another session, outside the project, ended this session's turn: turn=%q ended=%q", s.TurnID, s.endedTurnForTest())
	}
	if second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals"); second["carried"] == nil {
		t.Errorf("the message came during the work, and the next preparation took nothing over")
	}
}
