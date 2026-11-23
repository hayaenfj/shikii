package main

import (
	"context"
	"testing"
	"time"
)

// The fourth audit of 0.41.0 found that shikii forget could take a session that was at work: the state of the session's turn (sessionLatest)
// said "no turn is open" while the agent went on. These tests are for what the hook writes there: a subagent that goes on after the
// parent's Stop, a turn that goes on after a Stop, a Stop of an earlier turn that arrives late, and a request hook that is stopped
// before it gets to write.

func latestOfSession(t *testing.T, session string) sessionLatest {
	t.Helper()
	latest, known, err := readSessionLatest(session)
	if err != nil || !known {
		t.Fatalf("the state of %s cannot be read: %v %v", session, known, err)
	}
	return latest
}

// A subagent that begins is kept until it ends, whatever the Stop of the session does; a Stop closes the turn and leaves it.
func TestTheStateKeepsTheSubagentsThatBeganAndHaveNotEnded(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
	begin := func(agent string) {
		sessionTurnEvent("subagent-start", &hookInput{SessionID: "s1", AgentID: agent}, "", false)
	}
	begin("agent-x")
	begin("agent-y")
	begin("agent-x") // told twice: one subagent
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 2 || l.Subagents["agent-x"].IsZero() || l.Subagents["agent-y"].IsZero() {
		t.Fatalf("two subagents began: %+v", l.Subagents)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	if l, open := inTurnOf(t, "s1"); open || len(l.Subagents) != 2 {
		t.Errorf("the Stop of the session closed the turn but must leave the subagents: open %v, %+v", open, l.Subagents)
	}
	// the next request of the person leaves the subagents that are still open
	noteSessionRequest("s1", "/p/a/.shikii", "turn-2")
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 2 {
		t.Errorf("a new request dropped the subagents that had not ended: %+v", l.Subagents)
	}
	sessionTurnEvent("subagent-stop", &hookInput{SessionID: "s1", AgentID: "agent-x"}, "", false)
	sessionTurnEvent("subagent-stop", &hookInput{SessionID: "s1", AgentID: "agent-x"}, "", false) // told twice
	sessionTurnEvent("subagent-stop", &hookInput{SessionID: "s1", AgentID: "never-began"}, "", false)
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 1 || l.Subagents["agent-y"].IsZero() {
		t.Errorf("agent-x ended: only agent-y is open: %+v", l.Subagents)
	}
	sessionTurnEvent("subagent-stop", &hookInput{SessionID: "s1", AgentID: "agent-y"}, "", false)
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 0 {
		t.Errorf("both ended: %+v", l.Subagents)
	}
	// a session with no state is given none by a subagent
	sessionTurnEvent("subagent-start", &hookInput{SessionID: "new", AgentID: "agent-x"}, "", false)
	if _, known, _ := readSessionLatest("new"); known {
		t.Errorf("a subagent made a state for a session that has none")
	}
}

// After a Stop, any event of the session says that the agent works on: a tool call, a message, a subagent beginning or ending (the
// subagent's own events too), the notice of finished work. Only a Stop that the turn is not overtaken at closes it again.
func TestAnEventAfterAStopOpensTheTurnAgain(t *testing.T) {
	for _, c := range []struct {
		name  string
		event string
		in    hookInput
	}{
		{"a tool call", "tool-lesson", hookInput{}},
		{"a tool call of a subagent", "tool-lesson", hookInput{AgentID: "agent-x"}},
		{"a subagent beginning", "subagent-start", hookInput{AgentID: "agent-x"}},
		{"a subagent ending", "subagent-stop", hookInput{AgentID: "agent-x"}},
		{"the end of a subagent no id is told of", "subagent-stop", hookInput{}},
		{"the task of the old hook", "agent-task", hookInput{}},
		{"a completion notice", "user-prompt-submit", hookInput{Prompt: auditINotice}},
		{"a slash command", "user-prompt-submit", hookInput{Prompt: "/commit fix the docs"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			freshMachine(t)
			noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
			sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
			if _, open := inTurnOf(t, "s1"); open {
				t.Fatalf("precondition: the Stop closed the turn")
			}
			in := c.in
			in.SessionID = "s1"
			sessionTurnEvent(c.event, &in, "", false)
			l, open := inTurnOf(t, "s1")
			if !open || l.EventAt.IsZero() || !l.EventAt.After(l.ClosedAt) {
				t.Errorf("the event did not open the turn, or did not note when it came: open %v, event at %v, closed at %v", open, l.EventAt, l.ClosedAt)
			}
			sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
			if _, open := inTurnOf(t, "s1"); open {
				t.Errorf("the Stop of the turn that went on did not close it")
			}
		})
	}
	t.Run("the Stop of a subagent is no event of the session", func(t *testing.T) {
		freshMachine(t)
		noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
		sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
		before := latestOfSession(t, "s1")
		sessionTurnEvent("stop", &hookInput{SessionID: "s1", AgentID: "agent-x"}, sessionTurnAt("s1"), false)
		if after := latestOfSession(t, "s1"); after.EventAt != before.EventAt || after.ClosedAt != before.ClosedAt || *after.InTurn {
			t.Errorf("the Stop of a subagent changed the state of the turn: %+v -> %+v", before, after)
		}
	})
}

// An event that reached the hook after a Stop began means the turn went on: the Stop, which reads and judges for seconds, closes only
// what no event overtook. A Stop that began after the event closes it.
func TestAStopDoesNotCloseATurnAnEventOvertook(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
	began := time.Now().Add(time.Hour).UTC() // after everything noted so far
	sessionTurnEvent("tool-lesson", &hookInput{SessionID: "s1", at: began.Add(time.Second)}, "", false)
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", at: began}, "turn-1", false)
	if l, open := inTurnOf(t, "s1"); !open || !l.ClosedAt.IsZero() {
		t.Fatalf("a Stop that began before an event closed the turn: %+v", l)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", at: began.Add(2 * time.Second)}, "turn-1", false)
	if l, open := inTurnOf(t, "s1"); open || !l.ClosedAt.Equal(began.Add(2*time.Second)) {
		t.Errorf("the Stop that began after the event did not close the turn at the time it began: %+v", l)
	}
}

// The Stop of an earlier request that reaches the hook after the next request was noted (OpenCode asks for the conversation before it
// starts the hook) names the message it is of: it does not end the turn of the next request. An agent that names no message ends the
// turn it read when the Stop began.
func TestAStopOfAnEarlierRequestDoesNotCloseTheNextOnesTurn(t *testing.T) {
	freshMachine(t)
	noteSessionRequestKeyed("s1", "/p/a/.shikii", "turn-1", "msg-1")
	noteSessionRequestKeyed("s1", "/p/a/.shikii", "turn-2", "msg-2")
	if l := latestOfSession(t, "s1"); l.Key != "msg-2" || l.Turn != "turn-2" {
		t.Fatalf("the request did not keep the key of its message: %+v", l)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", TurnKey: "msg-1"}, sessionTurnAt("s1"), false)
	if l, open := inTurnOf(t, "s1"); !open || l.Ended {
		t.Fatalf("the Stop of the first request closed the second: %+v", l)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", TurnKey: "msg-2"}, sessionTurnAt("s1"), false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Errorf("the Stop of the request itself did not close its turn")
	}
	noteSessionRequestKeyed("s1", "/p/a/.shikii", "turn-3", "")
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Errorf("an agent that names no message did not end the turn with its Stop")
	}
}

// The request hook says the agent works before anything of its slow work (the processes, the working tree, the models): a hook that the
// agent stops for its time never gets to the end, and the turn would stay closed through a request that goes on. At the hook's first
// question to the process table the turn of an earlier Stop is open again.
func TestTheRequestHookOpensTheTurnBeforeItsSlowWork(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	if _, open := inTurnOf(t, "planning"); open {
		t.Fatalf("precondition: the Stop closed the turn")
	}
	var openAtTheFirstQuestion *bool
	old := readProcess
	readProcess = func(ctx context.Context, pid int) (processRow, processState) {
		if openAtTheFirstQuestion == nil {
			latest, _, _ := readSessionLatest("planning")
			open := latest.InTurn != nil && *latest.InTurn
			openAtTheFirstQuestion = &open
		}
		return old(ctx, pid)
	}
	t.Cleanup(func() { readProcess = old })
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": "Next, rename the export button.", "turn_key": "msg-7"})
	switch {
	case openAtTheFirstQuestion == nil:
		t.Fatalf("the hook did not ask the process table")
	case !*openAtTheFirstQuestion:
		t.Errorf("the turn was still closed when the hook began its slow work")
	}
	if l, open := inTurnOf(t, "planning"); !open || l.Key != "msg-7" {
		t.Errorf("the request does not hold the turn open with the key of its message: %+v", l)
	}
}

// Through the hooks, in the order Claude Code runs them: the request, a subagent in the background, the Stop of the session, the end of
// the subagent and the Stop of the turn that report starts. At each point the state says what forget must go by.
func TestTheHooksKeepABackgroundSubagentOpenThroughTheStopOfItsSession(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	hook := func(event string, in map[string]any) {
		t.Helper()
		in["session_id"], in["cwd"] = "planning", f.repo
		runUnreviewedHook(t, event, in)
	}
	hook("subagent-start", map[string]any{"agent_id": "agent-x", "agent_type": "Explore"})
	hook("stop", map[string]any{"last_assistant_message": "Started the audit in the background."})
	if l, open := inTurnOf(t, "planning"); open || len(l.Subagents) != 1 {
		t.Fatalf("after the Stop the turn is closed and the subagent open: open %v, %+v", open, l.Subagents)
	}
	hook("subagent-stop", map[string]any{"agent_id": "agent-x", "last_assistant_message": "done"})
	if l, open := inTurnOf(t, "planning"); !open || len(l.Subagents) != 0 {
		t.Errorf("the end of the subagent closed it and reopened the turn that its report starts: open %v, %+v", open, l.Subagents)
	}
	hook("user-prompt-submit", map[string]any{"prompt": auditINotice})
	hook("stop", map[string]any{"last_assistant_message": "The audit is done."})
	if l, open := inTurnOf(t, "planning"); open || len(l.Subagents) != 0 {
		t.Errorf("after the Stop of the turn the report started nothing is open: open %v, %+v", open, l.Subagents)
	}
}
