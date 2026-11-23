package main

import "testing"

// The sixth audit of 0.41.0 (low 1). The plugin tells the start of every turn of a subagent; only the first one asks shikii for the child's
// rules. A later one says it is a later turn (continued), so shikii delivers nothing and records no delivery the child never sees.
func TestOpenCodePluginMarksEveryLaterStartOfASubagentAsContinued(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "subagent-start", 0, additionalJSON("FOR-THE-CHILD"))
	reply := []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done."}}}}
	run, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1", "agent": "general"}},
		"messages": map[string]any{"child": reply},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "You are a subagent spawned by another session.\nRead the notes and report."},
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "one more thing"},
			{"op": "prompt", "sid": "child", "text": "and this too"}, // joins that turn: no start
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "a third turn"},
			{"op": "idle", "sid": "child"},
		}})
	if len(run.Errors) != 0 {
		t.Fatalf("errors: %v", run.Errors)
	}
	var continued []bool
	for _, c := range calls {
		if c.Event == "subagent-start" {
			flag, _ := c.Stdin["continued"].(bool)
			continued = append(continued, flag)
		}
	}
	if len(continued) != 3 || continued[0] || !continued[1] || !continued[2] {
		t.Fatalf("the first start is of the first turn and the later two say they are continued: %v", continued)
	}
}
