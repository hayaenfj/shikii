package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// subagentStopFixture starts a parent turn that received the lesson and a subagent whose task reaches the same lesson. It returns
// the store, the scope, the lesson's key, the context the subagent was given and the task that reaches the lesson.
func subagentStopFixture(t *testing.T, session, agentID string) (store.Store, string, string, string, string) {
	t.Helper()
	st, scope, _, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, session)
	task := draft.Title + " " + draft.AppliesTo
	ctx := subagentContext(t, startSubagent(t, session, scope, agentID, "general-purpose", task))
	key := "lesson:" + draft.ID + ":" + revision
	if !strings.Contains(ctx, key) {
		t.Fatalf("precondition: the subagent received the lesson: %s", ctx)
	}
	return st, scope, key, ctx, task
}

// subagentAssessments returns the assessments of the deliveries to agentID, the unassessed ones as nil.
func subagentAssessments(t *testing.T, st store.Store, scope, agentID string) []*store.MemoryAssessment {
	t.Helper()
	report, err := st.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	var out []*store.MemoryAssessment
	for _, item := range report.All {
		if item.Agent == agentID {
			out = append(out, item.Assessment)
		}
	}
	return out
}

func subagentStop(t *testing.T, session, scope, agentID, report string) {
	t.Helper()
	if out := runHook(t, "subagent-stop", map[string]any{"session_id": session, "cwd": scope, "agent_id": agentID, "agent_type": "general-purpose", "last_assistant_message": report}); out != "" {
		t.Fatalf("the hook outputs nothing, so the subagent ends with the report it wrote: %q", out)
	}
}

// Only the one a lesson was delivered to can tell whether it was used. The subagent is told how to say it, the hook saves what its
// final report says while the parent's turn is still running, and the parent's own delivery of the same lesson is left to the
// parent's reflection.
func TestSubagentAssessesItsOwnDeliveriesInItsFinalReport(t *testing.T) {
	st, scope, key, ctx, _ := subagentStopFixture(t, "s", "agent-1")
	if !strings.Contains(ctx, "shikii: <its key> helpful - <what you did because of it>") || !strings.Contains(ctx, "shikii: <its key> unused") {
		t.Fatalf("the subagent is told how to report what it did with a lesson: %s", ctx)
	}
	if rep, err := st.MemoryReport(scope); err != nil || rep.Stats.Subagent != 1 || rep.Stats.Pending != 1 {
		t.Fatalf("before the subagent ends, its delivery is counted apart from the parent's: %+v %v", rep.Stats, err)
	}
	if out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) }); !strings.Contains(out, "delivered to subagents and not assessed by them 1") {
		t.Fatalf("status says how many deliveries to subagents are not assessed:\n%s", out)
	}
	subagentStop(t, "s", scope, "agent-1", "Updated the steps and ran the check.\n\n- shikii: "+key+" helpful - Added the check step before the install command.")
	got := subagentAssessments(t, st, scope, "agent-1")
	if len(got) != 1 || got[0] == nil || got[0].Result != "helpful" || got[0].Agent != "agent-1" || got[0].Decision != "Added the check step before the install command." || got[0].Phrase != "task" {
		t.Fatalf("the subagent's own report is saved as the assessment of its delivery: %+v", got)
	}
	rep, err := st.MemoryReport(scope)
	if err != nil || rep.Stats.Subagent != 0 || rep.Stats.Helpful != 1 || rep.Stats.Pending != 1 {
		t.Fatalf("the parent's delivery of the same lesson stays with the parent's reflection: %+v %v", rep.Stats, err)
	}
	if out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) }); strings.Contains(out, "delivered to subagents") {
		t.Fatalf("with none left the line says nothing of subagents:\n%s", out)
	}
	// the first report stands
	subagentStop(t, "s", scope, "agent-1", "shikii: "+key+" unused")
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil || got[0].Result != "helpful" {
		t.Fatalf("an assessment is not overwritten: %+v", got)
	}
}

// unused needs no account of what was done; the form is the exact key and one of three results, and nothing else is read.
func TestSubagentStopReadsOnlyTheExactLines(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	other := key[:len(key)-1] + "0"
	if other == key {
		other = key[:len(key)-1] + "1"
	}
	for name, report := range map[string]string{
		"no line":                           "Updated the steps.",
		"a result a subagent cannot give":   "shikii: " + key + " unknown - Could not tell.",
		"helpful without what was done":     "shikii: " + key + " helpful",
		"a key that is not a lesson's":      "shikii: lesson:readme helpful - Followed it.",
		"a revision that was not delivered": "shikii: " + other + " helpful - Followed it.",
		"the words inside a sentence":       "I would say shikii: " + key + " helpful - Followed it.",
		"another subagent":                  "",
	} {
		agent := "agent-1"
		if name == "another subagent" {
			agent, report = "agent-2", "shikii: "+key+" helpful - Followed it."
		}
		subagentStop(t, "s", scope, agent, report)
		if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] != nil {
			t.Fatalf("%s: nothing is saved: %+v", name, got)
		}
	}
	// the conversation's own agent is not a subagent: its Stop is not this hook, and a report without an agent id saves nothing
	subagentStop(t, "s", scope, "", "shikii: "+key+" helpful - Followed it.")
	if rep, err := st.MemoryReport(scope); err != nil || rep.Stats.Helpful != 0 || rep.Stats.Pending != 1 || rep.Stats.Subagent != 1 {
		t.Fatalf("a report without a subagent's id assesses nothing: %+v %v", rep.Stats, err)
	}
	subagentStop(t, "s", scope, "agent-1", "Read the files.\nshikii: "+key+" unused\n")
	got := subagentAssessments(t, st, scope, "agent-1")
	if len(got) != 1 || got[0] == nil || got[0].Result != "unused" || got[0].Agent != "agent-1" || strings.TrimSpace(got[0].Decision) == "" {
		t.Fatalf("unused is saved without an account of what was done: %+v", got)
	}
}

