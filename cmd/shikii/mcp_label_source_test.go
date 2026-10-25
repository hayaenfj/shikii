package main

import (
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// Behaviour check 0.9.2 (OpenCode, glm-5.3): the agent called shikii_label on its own needs-plan trace with source "ask",
// taking "Don't ask me questions" as the person's answer. The label was stored as a person's answer (source ask, weight 1).
// A label that comes through MCP is the agent's, whatever source it names.
func TestMCPLabelCannotClaimThePersonsAnswer(t *testing.T) {
	send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	call := func(id int, name string, args map[string]any) map[string]any {
		return result(t, send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
	}
	d := call(1, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}})["structuredContent"].(map[string]any)
	call(2, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request", "source": "ask", "weight": 1})
	st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
	labs, _ := st.LabeledTraces("intent")
	if l, ok := labs[d["trace_id"].(string)]; ok && l.Source == "ask" {
		t.Fatalf("an MCP label was stored as the person's answer: source=%q weight=%v", l.Source, l.Weight)
	}
}

// Go's JSON decoder matches field names without case and with Unicode folding, so "Source" or "ſource" (U+017F) is read as source too.
// None of them may carry the person's source past the MCP rewrite.
func TestMCPLabelSourceInAnySpelling(t *testing.T) {
	for _, key := range []string{"ſource", "Source", "SOURCE"} {
		send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
		call := func(id int, name string, args map[string]any) map[string]any {
			return result(t, send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
		}
		d := call(1, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}})["structuredContent"].(map[string]any)
		call(2, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request", key: "ask", "weight": 1})
		st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
		labs, _ := st.LabeledTraces("intent")
		if l, ok := labs[d["trace_id"].(string)]; !ok || l.Source != "mcp" {
			t.Errorf("key %q: the MCP label was stored as %+v (want source mcp)", key, l)
		}
	}
}
