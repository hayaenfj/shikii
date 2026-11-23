package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// shikii mcp: serves the same content as serve, as a stdio MCP server (JSON-RPC 2.0, one message per line).
// Agents such as Claude Code can call deciding, labeling and checking status as tools. It does no inference itself.
//
//	tools: shikii_specs / shikii_decide / shikii_label / shikii_status
//	example config (.mcp.json): {"mcpServers":{"shikii":{"command":"shikii","args":["mcp","--specs","decisions","--local","http://127.0.0.1:8080"]}}}
//
// Internally it calls the HTTP handlers as they are (the logic is not kept twice).

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	var o serveOpts
	o.ef.bind(fs)
	fs.StringVar(&o.specsDir, "specs", "", "folder of specs (*.yaml) (default: <store>/decisions)")
	fs.StringVar(&o.storeDir, "store", ".shikii", "where records and labels are kept; .shikii is the project of the folder it starts in. Without --store or --specs there is no start project and every tool call must name project")
	fs.BoolVar(&o.autoCalibrate, "auto-calibrate", true, "calibrate automatically and write back to the spec once truths reach eval.min_n")
	fs.StringVar(&o.proposer, "proposer", "", "URL of the proposer (a chat LLM) that drafts wording candidates")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "model name of the proposer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log.SetOutput(os.Stderr) // stdout is the MCP channel. Logs go to stderr
	o.ef.useForHookDecisions()
	// A missing or unreadable project never stops the server before the handshake (0.9.3: Codex starts it in the folder its daemon started in,
	// which often has no shikii project). Tool calls then name their project with project, or get a tool error that says to.
	r := &mcpRouter{opts: o}
	storeNamed := false
	fs.Visit(func(f *flag.Flag) { storeNamed = storeNamed || f.Name == "store" || f.Name == "specs" })
	if !storeNamed {
		// started without --store: a shared registration (Codex's, started in its daemon's folder). A call that forgets project must not land in
		// whatever project that folder is, so there is no start store (0.9.3 audit 2, low 1)
		r.startErr = errors.New("started without --store (a shared registration such as Codex's)")
	} else if err := resolveStoreFlag(fs); err != nil {
		r.startErr = err
	} else {
		if o.specsDir == "" {
			o.specsDir = specsDirOf(o.storeDir)
		}
		if _, err := loadSpecs(o.specsDir); err != nil {
			r.startErr = err
		} else {
			r.start = newServer(o)
			r.startStore = feedbackScope(o.storeDir)
		}
	}
	if r.startErr != nil {
		log.Printf("shikii mcp: no start project (%v); each tool call must name its project", r.startErr)
	} else {
		log.Printf("shikii mcp: specs=%s store=%s local=%q hosted=%q", o.specsDir, o.storeDir, systemone.RedactURL(o.ef.local), systemone.RedactURL(o.ef.hosted))
	}
	return runMCP(bufio.NewReader(os.Stdin), os.Stdout, r)
}

// mcpRouter picks the handler for each tool call: the project named by the call's project argument, or the store the server started with.
type mcpRouter struct {
	opts       serveOpts    // the start options (model endpoints and so on), reused for every project
	start      http.Handler // the start store's server; nil when the server started without a usable project
	startStore string       // the start store, resolved, to reuse start for a call naming the same project
	startErr   error        // why there is no start store
	projects   map[string]http.Handler
}

func (r *mcpRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.start == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": r.noProject().Error()})
		return
	}
	r.start.ServeHTTP(w, req)
}

func (r *mcpRouter) noProject() error {
	msg := "this shikii MCP server has no project"
	if r.startErr != nil {
		msg += " (" + r.startErr.Error() + ")"
	}
	return errors.New(msg + ": pass project, the absolute path of a registered project; for a worktree pass the original project")
}

// handler returns the server for a call. Without project it is the start store; with project it is that registered project's own server (kept per project).
func (r *mcpRouter) handler(project string, named bool) (http.Handler, error) {
	if !named {
		if r.start == nil {
			return nil, r.noProject()
		}
		return r.start, nil
	}
	root, scope, err := mcpProjectStore(project)
	if err != nil {
		return nil, err
	}
	if r.start != nil && r.startStore == feedbackScope(root) {
		return r.start, nil
	}
	if h := r.projects[scope]; h != nil {
		return h, nil
	}
	o := r.opts
	o.storeDir, o.specsDir, o.wikiDir, o.projectScope = root, "", "", scope
	h := newServer(o)
	if r.projects == nil {
		r.projects = map[string]http.Handler{}
	}
	r.projects[scope] = h
	return h, nil
}

