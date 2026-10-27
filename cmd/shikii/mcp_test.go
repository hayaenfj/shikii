package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
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

// Second 0.9.3 audit (test holes): a working folder below a registered project outside git is found by walking up; project is taken out
// whatever its case, only once, and only as a string.
func TestAudit2MCPProjectWalkUpAndArgumentKeys(t *testing.T) {
	plain := t.TempDir() // not a git repository
	hookEnv(t, nil, plain)
	if top := projectScopeAt(plain); top != feedbackScope(plain) {
		t.Skipf("the temporary folder is inside git (%s)", top)
	}
	sub := filepath.Join(plain, "src", "deep")
	os.MkdirAll(sub, 0o755)
	t.Chdir(t.TempDir())
	write := func(id int, args map[string]any) map[string]any {
		args["operation"], args["path"] = "handoff_write", "w.md"
		return mcpCall(id, "shikii_wiki", args)
	}
	answers, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:1"},
		mcpInit(1),
		write(2, map[string]any{"project": sub, "content": "walked up"}),
		write(3, map[string]any{"PROJECT": plain, "content": "any case"}),
		write(4, map[string]any{"project": plain, "Project": plain, "content": "twice"}),
		write(5, map[string]any{"project": 1, "content": "number"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 3} {
		if text, isErr := toolText(t, answers, id); isErr {
			t.Fatalf("id %d: %s", id, text)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(plain, ".shikii", "handoff", "w.md")); string(got) != "any case" {
		t.Fatalf("both calls write to the registered project above: %q", got)
	}
	for id, must := range map[int]string{4: "only once", 5: "must be a string"} {
		if text, isErr := toolText(t, answers, id); !isErr || !strings.Contains(text, must) {
			t.Fatalf("id %d: %v %s", id, isErr, text)
		}
	}
}

// 0.9.6 audit, low 1: cmdMCP points hookDecide at its --local (useForHookDecisions); the MCP test helper must put it back, or a later test
// that reads hookLocalURL sees a closed test server's URL.
func TestMCPTestHelperRestoresTheHookURLs(t *testing.T) {
	before := hookLocalURL
	t.Run("mcp", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Chdir(t.TempDir())
		if _, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:47393"}, mcpInit(1)); err != nil {
			t.Fatal(err)
		}
	})
	if hookLocalURL != before {
		t.Fatalf("hookLocalURL is restored after an MCP test: %s (was %s)", hookLocalURL, before)
	}
}

