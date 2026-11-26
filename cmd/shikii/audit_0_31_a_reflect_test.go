package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// audit A: many pending conversations (each with its targets and one delivered lesson): the heads alone are what the bound must hold.
//
// One store grows from 12 to 25 to 42 conversations and is read at each size, instead of three stores built from nothing (79
// conversations, each costing three hook runs and their Git calls). The sessions are named s0, s1, ... in the order they are added, so
// the store at size n is the store a fresh build of n conversations gives. Reading it changes nothing (reflect query writes no
// file: the bytes of the store are the same before and after a CLI and an MCP query), so the larger sizes do not depend on the
// reads of the smaller ones. A subtest run alone (-run .../25_conversations) builds all it needs itself.
func TestAuditAReflectQueryWithManyPendingConversations(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	built := 0 // conversations in the store so far
	for _, n := range []int{12, 25, 42} {
		t.Run(fmt.Sprintf("%d conversations", n), func(t *testing.T) {
			for ; built < n; built++ {
				session := fmt.Sprintf("s%d", built)
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