// mcpProjectStore resolves a tool call's project: an absolute folder inside a shikii project registered on this machine (the index init writes
// under ~/.shikii/agent/projects), whose store passes the same checks as the hook's. Anything else is refused, never created.
func mcpProjectStore(project string) (string, string, error) {
	if !filepath.IsAbs(project) {
		return "", "", fmt.Errorf("project must be the absolute path of a registered project, got %q", project)
	}
	if info, err := os.Stat(project); err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("project %q is not a folder on this machine", project)
	}
	agent, err := agentHome()
	if err != nil {
		return "", "", err
	}
	// the project folder, found as the hooks find it, or the nearest project above it that this machine's index vouches for by its
	// store's id (projectAbove: a copy of a store at the top of a worktree is passed over, and so are a store the index does not
	// vouch for and the shared place; final confirmation audit of 0.37.0)
	scope := projectScopeAt(project)
	if !registeredProjectAt(agent, scope) {
		scope = projectAbove(feedbackScope(project), "")
	}
	if scope == "" {
		return "", "", fmt.Errorf("project %q is not in a shikii project registered on this machine: for a worktree pass the original project without initializing another wiki; for a new project run shikii init in that project's folder", project)
	}
	root := filepath.Join(scope, ".shikii")
	if err := validateStoreDirectories(root); err != nil {
		return "", "", fmt.Errorf("project %q: %w", project, err)
	}
	return root, scope, nil
}

