package main

import (
	"testing"
)

// The fifth audit of 0.41.0 (medium 2). A subagent the parent continues, after its first turn ended, thinks inside OpenCode with no
// event of the parent's: shikii keeps a subagent open from its start to its end, so each turn of the subagent's session is told, the
// first one with its task and every later one by a start of its own (a message that joins the turn that is running is not a new turn).
func TestOpenCodePluginTellsTheStartAndTheEndOfEveryTurnOfASubagent(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "subagent-start", 0, additionalJSON("FOR-THE-CHILD"))
	reply := []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done."}}}}
	run, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1", "agent": "general"}},
		"messages": map[string]any{"child": reply},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "You are a subagent spawned by another session.\nRead the notes and report."},
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "one more thing"}, // the parent continues it: a turn of its own
			{"op": "prompt", "sid": "child", "text": "and this too"},   // joins that turn
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "a third turn"},
			{"op": "idle", "sid": "child"},
		}})
	if len(run.Errors) != 0 {
		t.Fatalf("errors: %v", run.Errors)
	}
	var order []string
	for _, c := range calls {
		if c.Event == "subagent-start" || c.Event == "subagent-stop" {
			if c.Stdin["session_id"] != "s1" || c.Stdin["agent_id"] != "child" || c.Stdin["agent_type"] != "general" {
				t.Errorf("a start or an end is of the subagent under the parent's session: %+v", c)
			}
			order = append(order, c.Event)
		}
	}
	want := []string{"subagent-start", "subagent-stop", "subagent-start", "subagent-stop", "subagent-start", "subagent-stop"}
	if len(order) != len(want) {
		t.Fatalf("every turn of the subagent is told at its start and at its end: got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("a start comes before the end of its turn: got %v, want %v", order, want)
		}
	}
	starts := 0
	for _, c := range calls {
		if c.Event != "subagent-start" {
			continue
		}
		if starts++; starts > 1 && (c.Stdin["prompt"] != nil && c.Stdin["prompt"] != "") {
			t.Errorf("a later start carries no task: %+v", c)
		}
	}
}
