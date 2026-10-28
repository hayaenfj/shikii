package main

import (
	"bufio"
	"bytes"
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
	"github.com/hayaenfj/shikii/internal/notebook"
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
	return errors.New(msg + ": pass project, the absolute path of your working folder (a folder where shikii init was run on this machine)")
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
		return "", "", fmt.Errorf("project must be the absolute path of your working folder, got %q", project)
	}
	if info, err := os.Stat(project); err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("project %q is not a folder on this machine", project)
	}
	agent, err := agentHome()
	if err != nil {
		return "", "", err
	}
	registered := map[string]bool{}
	for _, p := range registeredProjects(agent) {
		registered[p.Path] = true
	}
	// the project folder, found as the hook finds it (the git top folder), or the nearest registered folder above it (outside git)
	scope := projectScopeAt(project)
	if !registered[scope] {
		scope = ""
		for dir := feedbackScope(project); dir != "" && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if registered[dir] {
				scope = dir
				break
			}
		}
	}
	if scope == "" {
		return "", "", fmt.Errorf("project %q is not in a shikii project registered on this machine: run shikii init in that project's folder, and pass the absolute path of your working folder", project)
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
			return "", false, nil, errors.New("project must be a string: the absolute path of your working folder")
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

// mcpProjectSchema is the project argument every tool takes.
var mcpProjectSchema = map[string]any{"type": "string", "description": "the absolute path of your working folder, inside a project where shikii init ran on this machine; this call uses its .shikii/. Required when the server is shared (Codex)"}

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
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const mcpProtocol = "2025-03-26"

// mcpTools is the list of tools. inputSchema is JSON Schema.
var correctionSchema = map[string]any{"type": "object", "additionalProperties": false,
	"properties": map[string]any{"source_id": map[string]any{"type": "string"}, "quote": map[string]any{"type": "string"}, "action": map[string]any{"type": "string"}},
	"required":   []string{"source_id", "quote", "action"},
}

var maintenanceSchema = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
	"page_id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
}, "required": []string{"page_id", "revision", "key", "reason"}}

// kindSchema and sceneSchema are a page's kind and scene (0.9.0). The save endpoint requires them on pages with a lesson (omitting them on an edit keeps the old values).
var kindSchema = map[string]any{"type": "string", "enum": notebook.Kinds, "description": "the lesson's kind (required; omit on an edit to keep it): rule=a rule decided in this project, howto=how to do something, criteria=a basis for judging, fact=a premise or fact, history=how things came to be"}
var sceneSchema = map[string]any{"type": "string", "maxLength": notebook.SceneMax, "description": "the scene it applies to, short and in the work's own words (required; e.g. \"when committing\", \"when sending an invoice\"; one line, up to 48 characters). If place returns an existing scene close to it, use the same wording"}

// mcpInstructions is the MCP server's guidance. Agents without a hook (Codex, OpenCode) get only this and the tool descriptions.
const mcpInstructions = "This project's memory is its wiki. Before touching an area, query shikii_wiki with topic words: each lesson is a 'when X, do Y' of this project with the person's words as its sources, a hypothesis to check, never permission. When the person corrects you, or you make or change a rule, test or procedure (commands, scripts) yourself, write it to the wiki in the same turn: edit the closest page or make a howto or rule page (a kind, one of rule/howto/criteria/fact/history, and a scene in the project's own words, reusing one place returns; classify unclassified pages query lists before the turn ends). Truths come only from the person's answer or a checked result: never call shikii_label with your own judgment. Decisions: shikii_specs lists the questions, shikii_decide asks the decision model under this project's lines (act / ask / discard). Pass the absolute path of your working folder as project on every call (omit it only when the server runs inside the project)."

