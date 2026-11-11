package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Found while fixing the second round of the audit of 0.31.0: the entries the MCP tools shikii_decide, shikii_label and shikii_tune
// reach read their JSON on their own, and answered a value of the wrong type in encoding/json's words ("bad json: json: cannot
// unmarshal string into Go struct field .inputs of type map[string]string"). They keep passing over keys they do not know, and
// say what they cannot read in the reader's words, like the entries of shikii_wiki, shikii_review and shikii_reflect.

// toolWrongTypes is every MCP tool with arguments that hold one value of the wrong type, and the reader's sentence for it. A tool
// that sends no body has no arguments here.
var toolWrongTypes = map[string]struct{ arguments, want string }{
	"shikii_specs":   {},
	"shikii_status":  {},
	"shikii_decide":  {`{"spec":"needs-plan","inputs":"the request as text"}`, "pass the JSON inputs as an object"},
	"shikii_label":   {`{"trace_id":"0000000000000001","truth":"yes","weight":"heavy"}`, "pass the JSON weight as a number (got string)"},
	"shikii_tune":    {`{"spec":"needs-plan","propose":"three"}`, "pass the JSON propose as an integer (got string)"},
	"shikii_wiki":    {`{"operation":"prepare","work":"the ids as text"}`, "pass the JSON work as an object"},
	"shikii_review":  {`{"operation":"submit","trace_id":7}`, "pass the JSON trace_id as a string (got number)"},
	"shikii_reflect": {`{"operation":"submit","reviews":"one"}`, "pass the JSON reviews as an array"},
}

// toolsWithoutARow names the tools that have no row in the table, and the rows that name no tool.
func toolsWithoutARow(tools []map[string]any) (uncovered, idle []string) {
	named := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		named[name] = true
		if _, covered := toolWrongTypes[name]; !covered {
			uncovered = append(uncovered, name)
		}
	}
	for name := range toolWrongTypes {
		if !named[name] {
			idle = append(idle, name)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(idle)
	return uncovered, idle
}

// encodingJSONWords are words only encoding/json says ("bad json: " is these entries' own opening, so "json: " alone is not one).
var encodingJSONWords = []string{"json: cannot", "json: unknown", "json: invalid", "Go struct field", "Go value", "cannot unmarshal", "invalid character", "unexpected EOF", ": EOF"}

func TestEveryToolAnswersWhatItCannotReadInTheReadersWords(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: feedbackScope(repo)})
	// a value of the wrong type, through each tool
	for name, c := range toolWrongTypes {
		if c.arguments == "" {
			continue
		}
		params, _ := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(c.arguments)})
		result, _ := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)["result"].(map[string]any)
		raw, _ := json.Marshal(result)
		if result["isError"] != true || !strings.Contains(string(raw), c.want) {
			t.Errorf("%s with %s is refused in the reader's words (%q): %s", name, c.arguments, c.want, raw)
		}
		for _, word := range encodingJSONWords {
			if strings.Contains(string(raw), word) {
				t.Errorf("%s with %s is answered in encoding/json's words (%q): %s", name, c.arguments, word, raw)
			}
		}
	}
	// the table is every tool
	if uncovered, idle := toolsWithoutARow(mcpTools); len(uncovered) > 0 || len(idle) > 0 {
		t.Errorf("tools without a row in toolWrongTypes: %v; rows that name no tool: %v", uncovered, idle)
	}
	// the negative example: a tool nobody listed is found
	if uncovered, _ := toolsWithoutARow(append([]map[string]any{{"name": "shikii_new"}}, mcpTools...)); len(uncovered) != 1 || uncovered[0] != "shikii_new" {
		t.Errorf("a new tool without a row is not reported: %v", uncovered)
	}
	// a body that is no JSON object, at the entries of the decision tools (an MCP client always sends an object; a caller of the
	// HTTP endpoint may not)
	for _, path := range []string{"/v1/decide", "/v1/label", "/v1/tune/sync"} {
		for body, want := range map[string]string{
			`{"spec":"needs-plan",}`: "the JSON is malformed (at byte 22)",
			`{"spec":"needs-plan"`:   "the JSON is malformed (at byte 20: the input ends there, before the JSON is closed; send the whole object)",
			"":                       "(got nothing)",
			"null":                   "(got null)",
			`["needs-plan"]`:         "(got array)",
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			var reply struct {
				Error string `json:"error"`
			}
			if rec.Code != http.StatusBadRequest || json.Unmarshal(rec.Body.Bytes(), &reply) != nil || !strings.Contains(reply.Error, want) {
				t.Errorf("POST %s with %q answers %d %s, want 400 with %q", path, body, rec.Code, rec.Body.String(), want)
			}
			for _, word := range encodingJSONWords {
				if strings.Contains(reply.Error, word) {
					t.Errorf("POST %s with %q is answered in encoding/json's words (%q): %s", path, body, word, reply.Error)
				}
			}
		}
	}
	// these entries still pass over a key they do not know, and read the JSON behind a byte order mark: the reply is about the
	// decision, not about the key or the mark
	for _, body := range []string{`{"spec":"no-such-decision","inputs":{},"not_a_field":1}`, string([]byte{0xef, 0xbb, 0xbf}) + `{"spec":"no-such-decision","inputs":{}}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/decide", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "not_a_field") || strings.Contains(rec.Body.String(), "malformed") || !strings.Contains(rec.Body.String(), "no-such-decision") {
			t.Errorf("POST /v1/decide with %q answers about the decision: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}