// takeProject removes the project argument (any key the handlers would read as project) from a tool call's arguments.
// Arguments that are not an object are passed on unchanged.
func takeProject(args json.RawMessage) (project string, named bool, rest json.RawMessage, err error) {
	if len(args) == 0 {
		return "", false, args, nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(args, &m) != nil || m == nil {
		return "", false, args, nil
	}
	for k, v := range m {
		if !strings.EqualFold(k, "project") {
			continue
		}
		if named {
			return "", false, nil, errors.New("pass project only once")
		}
		if json.Unmarshal(v, &project) != nil {
			return "", false, nil, errors.New("project must be a string: the absolute path of a registered project")
		}
		named = true
		delete(m, k)
	}
	if !named {
		return "", false, args, nil
	}
	b, err := json.Marshal(m)
	return project, true, b, err
}

func init() {
	for _, tl := range mcpTools {
		tl["inputSchema"].(map[string]any)["properties"].(map[string]any)["project"] = mcpProjectSchema
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Result and Error are set by an answer to a request the server sent (a question to the person, mcp_conn.go), not by a request.
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const mcpProtocol = "2025-03-26"

// runMCP reads JSON-RPC one message per line and writes the answers. Notifications (no id) get no answer. One message is handled
// at a time; a tool that has to ask the person (forget) sends its question and reads on from the same stream while it waits (mcp_conn.go).
func runMCP(r *bufio.Reader, w io.Writer, h http.Handler) error {
	lines := make(chan mcpLine)
	done := make(chan struct{})
	defer close(done)
	go func() { // the one reader of r, so that a wait for an answer can end on a time limit without a read left behind
		defer close(lines)
		for {
			data, err := r.ReadBytes('\n')
			select {
			case lines <- mcpLine{data, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	c := &mcpConn{lines: lines, enc: json.NewEncoder(w)}
	for l := range lines {
		if err := c.serve(l.data, h); err != nil {
			return err
		}
		if c.ended { // the wait for an answer met the end of the stream
			return c.endErr
		}
		if l.err == io.EOF {
			return nil
		}
		if l.err != nil {
			return l.err
		}
	}
	return nil
}

// handleRPC answers one request without a stream to ask the person on (a client that cannot be asked, and the tests of the tools).
func handleRPC(req rpcRequest, h http.Handler) map[string]any {
	return (&mcpConn{}).handle(req, h)
}

func (c *mcpConn) handle(req rpcRequest, h http.Handler) map[string]any {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	reply := func(result any) map[string]any {
		if isNotification {
			return nil
		}
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
	}
	fail := func(code int, msg string) map[string]any {
		if isNotification {
			return nil
		}
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": rpcError{code, msg}}
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Elicitation json.RawMessage `json:"elicitation"`
			} `json:"capabilities"`
		}
		_ = json.Unmarshal(req.Params, &p)
		c.elicit = canElicit(p.Capabilities.Elicitation)
		v := mcpProtocol
		if p.ProtocolVersion != "" {
			v = p.ProtocolVersion
		}
		return reply(map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "shikii", "version": shikii.Version},
			"instructions":    mcpBootstrap,
		})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return reply(map[string]any{})
	case "tools/list":
		return reply(map[string]any{"tools": mcpTools})
	case "tools/call":
		// Tool calls need a response ID. An unacknowledged notification cannot
		// create work state, claim knowledge delivery or perform a write.
		if isNotification {
			return nil
		}
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return fail(-32602, "bad params: "+err.Error())
		}
		if !knownTool(p.Name) {
			return fail(-32602, fmt.Sprintf("unknown tool %q", p.Name))
		}
		// project names the caller's project (0.9.3: Codex shares one server among all projects). It is taken out before the handlers read the arguments
		project, named, args, perr := takeProject(p.Arguments)
		if perr != nil {
			return reply(toolError(perr))
		}
		forgetting := false
		if p.Name == "shikii_wiki" {
			// only the operations the definition lists run; arguments that do not decode name no operation and are refused too
			var call struct {
				Operation string `json:"operation"`
			}
			_ = json.Unmarshal(args, &call)
			if refusal := mcpWikiRefusal(call.Operation); refusal != "" {
				return reply(toolError(errors.New(refusal)))
			}
			forgetting = call.Operation == "forget"
		}
		rt, ok := h.(*mcpRouter)
		if !ok {
			rt = &mcpRouter{start: h}
		}
		target, herr := rt.handler(project, named)
		if herr != nil {
			return reply(toolError(herr))
		}
		if forgetting {
			// not the HTTP handler (shikii serve refuses forget): forget asks the person through this connection
			answer, answered := c.forget(req.ID, target, args)
			if !answered {
				return nil // the client cancelled the call: it waits for no answer
			}
			return reply(answer)
		}
		code, body, err := callTool(target, p.Name, args, func(value any) int {
			data, _ := json.Marshal(value)
			return workContextSize(reply(toolResult(http.StatusOK, data, p.Name)))
		})
		if err != nil {
			return fail(-32602, err.Error())
		}
		return reply(toolResult(code, body, p.Name))
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}

type workResponseSizeKey struct{}

// callTool maps a tool name to an HTTP handler. It returns the HTTP status and the JSON body.
func callTool(h http.Handler, name string, args json.RawMessage, sizing ...func(any) int) (int, []byte, error) {
	if len(args) == 0 {
		args = []byte("{}")
	}
	var method, path string
	switch name {
	case "shikii_specs":
		method, path = http.MethodGet, "/v1/specs"
	case "shikii_decide":
		method, path = http.MethodPost, "/v1/decide"
	case "shikii_label":
		method, path = http.MethodPost, "/v1/label"
	case "shikii_status":
		method, path = http.MethodGet, "/v1/status"
	case "shikii_tune":
		method, path = http.MethodPost, "/v1/tune/sync"
	case "shikii_wiki":
		method, path = http.MethodPost, "/v1/wiki/notebook"
	case "shikii_review":
		method, path = http.MethodPost, "/v1/review"
	case "shikii_reflect":
		method, path = http.MethodPost, "/v1/reflect"
	default:
		return 0, nil, fmt.Errorf("unknown tool %q", name)
	}
	var body io.Reader
	if method == http.MethodPost {
		if name == "shikii_label" {
			// A label through MCP is the agent's: mark it "mcp" whatever source it names, so it is never stored as a person's answer
			// (audit S6; behaviour check 0.9.2: an agent passed source "ask" and its own judgment was stored as the person's answer)
			// Values stay raw so that no number can fail the read; arguments that are not an object are refused, never passed on unchanged
			var m map[string]json.RawMessage
			if err := json.Unmarshal(args, &m); err != nil || m == nil {
				return http.StatusBadRequest, []byte(`{"error":"shikii_label arguments must be a JSON object"}`), nil
			}
			// the handler decodes keys case-insensitively (Unicode folding: "Source", "ſource"), so drop every key it would read as source
			for k := range m {
				if strings.EqualFold(k, "source") {
					delete(m, k)
				}
			}
			m["source"] = json.RawMessage(`"mcp"`)
			b, err := json.Marshal(m)
			if err != nil {
				return http.StatusBadRequest, []byte(`{"error":"shikii_label arguments must be a JSON object"}`), nil
			}
			args = b
		}
		body = bytes.NewReader(args)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if len(sizing) > 0 && sizing[0] != nil {
		req = req.WithContext(context.WithValue(req.Context(), workResponseSizeKey{}, sizing[0]))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), nil
}

func knownTool(name string) bool {
	for _, tl := range mcpTools {
		if tl["name"] == name {
			return true
		}
	}
	return false
}

// toolError is a tool result with isError, so the agent reads why and what to pass.
func toolError(err error) map[string]any {
	b, _ := json.Marshal(map[string]any{"error": err.Error()})
	return toolResult(http.StatusBadRequest, b)
}

// toolResult is an MCP tool result. Errors (4xx/5xx) are returned with isError, not as JSON-RPC errors (so the agent can read them and fix things).
func toolResult(code int, body []byte, tool ...string) map[string]any {
	text := strings.TrimSpace(string(body))
	res := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": code >= 400}
	var structured map[string]any
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var extra any
	if d.Decode(&structured) == nil && structured != nil && d.Decode(&extra) == io.EOF && code < 400 {
		res["structuredContent"] = structured
		if len(tool) == 1 && (tool[0] == "shikii_wiki" || tool[0] == "shikii_reflect") {
			text = knowledgeMarkdown(body, structured)
			res["content"] = []map[string]any{{"type": "text", "text": text}}
		}
	}
	return res
}
