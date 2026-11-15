package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
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

// Every operation runNotebook handles is either in shikii_wiki's MCP enum or in mcpWikiCLIOnly, never both: an operation added to
// runNotebook later fails here until it is placed. A call of a CLI-only operation through MCP is refused with the way to do it, and
// the same operation still runs through HTTP (and so the CLI).
func TestMCPLeavesRarelyUsedWikiOperationsToTheCLI(t *testing.T) {
	handled := runNotebookOperations(t)
	enum := mcpWikiOperations(t)
	if problem := wikiSurfaceProblem(append(handled, "newly_added"), enum, mcpWikiCLIOnly); !strings.Contains(problem, "newly_added") {
		t.Fatalf("the check accepts an operation that is neither on MCP nor CLI-only: %q", problem)
	}
	if problem := wikiSurfaceProblem(handled, append(enum, "organize"), map[string]string{"organize": "x"}); !strings.Contains(problem, "organize") {
		t.Fatalf("the check accepts an operation that is both on MCP and CLI-only: %q", problem)
	}
	if problem := wikiSurfaceProblem(handled, enum, mcpWikiCLIOnly); problem != "" {
		t.Fatal(problem)
	}
	for _, op := range []string{"organize", "reconcile", "consult", "reuse", "diagnose"} {
		if mcpWikiCLIOnly[op] == "" {
			t.Errorf("%s is not left to the CLI", op)
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
	// HTTP keeps the operation: organize without its object fails with organize's own error, not the MCP refusal.
	h := newServer(serveOpts{storeDir: t.TempDir()})
	_, body, err := callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"organize"}`))
	if err != nil || !strings.Contains(string(body), "wiki organize needs an organization object") {
		t.Fatalf("HTTP no longer runs organize: %v %s", err, body)
	}
}

func wikiSurfaceProblem(handled, enum []string, cliOnly map[string]string) string {
	onMCP := map[string]bool{}
	for _, op := range enum {
		onMCP[op] = true
	}
	var problems []string
	for _, op := range handled {
		_, cli := cliOnly[op]
		switch {
		case onMCP[op] && cli:
			problems = append(problems, op+" is both on MCP and CLI-only")
		case !onMCP[op] && !cli:
			problems = append(problems, op+" is handled by runNotebook but neither on MCP nor CLI-only")
		}
	}
	sort.Strings(problems)
	return strings.Join(problems, "; ")
}

// runNotebookOperations reads the case labels of runNotebook's switch from notebook.go.
func runNotebookOperations(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "notebook.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runNotebook" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range clause.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					op, _ := strconv.Unquote(lit.Value)
					ops = append(ops, op)
				}
			}
			return true
		})
	}
	if len(ops) < 10 {
		t.Fatalf("read only %d operations from runNotebook: %v", len(ops), ops)
	}
	return ops
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
