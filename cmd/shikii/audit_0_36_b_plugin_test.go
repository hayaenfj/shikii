package main

import "testing"

// P7: a continued subagent whose new turn ends with a new message of the same text as its earlier one.
func TestAudit036ConfirmPluginReportsANewMessageWithTheSameText(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1"}},
		"messages": map[string]any{"child": []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done.\nshikii: lesson:0123456789abcdef:0123456789abcdef unused"}}}}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "a task"},
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "one more thing"},
			{"op": "say", "sid": "child", "text": "Done.\nshikii: lesson:0123456789abcdef:0123456789abcdef unused"},
			{"op": "idle", "sid": "child"},
		}})
	if n := countCalls(calls, "subagent-stop"); n != 2 {
		t.Fatalf("the second turn added a message (the same words), so the subagent is reported twice, got %d: %+v", n, calls)
	}
}

// control: a new message with other words is reported.
func TestAudit036ConfirmPluginReportsANewMessageWithOtherText(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1"}},
		"messages": map[string]any{"child": []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done."}}}}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "a task"},
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "one more thing"},
			{"op": "say", "sid": "child", "text": "Done again."},
			{"op": "idle", "sid": "child"},
		}})
	if n := countCalls(calls, "subagent-stop"); n != 2 {
		t.Fatalf("control: a new message with other words is reported, got %d: %+v", n, calls)
	}
}