var mcpTools = []map[string]any{
	{
		"name":        "shikii_reflect",
		"description": "Reflection in one place (query shows 2 conversations at a time from the oldest; submit takes any number). query reads each conversation's original text, targets, delivered lessons and the person's corrections; place takes a draft lesson and returns where it could go and suspected contradictions; submit saves reviews, wiki and memory. memory records helpful/unused/harmful/unknown with the decision it was used for and the evidence checked, plus correction_result (followed/repeated/not_applicable/unknown) for a delivered correction; delivery or a successful task alone is no effect, and too little evidence is unknown. A correction lesson carries correction:{source_id,quote,action}. Never mixed into truths or calibration. If the reply says complete:false, fix each error and resend; complete:true only says every item was saved: while left lists a conversation, submit what it names before finishing.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"operation":   map[string]any{"type": "string", "enum": []string{"query", "place", "submit"}},
			"maintenance": map[string]any{"type": "array", "items": maintenanceSchema},
			"memory": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"source_id": map[string]any{"type": "string"}, "key": map[string]any{"type": "string"},
				"result":   map[string]any{"type": "string", "enum": []string{"helpful", "unused", "harmful", "unknown"}},
				"decision": map[string]any{"type": "string"}, "evidence": map[string]any{"type": "string"},
				"correction_result": map[string]any{"type": "string", "enum": []string{"followed", "repeated", "not_applicable", "unknown"}},
			}, "required": []string{"source_id", "key", "result", "decision", "evidence"}}},
			"reviews": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"source_id": map[string]any{"type": "string"}, "trace_id": map[string]any{"type": "string"},
				"truth": map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}}, "reason": map[string]any{"type": "string"},
			}, "required": []string{"source_id", "trace_id", "truth", "reason"}}},
			"wiki": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"sources":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"correction":    correctionSchema,
				"change_reason": map[string]any{"type": "string"},
				"id":            map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "lesson": map[string]any{"type": "string"}, "applies_to": map[string]any{"type": "string"},
				"replaces": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"body":     map[string]any{"type": "string", "description": "the knowledge text, combining background, evidence, conditions and open points from the sources. Up to 16KB. Omit to keep the existing text; an empty string clears it"},
				"related":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of related pages in the same project. Omit to keep; an empty array clears"},
				"kind":     kindSchema,
				"scene":    sceneSchema,
			}, "required": []string{"sources"}}},
		}, "required": []string{"operation"}},
	},
	{
		"name":        "shikii_review",
		"description": "Tentatively scores, with evidence, whether the work stayed within what was asked and was completed, from the conversation and the work's results. Use query to check unscored original text, targets and examples of the person's corrections for the same work; save source_id / trace_id / truth / reason with submit. If the evidence is not enough, use unknown. Do not count a follow-up request, a greeting or silence alone as success or failure. Never mix this into human truths or calibration. shikii picks the spot checks for the person.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"operation": map[string]any{"type": "string", "enum": []string{"query", "submit"}},
			"source_id": map[string]any{"type": "string"}, "trace_id": map[string]any{"type": "string"},
			"truth":  map[string]any{"type": "string", "enum": []string{"yes", "no", "unknown"}},
			"reason": map[string]any{"type": "string", "description": "the concrete reason for the score, from the original text and the result"},
		}, "required": []string{"operation"}},
	},
	{
		"name":        "shikii_specs",
		"description": "List the decision specs shikii knows: id, type, question, thresholds and the answers (truths) each accepts. Call this first to learn spec ids and valid truths.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name":        "shikii_decide",
		"description": "Ask a decision model one typed question defined by a spec. Returns the band (act = safe to act automatically, ask = a human should confirm, discard = do nothing, abstain = not decided), the top answer, probabilities and a trace_id. act only appears when the spec is calibrated for this model.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":    map[string]any{"type": "string", "description": "spec id (see shikii_specs)"},
				"inputs":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "values for the spec's state template, e.g. {\"message\": \"...\"}. If the spec has state.context, also pass its extra inputs (e.g. \"previous\"): they are used only when the minimal question is unsure"},
				"tainted": map[string]any{"type": "boolean", "description": "true if this session touched production data: the request then never leaves the machine"},
			},
			"required": []string{"spec", "inputs"},
		},
	},
	{
		"name":        "shikii_label",
		"description": "Record what was actually true for an earlier decision (by trace_id). Use the option id, yes/no, or the level index as truth; '!x' means 'it was not x'. When enough truths exist the spec is calibrated automatically and the result says so.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id": map[string]any{"type": "string"},
				"truth":    map[string]any{"type": "string"},
				"spec":     map[string]any{"type": "string", "description": "optional spec id to narrow the lookup"},
				"weight":   map[string]any{"type": "number", "description": "1 for a firm truth, lower (e.g. 0.5) for a weak one"},
				"source":   map[string]any{"type": "string", "description": "ignored: a truth sent through MCP is always recorded with source mcp (sent by the agent). It still counts as a truth and takes the decision off the person's Answer page, so send only what the person said or a checked result, never your own judgment"},
			},
			"required": []string{"trace_id", "truth"},
		},
	},
	{
		"name":        "shikii_tune",
		"description": "Score the candidate wordings of a spec (its variants:, plus N proposed by the configured chat model when propose > 0) on the stored truths. Returns per-candidate top-1, Brier, precision/recall at act, the holdout, and whether a winner may be adopted. Adoption itself is not a tool: a person must confirm on the page that the new wording still asks the same thing.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"spec":    map[string]any{"type": "string"},
				"propose": map[string]any{"type": "integer", "description": "how many candidates the proposer should add (0 = only the spec's own variants)"},
			},
			"required": []string{"spec"},
		},
	},
	{
		"name":        "shikii_status",
		"description": "Inventory per spec: records, truths, and for each (model, wording hash) whether it is collecting, ready, calibrated or stale.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
	},
	{
		"name":        "shikii_wiki",
		"description": "Grows this project's wiki. query reads lessons, unprocessed sources and up to 2 pages to maintain (page on with next_cursor); a query naming source ids returns their original text. capture saves a source you checked (code, a test, a Release ...) with kind, version, time and reference. ingest saves a lesson: a short lesson and a body (background, reasons, open points), applies_to, source ids, related pages, a kind and a scene in the work's own words; id updates a page (send lesson, title and applies_to again), replaces retires old ones, and sources alone marks them processed. For an explicit correction add correction:{source_id,quote,action} (the person's reply verbatim, the next action). classify gives a kind and a scene to the unclassified pages (from before 0.9.0) that query lists; maintain records a review (latest page_id, revision, key, reason). lint checks sources and links and rebuilds the index. handoff_list / handoff_read / handoff_write / handoff_append handle work records in .shikii/handoff/ (files you rewrite as you go: audit reports, release steps and records, diffs), which the person reads on the page. Lessons go to ingest, evidence as checked to capture, working files to handoff. Everything is a hypothesis: a quote does not prove its meaning, and nothing here changes truths, calibration or the confirmed state. Do not ask the person to operate the page.",
		"inputSchema": map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"operation":   map[string]any{"type": "string", "enum": []string{"query", "ingest", "capture", "maintain", "lint", "classify", "handoff_list", "handoff_read", "handoff_write", "handoff_append"}},
				"path":        map[string]any{"type": "string", "description": "path of a work record (handoff_*), relative to .shikii/handoff/ (e.g. audit-0.8.1/report-A.md). Refuses .., hidden names and absolute paths"},
				"content":     map[string]any{"type": "string", "description": "text for handoff_write (replace) or handoff_append (add to the end). UTF-8 text, up to 1 MiB per call"},
				"maintenance": maintenanceSchema,
				"observation": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
					"kind":  map[string]any{"type": "string", "enum": []string{"human", "agent", "code", "test", "release"}},
					"title": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "reference": map[string]any{"type": "string"}, "version": map[string]any{"type": "string"}, "observed_at": map[string]any{"type": "string", "format": "date-time"},
				}, "required": []string{"kind", "title", "content", "reference", "version", "observed_at"}, "description": "a copy of a source you checked. The program never runs or fetches the reference. It is an observation the AI provides and never becomes permission to act or a truth"},
				"query":         map[string]any{"type": "string", "description": "topic words to search for. Returns search.total and candidates; when there are many, pass next_cursor to cursor with the same search. It is not a semantic search, so check that the topic matches"},
				"cursor":        map[string]any{"type": "string", "description": "the next_cursor from a response to the same query or sources. Read the rest instead of cutting by count"},
				"sources":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of sources observed in this work. When there are many, pass next_cursor to cursor with the same sources to read the rest"},
				"id":            map[string]any{"type": "string", "description": "id of the existing lesson to update"},
				"correction":    correctionSchema,
				"change_reason": map[string]any{"type": "string"},
				"title":         map[string]any{"type": "string"},
				"lesson":        map[string]any{"type": "string", "description": "a summary used for search and short quotes. Up to 2000 bytes. It is the machine's interpretation, so it is saved as a hypothesis"},
				"body":          map[string]any{"type": "string", "description": "the knowledge text, combining background, evidence, conditions and open points from the sources. Up to 16KB. Omit to keep the existing text; an empty string clears it"},
				"related":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of related pages in the same project. Omit to keep; an empty array clears"},
				"applies_to":    map[string]any{"type": "string", "description": "the conditions under which this lesson applies"},
				"replaces":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ids of old lessons from the same work that this lesson updates. Omitting it on an update keeps the existing value; an empty array removes the replacement"},
				"kind":          kindSchema,
				"scene":         sceneSchema,
			},
			"required": []string{"operation"},
		},
	},
}

