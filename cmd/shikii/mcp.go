package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// shikii mcp: serve と同じ中身を、stdio の MCP サーバー(JSON-RPC 2.0、1 行 1 メッセージ)として出す。
// Claude Code などのエージェントが、判定・正解付け・状態の確認を道具として呼べる。推論は持たない。
//
//	道具: shikii_specs / shikii_decide / shikii_label / shikii_status
//	設定例(.mcp.json): {"mcpServers":{"shikii":{"command":"shikii","args":["mcp","--specs","decisions","--local","http://127.0.0.1:8080"]}}}
//
// 中身は HTTP のハンドラをそのまま内部で呼ぶ(ロジックを二重に持たない)。

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	var o serveOpts
	o.ef.bind(fs)
	fs.StringVar(&o.specsDir, "specs", "decisions", "spec(*.yaml)のフォルダ")
	fs.StringVar(&o.storeDir, "store", ".shikii", "記録とラベルの置き場")
	fs.BoolVar(&o.autoCalibrate, "auto-calibrate", true, "正解が eval.min_n に届いたら自動で較正して spec に書き戻す")
	fs.StringVar(&o.proposer, "proposer", "", "聞き方の候補を作る提案役(チャット用 LLM)の URL")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "提案役のモデル名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := loadSpecs(o.specsDir); err != nil {
		return err
	}
	log.SetOutput(os.Stderr) // stdout は MCP の通信路。ログは stderr へ
	log.Printf("shikii mcp: specs=%s store=%s local=%q hosted=%q", o.specsDir, o.storeDir, systemone.RedactURL(o.ef.local), systemone.RedactURL(o.ef.hosted))
	return runMCP(bufio.NewReader(os.Stdin), os.Stdout, newServer(o))
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

// mcpTools は道具の一覧。inputSchema は JSON Schema。
var mcpTools = []map[string]any{
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
				"source":   map[string]any{"type": "string", "description": "where the truth came from, e.g. outcome, ask, manual"},
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
}

// runMCP は 1 行 1 メッセージの JSON-RPC を読み、答えを書く。通知(id 無し)には答えない。
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
			"serverInfo":      map[string]any{"name": "shikii", "version": "0.1"},
			"instructions":    "shikii turns a decision model's probability into act / ask / discard. Call shikii_specs first; decide with shikii_decide; when you learn what was true, call shikii_label with the trace_id so the spec can be calibrated.",
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
		code, body, err := callTool(h, p.Name, p.Arguments)
		if err != nil {
			return fail(-32602, err.Error())
		}
		return reply(toolResult(code, body))
	default:
		return fail(-32601, "method not found: "+req.Method)
	}
}

// callTool は道具名を HTTP のハンドラに写す。戻りは HTTP の状態と JSON 本文。
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
	default:
		return 0, nil, fmt.Errorf("unknown tool %q", name)
	}
	var body io.Reader
	if method == http.MethodPost {
		if name == "shikii_label" {
			// エージェントのラベルは人のラベルと区別できるように、出どころが無ければ "mcp" を付ける(監査 S6)
			var m map[string]any
			if json.Unmarshal(args, &m) == nil {
				if s, _ := m["source"].(string); s == "" {
					m["source"] = "mcp"
					if b, err := json.Marshal(m); err == nil {
						args = b
					}
				}
			}
		}
		body = bytes.NewReader(args)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), nil
}

// toolResult は MCP の道具の結果。誤り(4xx/5xx)は isError で返し、JSON-RPC のエラーにはしない(エージェントが読んで直せる)。
func toolResult(code int, body []byte) map[string]any {
	text := strings.TrimSpace(string(body))
	res := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": code >= 400}
	var structured map[string]any
	if json.Unmarshal(body, &structured) == nil && code < 400 {
		res["structuredContent"] = structured
	}
	return res
}