// Audit S6: labels through MCP show their source as "mcp". weight does not exceed 1.
func TestMCPLabelsAreMarkedAndWeightIsBounded(t *testing.T) {
	send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	call := func(id int, name string, args map[string]any) map[string]any {
		return result(t, send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
	}
	d := call(1, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}})["structuredContent"].(map[string]any)
	l := call(2, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request"})
	if l["isError"] == true || l["structuredContent"].(map[string]any)["source"] != "mcp" {
		t.Fatalf("label from MCP must carry source=mcp: %v", l)
	}
	heavy := call(3, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request", "weight": 5})
	if heavy["isError"] != true {
		t.Fatalf("weight > 1 must be rejected: %v", heavy)
	}
	st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
	labs, _ := st.LabeledTraces("intent")
	if len(labs) != 1 || labs[d["trace_id"].(string)].Source != "mcp" {
		t.Fatalf("stored label: %+v", labs)
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

// A label through MCP is the agent's: whatever source it names, in any spelling the handler would read as source, and even with an extra field
// whose number cannot be read into a map, it is saved with source "mcp" and never as the person's answer.
func TestMCPLabelCannotClaimThePersonsAnswer(t *testing.T) {
	for _, tc := range []struct{ name, key, extra string }{
		{"source", "source", ""},
		{"long s", "ſource", ""},
		{"capitalized", "Source", ""},
		{"upper case", "SOURCE", ""},
		{"odd extra field", "source", `,"x":1e400`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
			d := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_decide", "arguments": map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}}}}))["structuredContent"].(map[string]any)
			raw := json.RawMessage(fmt.Sprintf(`{"trace_id":%q,"truth":"change_request",%q:"ask","weight":1%s}`, d["trace_id"], tc.key, tc.extra))
			send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_label", "arguments": raw}})
			st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
			labs, _ := st.LabeledTraces("intent")
			if l, ok := labs[d["trace_id"].(string)]; !ok || l.Source != "mcp" {
				t.Fatalf("the MCP label must be saved as the agent's (source mcp): saved=%v source=%q weight=%v", ok, l.Source, l.Weight)
			}
		})
	}
}

// 0.9.5 check: MCP and the page take --local / --hosted, but decisions they make through hookDecide (reflect place: where a lesson goes) used the
// hook's fixed model URL, so `shikii mcp --local <other model>` still asked http://127.0.0.1:8080 for those (and a page it started got :8080 too).
// The URLs given to mcp and serve are now the ones hookDecide uses in that process.
func TestMCPLocalReachesHookDecisions(t *testing.T) {
	oldLocal, oldHosted := hookLocalURL, hookHostedURL
	t.Cleanup(func() { hookLocalURL, hookHostedURL = oldLocal, oldHosted })
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	hookLocalURL, hookHostedURL = "http://127.0.0.1:8080", "https://api.typesafe.ai"
	if _, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:47391", "--hosted", "https://example.invalid/decisions"}, mcpInit(1)); err != nil {
		t.Fatal(err)
	}
	if hookLocalURL != "http://127.0.0.1:47391" || hookHostedURL != "https://example.invalid/decisions" {
		t.Fatalf("mcp's --local and --hosted reach the decisions made through hookDecide: local=%s hosted=%s", hookLocalURL, hookHostedURL)
	}
}

// runMCPProcess runs cmdMCP as Codex does: stdin and stdout are the process's own, and it starts in the current folder.
// It returns the answers by id and cmdMCP's error.
func runMCPProcess(t *testing.T, args []string, reqs ...map[string]any) (map[float64]map[string]any, error) {
	t.Helper()
	oldLocal, oldHosted := hookLocalURL, hookHostedURL // cmdMCP points hookDecide at its --local / --hosted (useForHookDecisions)
	t.Cleanup(func() { hookLocalURL, hookHostedURL = oldLocal, oldHosted })
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

func TestProjectDefaultCLIRejectsLinkedStore(t *testing.T) {
	a, b := gitRepo(t), gitRepo(t)
	bStore := filepath.Join(b, ".shikii")
	if err := os.MkdirAll(filepath.Join(bStore, "decisions"), 0700); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, filepath.Join(bStore, "decisions"), "true")
	if err := os.Symlink(bStore, filepath.Join(a, ".shikii")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	if _, _, err := currentProjectStore(); err == nil {
		t.Fatal("the precondition that the check rejects links does not hold")
	}
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": .9, "question": .1, "other": 0})
	var output bytes.Buffer
	old := stdout
	stdout = &output
	defer func() { stdout = old }()
	err := cmdDecide([]string{".shikii/decisions/intent.yaml", "--local", model.URL, "--model", "m", "-i", "message=AUDIT_PROJECT_A_SYNTHETIC"})
	if err == nil {
		t.Fatal("must reject on the store check error and not save into another work")
	}
	if err := cmdServe([]string{"--specs", filepath.Join(a, "missing-specs")}); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("the page must also carry over the store refusal: %v", err)
	}
	// MCP answers the handshake (0.9.3: it never exits before it), and each tool call carries over the store refusal
	answers, err := runMCPProcess(t, []string{"--specs", filepath.Join(bStore, "decisions"), "--local", model.URL, "--model", "m"}, mcpInit(1),
		mcpCall(2, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "AUDIT_PROJECT_A_SYNTHETIC"}}))
	if err != nil {
		t.Fatal(err)
	}
	if text, isErr := toolText(t, answers, 2); !isErr || !strings.Contains(text, "link") {
		t.Fatalf("MCP must also carry over the store refusal: %v %s", isErr, text)
	}
	records, readErr := (store.Store{Dir: bStore}).Records("intent", 0)
	if readErr != nil || len(records) != 0 {
		t.Fatalf("must not change the refused link target: records=%d err=%v", len(records), readErr)
	}

}
