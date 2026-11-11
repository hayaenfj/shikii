package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// audit A: many pending conversations (each with its targets and one delivered lesson): the heads alone are what the bound must hold.
func TestAuditAReflectQueryWithManyPendingConversations(t *testing.T) {
	for _, n := range []int{12, 25, 42} {
		t.Run(fmt.Sprintf("%d conversations", n), func(t *testing.T) {
			st, scope, h, _, _ := memoryEffectFixture(t)
			for i := 0; i < n; i++ {
				session := fmt.Sprintf("s%d", i)
				memoryEffectPrompt(t, scope, session)
				memoryEffectComplete(t, st, scope, session)
			}
			var cli map[string]any
			out := capture(t, func() error { return cmdReflect([]string{"query"}) })
			if err := json.Unmarshal([]byte(out), &cli); err != nil {
				t.Fatalf("%v: %.200s", err, out)
			}
			sources, _ := cli["sources"].([]any)
			heads, memories, targets := 0, 0, 0
			for _, raw := range sources {
				s := raw.(map[string]any)
				if s["head"] == true {
					heads++
				}
				ms, _ := s["memories"].([]any)
				ts, _ := s["targets"].([]any)
				memories += len(ms)
				targets += len(ts)
			}
			t.Logf("CLI: %d bytes, %d sources (%d heads), %d memories, %d targets, more_pending=%v, output=%v", len(out), len(sources), heads, memories, targets, cli["more_pending"], cli["output"])
			if len(out) > 28<<10 {
				t.Errorf("the CLI reply of reflect query is %d bytes, over 28672", len(out))
			}
			result, size := mcpToolCall(t, h, "shikii_reflect", `{"operation":"query"}`)
			t.Logf("MCP: %d bytes, output=%v", size, result["structuredContent"].(map[string]any)["output"])
			if size > 64<<10 {
				t.Errorf("the MCP reply of reflect query is %d bytes, over 65536", size)
			}
		})
	}
}
