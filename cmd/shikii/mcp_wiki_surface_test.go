package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Measured on 2026-11-15 from the Claude Code and Codex logs on the author's machine: tools/list returned 48,401 bytes, 34,415 of
// them shikii_wiki's definition, and Codex and OpenCode take every definition at the start of each session. Of the wiki's
// operations, organize, reconcile, consult and reuse were never called, and diagnose only in sandboxes; consult and reuse are what
// prepare does with work.proposal or work.conditions on its receipt. So the MCP definition leaves those five to the CLI and HTTP,
// and a nested copy of a field's description points to the top-level one. The budget fails a definition that grows back.
const mcpWikiDefinitionMax = 27_000

func TestMCPWikiDefinitionStaysWithinItsBudget(t *testing.T) {
	size := mcpToolDefinitionSize(t, "shikii_wiki")
	if size > mcpWikiDefinitionMax {
		t.Fatalf("shikii_wiki's MCP definition is %d bytes, over %d: every Codex and OpenCode session takes it whole", size, mcpWikiDefinitionMax)
	}
	// A nested copy that says "As the top-level X." keeps its meaning only while X is a top-level field with its own description.
	broken := map[string]any{"properties": map[string]any{"learning": map[string]any{"description": "full"},
		"patch": map[string]any{"properties": map[string]any{"passages": map[string]any{"description": "As the top-level passages."}}}}}
	if problem := topLevelPointerProblem(broken); !strings.Contains(problem, "passages") {
		t.Fatalf("the check accepts a pointer to a field that is not at the top level: %q", problem)
	}
	var schema map[string]any
	for _, tool := range mcpTools {
		if tool["name"] == "shikii_wiki" {
			schema = tool["inputSchema"].(map[string]any)
		}
	}
	if problem := topLevelPointerProblem(schema); problem != "" {
		t.Fatal(problem)
	}
	if pointers := strings.Count(mustJSON(t, schema), "As the top-level "); pointers < 8 {
		t.Fatalf("only %d nested copies point to the top level; the walk is looking in the wrong place", pointers)
	}
}