// runMCP reads JSON-RPC one message per line and writes the answers. Notifications (no id) get no answer.
func runMCP(r *bufio.Reader, w io.Writer, h http.Handler) error {
	enc := json.NewEncoder(w)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var req rpcRequest
			if jerr := json.Unmarshal(line, &req); jerr != nil {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError{-32700, "parse error: " + jerr.Error()}})
			} else if resp := handleRPC(req, h); resp != nil {
				if err := enc.Encode(resp); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func handleRPC(req rpcRequest, h http.Handler) map[string]any {
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
		}
		_ = json.Unmarshal(req.Params, &p)
		v := mcpProtocol
		if p.ProtocolVersion != "" {
			v = p.ProtocolVersion
		}
		return reply(map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "shikii", "version": shikii.Version},
			"instructions":    mcpInstructions,
		})
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "ping":
		return reply(map[string]any{})
	case "tools/list":
		return reply(map[string]any{"tools": mcpTools})
	case "tools/call":
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
		rt, ok := h.(*mcpRouter)
		if !ok {
			rt = &mcpRouter{start: h}
		}
		target, herr := rt.handler(project, named)
		if herr != nil {
			return reply(toolError(herr))
		}
		code, body, err := callTool(target, p.Name, args)
		if err != nil {
			return fail(-32602, err.Error())
		}
		return reply(toolResult(code, body))
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}

// callTool maps a tool name to an HTTP handler. It returns the HTTP status and the JSON body.
func callTool(h http.Handler, name string, args json.RawMessage) (int, []byte, error) {
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
func toolResult(code int, body []byte) map[string]any {
	text := strings.TrimSpace(string(body))
	res := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": code >= 400}
	var structured map[string]any
	if json.Unmarshal(body, &structured) == nil && code < 400 {
		res["structuredContent"] = structured
	}
	return res
}
