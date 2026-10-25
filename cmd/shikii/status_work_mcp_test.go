package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// A truth an agent sent through MCP (source mcp) is not the person's answer. The page's "truths decided by you" must not count it.
func TestCountPersonWorkLeavesOutMCPTruths(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	recs := []store.Record{{TraceID: "a1", Band: "ask", At: now}, {TraceID: "a2", Band: "ask", At: now}}
	labs := []store.Label{{TraceID: "a1", Source: "ask"}, {TraceID: "a2", Source: "mcp"}}
	if w := countPersonWork("needs-plan", false, recs, labs, nil, now); w.ByPerson != 1 {
		t.Fatalf("truths decided by the person: %d, want 1 (the mcp truth is the agent's): %+v", w.ByPerson, w)
	}
}
