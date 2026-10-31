package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
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

func TestKnowledgeAlwaysComparisonKeepsHistory(t *testing.T) {
	b, src := loopBook(t)
	p := loopPage(t, b, src, map[string]any{"kind": "history", "scene": "always"})
	snapshot := &hookSession{Scope: b.Scope, TurnID: "0000000000000183", Request: "parent synthetic request"}
	if err := saveSession(b.Root, "audit-history", snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err := hookSubagentStart(b.Root, &hookInput{SessionID: "audit-history", AgentID: "child-c", AgentType: "audit"}, snapshot); err != nil {
		t.Fatal(err)
	}
	cases, err := b.DeliveryCases()
	if err != nil || len(cases) != 1 {
		t.Fatal(cases, err)
	}
	patch := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{src}, ChangeReason: "synthetic history", Edits: []notebook.TextEdit{{Field: "body", Before: "", After: "Synthetic background."}}}
	result, err := compareKnowledge(b, patch, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	var got struct {
		Cases []comparisonCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Cases) != 1 {
		t.Fatal(string(raw))
	}
	t.Logf("recorded_retrieval=%s comparison_before=%s observed_reason=%s", cases[0].Entries[0].Retrieval, got.Cases[0].Before, got.Cases[0].ObservedReason)
	if got.Cases[0].Before != cases[0].Entries[0].Retrieval {
		t.Fatal("always comparison changes the historical baseline classification")
	}
}
