package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMCPProcess runs cmdMCP as Codex does: stdin and stdout are the process's own, and it starts in the current folder.
// It returns the answers by id and cmdMCP's error.
func runMCPProcess(t *testing.T, args []string, reqs ...map[string]any) (map[float64]map[string]any, error) {
	t.Helper()
	dir := t.TempDir()
	in, err := os.Create(filepath.Join(dir, "in"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reqs {
		r["jsonrpc"] = "2.0"
		b, _ := json.Marshal(r)
		in.Write(append(b, '\n'))
	}
	in.Seek(0, 0)
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	runErr := cmdMCP(args)
	os.Stdin, os.Stdout = oldIn, oldOut
	in.Close()
	out.Close()
	f, _ := os.Open(filepath.Join(dir, "out"))
	defer f.Close()
	answers := map[float64]map[string]any{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		id, _ := m["id"].(float64)
		answers[id] = m
	}
	return answers, runErr
}

func mcpInit(id int) map[string]any {
	return map[string]any{"id": id, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}}}
}

func mcpCall(id int, name string, args map[string]any) map[string]any {
	return map[string]any{"id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}
}

// toolText returns the tool result's text and whether it is an error. A JSON-RPC error fails the test (tool errors must be readable by the agent).
func toolText(t *testing.T, answers map[float64]map[string]any, id int) (string, bool) {
	t.Helper()
	a := answers[float64(id)]
	if a == nil {
		t.Fatalf("no answer for id %d: %v", id, answers)
	}
	if e, ok := a["error"]; ok {
		t.Fatalf("id %d: a JSON-RPC error instead of a tool result: %v", id, e)
	}
	r := a["result"].(map[string]any)
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	return text, r["isError"] == true
}

// 0.9.3: Codex starts shikii mcp in the folder its daemon started in, often a folder without shikii. The server must still answer the
// handshake (before, it exited with "no spec (*.yaml) in .shikii/decisions" and Codex showed a handshake error), and a tool call that needs a project
// returns a tool error that says to pass project.
func TestMCPStartsWithoutAProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	answers, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:1"},
		mcpInit(1),
		map[string]any{"id": 2, "method": "tools/list"},
		mcpCall(3, "shikii_specs", map[string]any{}),
		mcpCall(4, "shikii_wiki", map[string]any{"operation": "query", "query": "x"}),
	)
	if err != nil {
		t.Fatalf("shikii mcp must not exit before the handshake in a folder without shikii: %v", err)
	}
	if r, _ := answers[1]["result"].(map[string]any); r == nil || r["serverInfo"] == nil {
		t.Fatalf("initialize must be answered: %v", answers)
	}
	if r, _ := answers[2]["result"].(map[string]any); r == nil || len(r["tools"].([]any)) == 0 {
		t.Fatalf("tools/list must be answered: %v", answers)
	}
	for _, id := range []int{3, 4} {
		text, isErr := toolText(t, answers, id)
		if !isErr || !strings.Contains(text, "project") || !strings.Contains(text, "absolute path") {
			t.Fatalf("id %d: without a project the tool error says to pass project: %v %s", id, isErr, text)
		}
	}
}

// Every tool takes project, so a server shared by every Codex session (one registration in ~/.codex/config.toml) uses the caller's project.
func TestMCPToolsListProject(t *testing.T) {
	for _, tl := range mcpTools {
		props := tl["inputSchema"].(map[string]any)["properties"].(map[string]any)
		p, _ := props["project"].(map[string]any)
		if p == nil || p["type"] != "string" || !strings.Contains(p["description"].(string), "absolute path") {
			t.Fatalf("%s: inputSchema must have project with a description: %v", tl["name"], props["project"])
		}
	}
	if !strings.Contains(mcpInstructions, "project") || !strings.Contains(mcpInstructions, "working folder") {
		t.Fatal("mcpInstructions must tell agents to pass their working folder as project")
	}
}