// The store accepts a subagent's assessment only for a delivery to that subagent: not for another subagent's, and not for the
// conversation's own, which waits for the person's next message as before.
func TestASubagentAssessesOnlyWhatWasDeliveredToIt(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	var own, child string
	for _, d := range deliveries {
		if d.Agent == "agent-1" {
			child = d.ID
		} else if d.Agent == "" {
			own = d.ID
		}
	}
	if own == "" || child == "" {
		t.Fatalf("precondition: one delivery to the parent and one to the subagent: %+v", deliveries)
	}
	a := store.MemoryAssessment{Key: key, Result: "helpful", Decision: "Followed it.", Evidence: "The final report."}
	for name, c := range map[string]struct{ source, agent, want string }{
		"another subagent":                       {child, "agent-2", "delivered to itself"},
		"a subagent on the parent's delivery":    {own, "agent-1", "delivered to itself"},
		"the parent before the next message":     {own, "", "the person's next message"},
		"the parent on the subagent's, likewise": {child, "", "the person's next message"},
	} {
		a.SourceID, a.Agent = c.source, c.agent
		if err := st.SaveMemoryAssessment(scope, a); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: refused with %q, got %v", name, c.want, err)
		}
	}
	a.SourceID, a.Agent = child, "agent-1"
	if err := st.SaveMemoryAssessment(scope, a); err != nil {
		t.Fatalf("the subagent assesses its own delivery while the parent's turn runs: %v", err)
	}
}

// Two subagents of one turn received the same lesson. The report of one assesses the delivery to that one, whichever was delivered
// last, and leaves the other's to the other.
func TestSubagentStopAssessesTheDeliveryToTheReportingSubagent(t *testing.T) {
	st, scope, key, _, task := subagentStopFixture(t, "s", "agent-1")
	if ctx := subagentContext(t, startSubagent(t, "s", scope, "agent-2", "general-purpose", task)); !strings.Contains(ctx, key) {
		t.Fatalf("precondition: the second subagent received the lesson too: %s", ctx)
	}
	subagentStop(t, "s", scope, "agent-1", "shikii: "+key+" helpful - Added the check step before the install command.")
	first, second := subagentAssessments(t, st, scope, "agent-1"), subagentAssessments(t, st, scope, "agent-2")
	if len(first) != 1 || first[0] == nil || first[0].Agent != "agent-1" || len(second) != 1 || second[0] != nil {
		t.Fatalf("the report of the first subagent assesses its own delivery and not the second's: first=%+v second=%+v", first, second)
	}
	subagentStop(t, "s", scope, "agent-2", "shikii: "+key+" unused")
	if second := subagentAssessments(t, st, scope, "agent-2"); len(second) != 1 || second[0] == nil || second[0].Result != "unused" || second[0].Agent != "agent-2" {
		t.Fatalf("the second subagent assesses its own: %+v", second)
	}
}

// An agent whose subagent start carries the task itself (OpenCode's plugin passes it as prompt) keeps no task at a tool call: the
// task given with the start picks the lessons, a task kept for another child stays for that child, and the subagent's report is
// saved the same way.
func TestSubagentStartPicksLessonsByTheTaskGivenWithTheStart(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "s")
	key := "lesson:" + draft.ID + ":" + revision
	task := draft.Title + " " + draft.AppliesTo
	if out := runHook(t, "agent-task", map[string]any{"session_id": "s", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"description": "d", "prompt": task, "subagent_type": "general"}}); out != "" {
		t.Fatalf("keeping a task outputs nothing: %q", out)
	}
	start := func(agentID string, extra map[string]any) string {
		in := map[string]any{"session_id": "s", "cwd": scope, "agent_id": agentID, "agent_type": "general"}
		for k, v := range extra {
			in[k] = v
		}
		return subagentContext(t, runHook(t, "subagent-start", in))
	}
	if ctx := start("agent-far", map[string]any{"prompt": "Count the words of an unrelated poem about the sea."}); strings.Contains(ctx, key) {
		t.Fatalf("a task given with the start picks the lessons, not a task kept for another child: %s", ctx)
	}
	if ctx := start("agent-given", map[string]any{"prompt": "  " + task + "\n"}); !strings.Contains(ctx, key) || !strings.Contains(ctx, "shikii: <its key> unused") {
		t.Fatalf("the task given with the start reaches its lesson and the way to report it: %s", ctx)
	}
	if ctx := start("agent-kept", nil); !strings.Contains(ctx, key) {
		t.Fatalf("the kept task is still there for the child that brings none: %s", ctx)
	}
	subagentStop(t, "s", scope, "agent-given", "shikii: "+key+" helpful - Added the check step before the install command.")
	got := subagentAssessments(t, st, scope, "agent-given")
	if len(got) != 1 || got[0] == nil || got[0].Result != "helpful" || got[0].Phrase != "task" {
		t.Fatalf("the report of a subagent started with its task is saved: %+v", got)
	}
}
