package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// The MCP rewrite reads the arguments as a map first. When that read fails (a number too large for float64 in a field
// the handler does not know), the arguments must not reach the handler unchanged with the person's source in them.
func TestMCPLabelSourceWhenTheMapReadFails(t *testing.T) {
	send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	d := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_decide", "arguments": map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}}}}))["structuredContent"].(map[string]any)
	raw := json.RawMessage(fmt.Sprintf(`{"trace_id":%q,"truth":"change_request","source":"ask","weight":1,"x":1e400}`, d["trace_id"]))
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_label", "arguments": raw}})
	st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
	labs, _ := st.LabeledTraces("intent")
	if l, ok := labs[d["trace_id"].(string)]; ok && l.Source != "mcp" {
		t.Fatalf("an MCP label got past the rewrite: source=%q weight=%v", l.Source, l.Weight)
	}
}

// arguments null decodes into a nil map without an error; the rewrite must not panic on it.
func TestMCPLabelNullArguments(t *testing.T) {
	send, _ := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("shikii_label with arguments null panicked: %v", r)
		}
	}()
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_label", "arguments": json.RawMessage("null")}})
}