// Two registered projects A and B, one server started in a third folder: each call writes to the store of the project it names, and only registered
// shikii projects are accepted.
func TestMCPProjectArgumentPicksTheStore(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	a := gitRepo(t)
	hookEnv(t, model, a)
	b := gitRepo(t)
	initTestProject(t, b)
	// a git repository with a .shikii that init never registered (copied specs only)
	unregistered := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(unregistered, ".shikii", "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec, _ := os.ReadFile(filepath.Join(a, ".shikii", "decisions", "needs-plan.yaml"))
	os.WriteFile(filepath.Join(unregistered, ".shikii", "decisions", "needs-plan.yaml"), spec, 0o644)
	outside := t.TempDir()
	// a registered project whose handoff/ became a link to a shared location: refused by the store checks
	linked := gitRepo(t)
	initTestProject(t, linked)
	shared := t.TempDir()
	os.RemoveAll(filepath.Join(linked, ".shikii", "handoff"))
	if err := os.Symlink(shared, filepath.Join(linked, ".shikii", "handoff")); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(a, "src", "deep")
	os.MkdirAll(sub, 0o755)
	for _, project := range []string{a, b} {
		if _, err := os.Stat(filepath.Join(project, ".shikii", "records", "needs-plan.jsonl")); err == nil {
			t.Fatalf("%s: no record before the calls", project)
		}
	}
	t.Chdir(t.TempDir()) // the third folder, like Codex's daemon

	write := func(id int, project, content string) map[string]any {
		return mcpCall(id, "shikii_wiki", map[string]any{"operation": "handoff_write", "path": "where.md", "content": content, "project": project})
	}
	decide := func(id int, project string) map[string]any {
		return mcpCall(id, "shikii_decide", map[string]any{"spec": "needs-plan", "inputs": map[string]string{"request": "rename a variable"}, "project": project})
	}
	answers, err := runMCPProcess(t, []string{"--local", model.URL},
		mcpInit(1),
		write(2, a, "for A"),
		write(3, b, "for B"),
		decide(4, sub),
		decide(5, b),
		write(6, unregistered, "no"),
		write(7, outside, "no"),
		write(8, "relative/path", "no"),
		mcpCall(9, "shikii_specs", map[string]any{}),
		mcpCall(10, "shikii_specs", map[string]any{"project": b}),
		write(11, linked, "no"),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 3, 4, 5, 10} {
		if text, isErr := toolText(t, answers, id); isErr {
			t.Fatalf("id %d: a registered project must be accepted: %s", id, text)
		}
	}
	for project, want := range map[string]string{a: "for A", b: "for B"} {
		got, err := os.ReadFile(filepath.Join(project, ".shikii", "handoff", "where.md"))
		if err != nil || string(got) != want {
			t.Fatalf("%s: the call with this project writes to its own store: %q %v", project, got, err)
		}
	}
	for _, project := range []string{a, b} {
		recs, _ := os.ReadFile(filepath.Join(project, ".shikii", "records", "needs-plan.jsonl"))
		if strings.Count(string(recs), "\"trace_id\"") != 1 {
			t.Fatalf("%s: decide with this project must record in its own store", project)
		}
	}
	for _, p := range []string{filepath.Join(unregistered, ".shikii", "handoff"), filepath.Join(outside, ".shikii", "handoff"), shared} {
		if _, err := os.Stat(filepath.Join(p, "where.md")); err == nil {
			t.Fatalf("%s: a refused project must get nothing written", p)
		}
	}
	for id, must := range map[int]string{6: "registered", 7: "registered", 8: "absolute path", 9: "project", 11: "project"} {
		text, isErr := toolText(t, answers, id)
		if !isErr || !strings.Contains(text, must) {
			t.Fatalf("id %d: must be refused with a tool error naming %q: %v %s", id, must, isErr, text)
		}
	}
}

// A server started with a store (Claude Code and OpenCode: --store .shikii in the project) keeps using it when project is omitted.
func TestMCPWithoutProjectUsesTheStartStore(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	a := gitRepo(t)
	hookEnv(t, model, a)
	t.Chdir(a)
	answers, err := runMCPProcess(t, []string{"--store", ".shikii", "--local", model.URL},
		mcpInit(1),
		mcpCall(2, "shikii_wiki", map[string]any{"operation": "handoff_write", "path": "start.md", "content": "start"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := toolText(t, answers, 2); isErr {
		t.Fatal(text)
	}
	if got, _ := os.ReadFile(filepath.Join(a, ".shikii", "handoff", "start.md")); string(got) != "start" {
		t.Fatalf("without project the start store is used: %q", got)
	}
}

// 0.9.3 audit 2, low 1: a server started without --store (Codex's shared registration, started in its daemon's folder) does not fall back to
// that folder's project when a call forgets project: the call is refused, so nothing lands in another project. With project it works.
func TestMCPWithoutStoreFlagRequiresProject(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	a := gitRepo(t)
	hookEnv(t, model, a)
	t.Chdir(a) // the daemon happens to have started in a registered project
	answers, err := runMCPProcess(t, []string{"--local", model.URL},
		mcpInit(1),
		mcpCall(2, "shikii_wiki", map[string]any{"operation": "handoff_write", "path": "forgot.md", "content": "x"}),
		mcpCall(3, "shikii_wiki", map[string]any{"operation": "handoff_write", "path": "named.md", "content": "y", "project": a}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := toolText(t, answers, 2); !isErr || !strings.Contains(text, "project") {
		t.Fatalf("a call without project is refused when the server was started without --store: %q", text)
	}
	if _, err := os.Stat(filepath.Join(a, ".shikii", "handoff", "forgot.md")); !os.IsNotExist(err) {
		t.Fatal("nothing is written into the daemon folder's project")
	}
	if text, isErr := toolText(t, answers, 3); isErr {
		t.Fatalf("with project the call works: %s", text)
	}
}
