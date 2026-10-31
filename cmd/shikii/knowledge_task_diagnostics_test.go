package main

import (
	"github.com/hayaenfj/shikii/internal/store"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeTaskCaseKeepsParentTurn(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, nil)
	const parent = "0000000000000171"
	snapshot := &hookSession{Scope: b.Scope, TurnID: parent, Request: "parent synthetic request", SubagentTasks: []subagentTask{{Type: "audit", Prompt: "workflow design", At: time.Now()}}}
	if err := saveSession(b.Root, "audit-task", snapshot); err != nil {
		t.Fatal(err)
	}
	out, _, err := hookSubagentStart(b.Root, &hookInput{SessionID: "audit-task", AgentID: "child-a", AgentType: "audit"}, snapshot)
	if err != nil || !strings.Contains(out.Context, p.ID) {
		t.Fatal("expected task delivery", err)
	}
	ds, err := (store.Store{Dir: b.Root}).MemoryDeliveries(b.Scope)
	if err != nil || len(ds) != 1 || ds[0].Parent != parent {
		t.Fatal("expected stored parent", err)
	}
	cases, err := b.DeliveryCases()
	if err != nil || len(cases) != 1 {
		t.Fatal("expected task diagnostic", err)
	}
	t.Logf("delivery_parent=%s diagnostic_mode=%s diagnostic_turn=%q", ds[0].Parent, cases[0].Mode, cases[0].TurnID)
	if cases[0].TurnID != parent {
		t.Fatal("task diagnostic loses its parent turn")
	}
}

func TestKnowledgeAlwaysTaskIsDiagnosed(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"kind": "rule", "scene": "\u3044\u3064\u3082"})
	snapshot := &hookSession{Scope: b.Scope, TurnID: "0000000000000172", Request: "parent synthetic request"}
	if err := saveSession(b.Root, "audit-always", snapshot); err != nil {
		t.Fatal(err)
	}
	out, _, err := hookSubagentStart(b.Root, &hookInput{SessionID: "audit-always", AgentID: "child-b", AgentType: "audit"}, snapshot)
	if err != nil || !strings.Contains(out.Context, p.ID) {
		t.Fatal("expected always delivery", err)
	}
	ds, err := (store.Store{Dir: b.Root}).MemoryDeliveries(b.Scope)
	if err != nil || len(ds) != 1 {
		t.Fatal("expected one actual delivery", err)
	}
	cases, err := b.DeliveryCases()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual_deliveries=%d diagnostic_cases=%d", len(ds), len(cases))
	if len(cases) != 1 {
		t.Fatal("always-rule task delivery is absent from diagnostics")
	}
}