func topLevelPointerProblem(schema map[string]any) string {
	top, _ := schema["properties"].(map[string]any)
	var problems []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if d, ok := x["description"].(string); ok && strings.HasPrefix(d, "As the top-level ") {
				field := strings.TrimSuffix(strings.Fields(strings.TrimPrefix(d, "As the top-level "))[0], ".")
				own, _ := top[field].(map[string]any)
				if full, _ := own["description"].(string); full == "" || strings.HasPrefix(full, "As the top-level ") {
					problems = append(problems, "a nested copy points to the top-level "+field+", which has no description of its own")
				}
			}
			for _, y := range x {
				walk(y)
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	walk(schema)
	sort.Strings(problems)
	return strings.Join(problems, "; ")
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mcpToolDefinitionSize(t *testing.T, name string) int {
	t.Helper()
	for _, tool := range mcpTools {
		if tool["name"] != name {
			continue
		}
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false) // the size a client reads, without < for < and >
		if err := enc.Encode(tool); err != nil {
			t.Fatal(err)
		}
		return len(bytes.TrimSpace(b.Bytes()))
	}
	t.Fatalf("no tool %s", name)
	return 0
}

// The five operations MCP leaves to the CLI: a call of one through MCP is refused with the way to do it, and the same operation still
// runs through HTTP (and so the CLI). That MCP runs nothing outside its definition is TestMCPRunsOnlyTheWikiOperationsItsDefinitionLists.
func TestMCPLeavesRarelyUsedWikiOperationsToTheCLI(t *testing.T) {
	enum := mcpWikiOperations(t)
	for _, op := range []string{"organize", "reconcile", "consult", "reuse", "diagnose"} {
		if mcpWikiCLIOnly[op] == "" {
			t.Errorf("%s is not left to the CLI", op)
		}
		if slices.Contains(enum, op) {
			t.Errorf("%s is both in the MCP definition and left to the CLI", op)
		}
	}

	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}))
	want := map[string][]string{
		"consult":   {`"proposal"`, "receipt", "prepare"},
		"reuse":     {`"conditions"`, "receipt", "prepare"},
		"organize":  {"shikii wiki organize", `"organization"`, "standard input"},
		"reconcile": {"shikii wiki reconcile", `"patch"`, "standard input"},
		"diagnose":  {"shikii wiki diagnose", `"id"`, "standard input"},
	}
	for op, words := range want {
		got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": map[string]any{"operation": op}}}))
		text := got["content"].([]any)[0].(map[string]any)["text"].(string)
		var refusal struct {
			Error string `json:"error"`
		}
		if got["isError"] != true || json.Unmarshal([]byte(text), &refusal) != nil {
			t.Errorf("MCP runs %s: %s", op, text)
			continue
		}
		for _, w := range words {
			if !strings.Contains(refusal.Error, w) {
				t.Errorf("the refusal of %s does not say %s: %s", op, w, refusal.Error)
			}
		}
	}
	// HTTP keeps each of them: it answers with the operation's own reply or error, not with the line for an unknown operation.
	h := newServer(serveOpts{storeDir: t.TempDir()})
	for op := range mcpWikiCLIOnly {
		_, body, err := callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"`+op+`"}`))
		if err != nil || strings.Contains(string(body), "usage: wiki") {
			t.Errorf("HTTP no longer runs %s: %v %s", op, err, body)
		}
	}
}

// MCP runs only the wiki operations shikii_wiki's definition lists (its enum), and refuses every other one before a handler reads it.
// Before, the server refused only the names in mcpWikiCLIOnly and passed anything else on: an operation runNotebook handled under a
// constant ran over MCP, while the test that read runNotebook's string cases passed (audit of 0.33.0, U-3). The handler below runs
// every name it is sent, as runNotebook would with such a case: the listed operations reach it and nothing else does. The refusal of
// an operation that is not CLI-only names the operations MCP takes and none it refuses (U-2: it listed consult, organize and
// diagnose as if MCP took them).
func TestMCPRunsOnlyTheWikiOperationsItsDefinitionLists(t *testing.T) {
	var ran []string
	everything := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Operation string `json:"operation"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ran = append(ran, body.Operation)
		writeJSON(w, http.StatusOK, map[string]any{"ran": body.Operation})
	})
	call := func(h http.Handler, arguments string) (refused bool, text string) {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(arguments)})
		reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)
		result, _ := reply["result"].(map[string]any)
		if result == nil {
			t.Fatalf("%s: no tool result: %v", arguments, reply)
		}
		var body struct {
			Error string `json:"error"`
		}
		text = result["content"].([]map[string]any)[0]["text"].(string)
		if json.Unmarshal([]byte(text), &body) == nil && body.Error != "" {
			text = body.Error
		}
		return result["isError"] == true, text
	}

	enum := mcpWikiOperations(t)
	for _, op := range enum {
		ran = nil
		if refused, text := call(everything, `{"operation":"`+op+`"}`); refused || !slices.Equal(ran, []string{op}) {
			t.Errorf("MCP does not run %s, which its definition lists: ran %v, %s", op, ran, text)
		}
	}
	outside := []string{`{"operation":"zz_hidden"}`, `{"operation":""}`, `{}`, `{"operation":5}`, `{"operation":"Query"}`, `{"operation":"query "}`,
		`{"OPERATION":"organize"}`, `{"operation":"query","operation":"organize"}`}
	for op := range mcpWikiCLIOnly {
		outside = append(outside, `{"operation":"`+op+`"}`)
	}
	for _, arguments := range outside {
		ran = nil
		if refused, text := call(everything, arguments); !refused || len(ran) > 0 {
			t.Errorf("MCP ran %s, which its definition does not list: ran %v, %s", arguments, ran, text)
		}
	}

	refusedWord := func(text, op string) bool { return regexp.MustCompile(`\b` + op + `\b`).MatchString(text) }
	for _, h := range []http.Handler{everything, newServer(serveOpts{storeDir: t.TempDir()})} {
		refused, text := call(h, `{"operation":"nosuch"}`)
		if !refused || !strings.Contains(text, `"nosuch"`) {
			t.Errorf("an unknown operation is refused by name: %s", text)
		}
		for _, op := range enum {
			if !strings.Contains(text, op) {
				t.Errorf("the refusal of an unknown operation does not name %s, which MCP takes: %s", op, text)
			}
		}
		for op := range mcpWikiCLIOnly {
			if refusedWord(text, op) {
				t.Errorf("the refusal of an unknown operation names %s, which MCP refuses: %s", op, text)
			}
		}
	}
}

func mcpWikiOperations(t *testing.T) []string {
	t.Helper()
	for _, tool := range mcpTools {
		if tool["name"] == "shikii_wiki" {
			return tool["inputSchema"].(map[string]any)["properties"].(map[string]any)["operation"].(map[string]any)["enum"].([]string)
		}
	}
	t.Fatal("no shikii_wiki")
	return nil
}
