package main

import (
	"testing"
)

// The state of the session's turn says whether the agent is in the middle of a turn (sessionLatest.InTurn): any message of the person
// that reached the hook opens it, a slash command that is no request among them, and a Stop that lets the turn end closes it. shikii
// forget goes by it (forget_idle.go). Ended, the request's own end, is left as it was by a slash command.

func inTurnOf(t *testing.T, session string) (latest sessionLatest, open bool) {
	t.Helper()
	latest, known, err := readSessionLatest(session)
	if err != nil || !known || latest.InTurn == nil {
		t.Fatalf("the state of %s does not say whether a turn is open: %+v %v %v", session, latest, known, err)
	}
	return latest, *latest.InTurn
}

func TestTheTurnIsOpenedByAnyMessageAndClosedByAStopThatLetsItEnd(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
	if _, open := inTurnOf(t, "s1"); !open {
		t.Errorf("a request did not open the turn")
	}
	// a Stop a check stopped, a subagent's Stop, and a Stop of an earlier request leave it open
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), true)
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", AgentID: "child"}, sessionTurnAt("s1"), false)
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, "turn-0", false)
	if _, open := inTurnOf(t, "s1"); !open {
		t.Errorf("a Stop that does not end this turn closed it")
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	if latest, open := inTurnOf(t, "s1"); open || !latest.Ended {
		t.Errorf("the Stop that lets the turn end did not close it: %+v", latest)
	}
	// a slash command is no request: the request stays ended, and the agent works on the command until its Stop
	slash := &hookInput{SessionID: "s1", Prompt: "/commit fix the docs"}
	sessionTurnEvent("user-prompt-submit", &hookInput{SessionID: "s1", AgentID: "child", Prompt: slash.Prompt}, "", false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Errorf("a subagent's message opened the turn")
	}
	sessionTurnEvent("user-prompt-submit", slash, "", false)
	if latest, open := inTurnOf(t, "s1"); !open || !latest.Ended || latest.Turn != "turn-1" {
		t.Errorf("a slash command did not open the turn, or changed the request: %+v", latest)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Errorf("the Stop of the slash command's turn did not close it")
	}
	// a message with no text is no request either, and the agent works on it
	sessionTurnEvent("user-prompt-submit", &hookInput{SessionID: "s1", Prompt: "  "}, "", false)
	if latest, open := inTurnOf(t, "s1"); !open || !latest.Ended {
		t.Errorf("a message with no text did not open the turn, or changed the request: %+v", latest)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	// a completion notice reopens the request and the turn
	sessionTurnEvent("user-prompt-submit", &hookInput{SessionID: "s1", Prompt: auditINotice}, "", false)
	if latest, open := inTurnOf(t, "s1"); !open || latest.Ended {
		t.Errorf("a completion notice did not reopen the turn: %+v", latest)
	}
	// a message that is neither (the request itself is noted by the request hook) changes nothing here
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	sessionTurnEvent("user-prompt-submit", &hookInput{SessionID: "s1", Prompt: "Explain the page."}, "", false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Errorf("a request message opened the turn in a place that does not note requests")
	}
}

// A session that has no state is not given one by a slash command: the state is the request's, and a session from before it stays
// unknown (forget refuses it).
func TestASlashCommandMakesNoStateForASessionThatHasNone(t *testing.T) {
	freshMachine(t)
	sessionTurnEvent("user-prompt-submit", &hookInput{SessionID: "new", Prompt: "/model sonnet"}, "", false)
	if _, known, _ := readSessionLatest("new"); known {
		t.Errorf("a slash command made a state of the turn")
	}
}

// A state of an earlier shikii (no in_turn) is closed by a Stop and then says so; a state with the field has it written both ways.
func TestAStopGivesAnOlderStateTheAnswerThatNoTurnIsOpen(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("old", "/p/a/.shikii", "turn-1")
	updateSessionLatest("old", func(latest *sessionLatest, _ bool) bool { latest.InTurn = nil; return true }) // as an earlier shikii wrote it
	if latest, _, _ := readSessionLatest("old"); latest.InTurn != nil {
		t.Fatalf("precondition: the state has no in_turn: %+v", latest)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "old"}, sessionTurnAt("old"), false)
	if _, open := inTurnOf(t, "old"); open {
		t.Errorf("a Stop left an older state open")
	}
}

// At the hook: a request opens the turn, its Stop closes it, a slash command of the person opens it again (in its project, and where
// the hook leaves early: outside every project), and the next Stop closes it. The hook's own events are used, not the state's.
func TestTheHookOpensTheTurnForASlashCommandAndItsStopClosesIt(t *testing.T) {
	f := auditCFinishedFirstRequest(t) // a request whose work is recorded, so its Stop passes
	if _, open := inTurnOf(t, "planning"); !open {
		t.Fatalf("precondition: the request opened the turn")
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop passes: %s", reason)
	}
	if _, open := inTurnOf(t, "planning"); open {
		t.Fatalf("the Stop of the request did not close the turn")
	}
	for name, cwd := range map[string]string{"in its project": f.repo, "outside every project": t.TempDir()} {
		runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": cwd, "prompt": "/commit fix the docs"})
		if latest, open := inTurnOf(t, "planning"); !open || !latest.Ended {
			t.Errorf("a slash command %s did not open the turn (or reopened the request): %+v", name, latest)
		}
		if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Committed."})); blocked {
			t.Fatalf("the Stop of the slash command's turn was stopped: %s", reason)
		}
		if _, open := inTurnOf(t, "planning"); open {
			t.Errorf("the Stop after a slash command %s did not close the turn", name)
		}
	}
}
