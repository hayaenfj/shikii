package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// writeBodyErr through the MCP tools that reach the entries it now answers for (shikii_label, shikii_tune, shikii_decide), and
// through HTTP at every entry that calls it: a body over the bound is answered with the bound, never in encoding/json's words or as
// "bad json"; a body that is no object keeps the reader's words with 400.
func TestAuditIBodyOverTheBoundThroughTheTools(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: feedbackScope(repo)})
	big := strings.Repeat("x", maxBody+10)
	for _, tool := range []string{"shikii_label", "shikii_tune", "shikii_decide"} {
		args, _ := json.Marshal(map[string]any{"spec": "needs-plan", "trace_id": "t", "truth": "yes", "note": big})
		params, _ := json.Marshal(map[string]any{"name": tool, "arguments": json.RawMessage(args)})
		result, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)["result"].(map[string]any)
		raw, _ := json.Marshal(result)
		t.Logf("%s with %d bytes of arguments: %.200s", tool, len(args), raw)
		if result["isError"] != true || !strings.Contains(string(raw), "body larger than 1048576 bytes") || strings.Contains(string(raw), "bad json") {
			t.Errorf("%s with arguments over the bound: %.300s", tool, raw)
		}
	}
	for _, path := range []string{"/v1/decide", "/v1/label", "/v1/tune/sync", "/v1/tune/run", "/v1/tune/variant", "/v1/tune/adopt", "/v1/tune/review", "/v1/redo", "/v1/answers/restore", "/v1/wiki/claim"} {
		for body, want := range map[string]struct {
			code int
			text string
		}{
			`{"note":"` + big + `"}`: {413, "body larger than 1048576 bytes"},
			`{"spec":`:               {400, "bad json: "},
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != want.code || !strings.Contains(rec.Body.String(), want.text) {
				t.Errorf("POST %s with %d bytes: %d %.200s, want %d with %q", path, len(body), rec.Code, rec.Body.String(), want.code, want.text)
			}
		}
	}
}
