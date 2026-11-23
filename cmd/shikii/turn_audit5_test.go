package main

import (
	"testing"
	"time"
)

// The fifth audit of 0.41.0. A subagent the agent continued (Claude Code's SendMessage) may reach shikii with no SubagentStart: the events
// that carry its id are the only thing that says it works. And the process of the agent is noted at the time its hook began, so that the
// subagent that hook itself started is not taken for one of an earlier process.

// An event that carries the id of a subagent opens that subagent when no start was told; the end closes it. The first time is kept.
func TestAnEventOfASubagentNoStartWasToldOfOpensIt(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("s1", "/p/a/.shikii", "turn-1")
	sessionTurnEvent("stop", &hookInput{SessionID: "s1"}, sessionTurnAt("s1"), false)
	first := time.Now().Add(time.Hour).UTC()
	sessionTurnEvent("tool-lesson", &hookInput{SessionID: "s1", AgentID: "agent-x", at: first}, "", false)
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 1 || !l.Subagents["agent-x"].Equal(first) {
		t.Fatalf("a tool call of a subagent no start was told of did not open it: %+v", l.Subagents)
	}
	sessionTurnEvent("tool-lesson", &hookInput{SessionID: "s1", AgentID: "agent-x", at: first.Add(time.Minute)}, "", false)
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 1 || !l.Subagents["agent-x"].Equal(first) {
		t.Errorf("a later event of the same subagent changed when it began: %+v", l.Subagents)
	}
	sessionTurnEvent("tool-lesson", &hookInput{SessionID: "s1", at: first.Add(2 * time.Minute)}, "", false)
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", AgentID: "agent-y", at: first.Add(3 * time.Minute)}, "", false)
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 1 {
		t.Errorf("an event of the agent itself, or the Stop of a subagent, opened a subagent: %+v", l.Subagents)
	}
	sessionTurnEvent("subagent-stop", &hookInput{SessionID: "s1", AgentID: "agent-x"}, "", false)
	if l := latestOfSession(t, "s1"); len(l.Subagents) != 0 {
		t.Errorf("the end of the subagent did not close it: %+v", l.Subagents)
	}
}

// forget keeps refusing a session while such a subagent thinks in the background.
func TestForgetRefusesASessionWhileASubagentOnlyItsEventsToldOfIsOpen(t *testing.T) {
	w := newForgetWorld(t)
	w.idle(w.A)
	noteSessionRequest(w.A, w.root, "t2")
	w.hears(w.A, "tool-lesson", "agent-x") // continued by the agent: no SubagentStart came
	w.turnEnds(w.A)
	w.refuses(w.A, "a subagent it started is still open (agent-x, begun ")
	w.hears(w.A, "subagent-stop", "agent-x")
	w.turnEnds(w.A)
	w.takes(w.A)
}

// The hook that first writes the process of the agent is the one a subagent began in: the process is noted at the time that hook began,
// so the subagent is its own. (The request hook's question to the process table may have run out, so the first event that writes the
// process is a SubagentStart.)
func TestASubagentTheFirstEventOfTheAgentProcessStartedIsCounted(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	root, _, err := projectStoreAt(repo)
	if err != nil {
		t.Fatal(err)
	}
	useProcessTable(t, agentFamily("claude"))
	useParent(t, 500)
	noteSessionRequest("ses_first", root, "turn-1") // the request's hook noted the turn but could not write the process
	runHook(t, "subagent-start", map[string]any{"session_id": "ses_first", "cwd": repo, "agent_id": "agent-x", "agent_type": "Explore"})
	p := hookProcessOf(t, root, "ses_first")
	if p == nil || p.Noted.IsZero() {
		t.Fatalf("the process was not noted: %+v", p)
	}
	l := latestOfSession(t, "ses_first")
	if id, _ := openSubagent(l, p.Noted); id != "agent-x" {
		t.Errorf("the subagent began in the hook that noted the process (%v) and is not counted: begun %v, open %q", p.Noted, l.Subagents, id)
	}
}

// An agent that names the message of each turn (OpenCode): the request hook the agent stopped for its time never wrote the request, but it
// said first, before the slow work, which message the turn is of. The Stop of that message closes the turn; before, the state kept the
// message of the request before and the Stop of the new one was taken for a Stop of another turn, so the turn stayed open to the next request.
func TestAStopClosesTheTurnOfARequestWhoseHookWasStoppedForItsTime(t *testing.T) {
	freshMachine(t)
	noteSessionRequestKeyed("s1", "/p/a/.shikii", "turn-1", "msg-1")
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", TurnKey: "msg-1"}, sessionTurnAt("s1"), false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Fatalf("precondition: the Stop closed the first turn")
	}
	// the first thing the request hook does, and nothing after it: the agent stopped it
	sessionActivity("user-prompt-submit", &hookInput{SessionID: "s1", Prompt: "Next, rename the export button.", TurnKey: "msg-2", at: time.Now().Add(time.Hour).UTC()})
	if l, open := inTurnOf(t, "s1"); !open || l.Key != "msg-2" {
		t.Fatalf("the request did not open the turn with the key of its message: open %v, %+v", open, l)
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", TurnKey: "msg-1", at: time.Now().Add(2 * time.Hour).UTC()}, sessionTurnAt("s1"), false)
	if _, open := inTurnOf(t, "s1"); !open {
		t.Errorf("a late Stop of the first message closed the turn of the second")
	}
	sessionTurnEvent("stop", &hookInput{SessionID: "s1", TurnKey: "msg-2", at: time.Now().Add(3 * time.Hour).UTC()}, sessionTurnAt("s1"), false)
	if _, open := inTurnOf(t, "s1"); open {
		t.Errorf("the Stop of the message of a request whose hook was stopped did not close its turn")
	}
}
