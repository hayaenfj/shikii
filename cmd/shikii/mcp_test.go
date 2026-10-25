package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run the MCP exchange one round trip at a time (a buffer like io.Pipe instead of stdio).
func mcpSession(t *testing.T, pii string, probs map[string]float64, roots ...string) (send func(req map[string]any) map[string]any, specPath string) {
	t.Helper()
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath = writeSpec(t, specsDir, pii)
	model := fakeSystemOne(t, "m", probs)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	storeDir := filepath.Join(dir, ".shikii")
	if len(roots) > 0 {
		storeDir = roots[0]
	}
	h := newServer(serveOpts{specsDir: specsDir, storeDir: storeDir, ef: ef, autoCalibrate: true})
	var in bytes.Buffer
	var out bytes.Buffer
	send = func(req map[string]any) map[string]any {
		t.Helper()
		in.Reset()
		out.Reset()
		b, _ := json.Marshal(req)
		in.Write(append(b, '\n'))
		if err := runMCP(bufio.NewReader(&in), &out, h); err != nil {
			t.Fatalf("runMCP: %v", err)
		}
		line := strings.TrimSpace(out.String())
		if line == "" {
			return nil // notifications get no reply
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		return resp
	}
	return send, specPath
}

func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if e, ok := resp["error"]; ok {
		t.Fatalf("rpc error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	if r == nil {
		t.Fatalf("no result in %v", resp)
	}
	return r
}

func TestMCPInitializeAndToolList(t *testing.T) {
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}}}))
	if r["protocolVersion"] != "2025-03-26" || r["serverInfo"].(map[string]any)["name"] != "shikii" {
		t.Fatalf("initialize: %v", r)
	}
	if send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}) != nil {
		t.Fatal("notifications must not be answered")
	}
	r = result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}))
	names := map[string]bool{}
	for _, tl := range r["tools"].([]any) {
		m := tl.(map[string]any)
		names[m["name"].(string)] = true
		if m["inputSchema"] == nil || m["description"] == "" {
			t.Fatalf("tool must carry a schema and a description: %v", m)
		}
	}
	for _, want := range []string{"shikii_specs", "shikii_decide", "shikii_label", "shikii_status"} {
		if !names[want] {
			t.Fatalf("missing tool %s in %v", want, names)
		}
	}
}

// Go around decide → label → automatic calibration → decide is act, using only the tools.
func TestMCPDecideLabelLoop(t *testing.T) {
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	call := func(id int, name string, args map[string]any) map[string]any {
		t.Helper()
		r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
		if r["isError"] == true {
			t.Fatalf("%s: tool error: %v", name, r["content"])
		}
		sc, _ := r["structuredContent"].(map[string]any)
		if sc == nil {
			t.Fatalf("%s: no structuredContent in %v", name, r)
		}
		return sc
	}
	specs := call(1, "shikii_specs", map[string]any{})
	if len(specs["specs"].([]any)) != 1 {
		t.Fatalf("specs: %v", specs)
	}
	for i := 0; i < 2; i++ {
		d := call(10+i, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix " + string(rune('a'+i))}})
		if d["band"] != "ask" {
			t.Fatalf("uncalibrated decide must be ask, got %v", d["band"])
		}
		l := call(20+i, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request"})
		if i == 1 && l["calibrated"] != true {
			t.Fatalf("second label must trigger auto-calibration: %v", l)
		}
	}
	d := call(30, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix z"}})
	if d["band"] != "act" {
		t.Fatalf("after calibration the band must be act, got %v", d["band"])
	}
	st := call(40, "shikii_status", map[string]any{})
	if st["specs"].(([]any))[0].(map[string]any)["labels"].(float64) != 2 {
		t.Fatalf("status: %v", st)
	}
}

// Errors are returned as isError in the tool result, not as JSON-RPC errors (so the agent can read them and fix things).
func TestMCPToolErrorsAreReportedNotCrashed(t *testing.T) {
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_decide", "arguments": map[string]any{"spec": "nope", "inputs": map[string]any{}}}}))
	if r["isError"] != true || !strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), "unknown spec") {
		t.Fatalf("unknown spec must come back as a tool error: %v", r)
	}
	resp := send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "no_such_tool", "arguments": map[string]any{}}})
	if resp["error"] == nil {
		t.Fatalf("unknown tool must be a JSON-RPC error: %v", resp)
	}
	resp = send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "no/such/method"})
	if e, _ := resp["error"].(map[string]any); e == nil || e["code"].(float64) != -32601 {
		t.Fatalf("unknown method must be -32601: %v", resp)
	}
}

// 0.9.0: so agents that use only MCP can also give pages a kind and scene, the input schemas of shikii_wiki and shikii_reflect (its wiki items)
// have kind (an enum of 5) and scene (shikii_reflect's wiki items allow no extra fields, so without them it could not be sent as defined).
func TestMCPWikiToolsCarryKindAndScene(t *testing.T) {
	enc, err := json.Marshal(mcpTools)
	if err != nil {
		t.Fatal(err)
	}
	var tools []map[string]any
	json.Unmarshal(enc, &tools)
	props := map[string]map[string]any{}
	for _, tl := range tools {
		schema := tl["inputSchema"].(map[string]any)["properties"].(map[string]any)
		switch tl["name"] {
		case "shikii_wiki":
			props["wiki"] = schema
		case "shikii_reflect":
			props["reflect"] = schema["wiki"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		}
	}
	for name, p := range props {
		kind, _ := p["kind"].(map[string]any)
		enum, _ := kind["enum"].([]any)
		if len(enum) != 5 || enum[0] != "rule" || p["scene"] == nil || !strings.Contains(kind["description"].(string), "a rule decided in this project") {
			t.Fatalf("%s's input schema needs kind (5, with descriptions) and scene: %v", name, p)
		}
	}
	if len(props) != 2 {
		t.Fatalf("both shikii_wiki and shikii_reflect: %v", props)
	}
	// Agents without a hook (MCP only in Codex or OpenCode) also learn that saving needs a kind and scene, and how to give them to pages from earlier versions
	for _, tl := range tools {
		if tl["name"] == "shikii_wiki" {
			d := tl["description"].(string)
			if !strings.Contains(d, "kind") || !strings.Contains(d, "scene") || !strings.Contains(d, "classify") || !strings.Contains(d, "unclassified") {
				t.Fatalf("shikii_wiki's description covers kind and scene, and how to classify pages from earlier versions: %s", d)
			}
		}
	}
	if !strings.Contains(mcpInstructions, "kind") || !strings.Contains(mcpInstructions, "scene") || !strings.Contains(mcpInstructions, "classify") {
		t.Fatalf("the server's guidance covers kind, scene and classify: %s", mcpInstructions)
	}
}
