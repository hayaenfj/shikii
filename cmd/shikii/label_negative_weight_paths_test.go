package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// A negative weight is a mistake on every path that saves a truth: the page's /v1/label answers 400, the MCP shikii_label answers
// a tool error, the CLI label returns an error. None of them saves a label (before, each saved a firm truth of weight 1).
func TestNegativeLabelWeightIsRefusedOverHTTPMCPAndCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	url, _, storeDir := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	trace := d["trace_id"].(string)
	saved := func() int { return len(readLabels(t, storeDir, "intent")) }

	code, body := postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": trace, "truth": "question", "weight": -0.5})
	t.Logf("HTTP /v1/label weight -0.5: %d %v", code, body)
	if code != 400 || !strings.Contains(body["error"].(string), "weight must be in (0, 1]") {
		t.Errorf("HTTP: a negative weight is a 400 that names the weight: %d %v", code, body)
	}
	if n := saved(); n != 0 {
		t.Errorf("HTTP: nothing is saved, got %d labels: %+v", n, readLabels(t, storeDir, "intent"))
	}

	params, _ := json.Marshal(map[string]any{"name": "shikii_label", "arguments": map[string]any{"spec": "intent", "trace_id": trace, "truth": "question", "weight": -0.5}})
	answer := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, newServer(serveOpts{specsDir: specsDirOf(storeDir), storeDir: storeDir}))
	result, _ := answer["result"].(map[string]any)
	text := ""
	if content, ok := result["content"].([]map[string]any); ok && len(content) == 1 {
		text, _ = content[0]["text"].(string)
	}
	t.Logf("MCP shikii_label weight -0.5: isError=%v %s", result["isError"], text)
	if result["isError"] != true || !strings.Contains(text, "weight must be in (0, 1]") {
		t.Errorf("MCP: a negative weight is a tool error that names the weight: %v", answer)
	}
	if n := saved(); n != 0 {
		t.Errorf("MCP: nothing is saved, got %d labels: %+v", n, readLabels(t, storeDir, "intent"))
	}

	var cliErr error
	out := capture(t, func() error {
		cliErr = cmdLabel([]string{"--store", storeDir, "--weight", "-0.5", trace, "question"})
		return nil
	})
	t.Logf("CLI label --weight -0.5: err=%v out=%q", cliErr, out)
	if cliErr == nil || !strings.Contains(cliErr.Error(), "weight must be in (0, 1]") {
		t.Errorf("CLI: a negative weight is an error that names the weight: err=%v out=%q", cliErr, out)
	}
	if n := saved(); n != 0 {
		t.Errorf("CLI: nothing is saved, got %d labels: %+v", n, readLabels(t, storeDir, "intent"))
	}

	// the same request with a weight inside (0, 1] is saved with that weight: the refusals above are about the weight alone
	code, body = postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": trace, "truth": "question", "weight": 0.5})
	if labels := readLabels(t, storeDir, "intent"); code != 200 || len(labels) != 1 || labels[0].Weight != 0.5 {
		t.Errorf("a weight of 0.5 is saved as 0.5: %d %v %+v", code, body, labels)
	}
}
