package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func regressionRPC(t *testing.T, h http.Handler, fields map[string]any) (map[string]any, int) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": map[string]any{"operation": "prepare", "work": fields}})
	v := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)
	raw, _ := json.Marshal(v)
	r, ok := v["result"].(map[string]any)
	if !ok {
		t.Fatalf("RPC setup: %#v", v)
	}
	return r, len(raw) + 1
}

func TestWorkFinalEnvelopeMCPSourcePages(t *testing.T) {
	for _, budget := range []int{8192, 16384} {
		for _, titleSize := range []int{10, 150} {
			t.Run(fmt.Sprintf("budget%d-title%d", budget, titleSize), func(t *testing.T) {
				b, _ := loopBook(t)
				body := strings.Repeat("\u5408\u6210\u306e\u6761\u4ef6<&>\u3092\u78ba\u8a8d\u3057\u305f\u3002\n", 700)
				o, e := b.CaptureObserved(notebook.Observation{Kind: "test", Title: strings.Repeat("<", titleSize), Content: body, Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
				if e != nil {
					t.Fatal("setup", e)
				}
				prep, e := workCall(t, b, "prepare", map[string]any{"request": "\u5408\u6210\u306e\u539f\u8cc7\u6599\u3092\u56de\u53ce\u3059\u308b\u3002", "query": "source"})
				if e != nil {
					t.Fatal("setup", e)
				}
				h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
				cursor, joined := "", ""
				for page := 0; page < 100; page++ {
					r, n := regressionRPC(t, h, map[string]any{"receipt": prep["receipt"], "query": o.ID, "cursor": cursor, "max_bytes": budget})
					if r["isError"] != false {
						t.Fatalf("source delivery failed on page=%d bytes=%d: %v", page, n, r["content"])
					}
					if n > budget {
						t.Fatalf("successful MCP exceeds budget: %d>%d", n, budget)
					}
					c := r["structuredContent"].(map[string]any)
					if c["task"] != "\u5408\u6210\u306e\u539f\u8cc7\u6599\u3092\u56de\u53ce\u3059\u308b\u3002" || c["task_context"] == nil {
						t.Fatal("every source part must retain the current request and its provenance")
					}
					source := c["source"].(map[string]any)
					part := source["observations"].([]any)[0].(map[string]any)["content"].(string)
					if part == "" {
						t.Fatal("successful page made no progress")
					}
					joined += part
					cursor, _ = source["next_cursor"].(string)
					if cursor == "" {
						if joined != body {
							t.Fatalf("source changed: got=%d want=%d", len(joined), len(body))
						}
						t.Logf("budget=%d title=%d pages=%d recovered=%d final_wire=%d", budget, titleSize, page+1, len(joined), n)
						return
					}
				}
				t.Fatal("cursor failed to finish")
			})
		}
	}
}

func TestWorkFinalEnvelopeMeaningfulQuotesWithPartialClassification(t *testing.T) {
	b, s := loopBook(t)
	original := "Routing requires approval before executing a transfer. Never treat historical consent as current permission."
	p := loopPage(t, b, s, map[string]any{"title": "Routing approval", "body": original, "passages": []any{map[string]any{"role": "history", "quote": "approval before executing a transfer.", "conditions": "Old procedure only.", "reason": "Historical statement retained."}}})
	v, e := workCall(t, b, "prepare", map[string]any{"request": "Check routing approval.", "query": "lesson:" + p.ID, "reasoning": map[string]any{"depth": "brief"}})
	if e != nil {
		t.Fatal("setup", e)
	}
	row := v["knowledge"].([]any)[0].(map[string]any)
	o := row["orientation"].(map[string]any)
	for _, raw := range o["unclassified"].([]any) {
		q := raw.(map[string]any)
		quote := q["text"].(string)
		t.Logf("unclassified quote=%q", quote)
		if !strings.HasSuffix(strings.TrimSpace(quote), ".") {
			t.Errorf("quote is a severed sentence: %q", quote)
		}
	}
}

func TestWorkFinalEnvelopeAssignmentFinalBudget(t *testing.T) {
	for _, n := range []int{0, 10, 25, 55, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			b, s := loopBook(t)
			if n > 0 {
				loopPage(t, b, s, map[string]any{"title": "Synthetic routing policy", "body": strings.Repeat("Routing checks the synthetic owner condition.\n", n)})
			}
			a := assignmentInput(fmt.Sprintf("r3-budget-%d", n), "r3-group", "reviewer", "review")
			fields := map[string]any{"assignment": a, "query": "routing", "max_bytes": 8192}
			h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
			raw, _ := json.Marshal(map[string]any{"operation": "prepare", "work": fields})
			req := httptest.NewRequest("POST", "/v1/wiki/notebook", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatal("setup HTTP", rec.Code, rec.Body.String())
			}
			t.Logf("lines=%d HTTP_wire=%d", n, rec.Body.Len())
			if rec.Body.Len() > 8192 {
				t.Errorf("successful assigned HTTP response exceeds max_bytes: %d>8192", rec.Body.Len())
			}
			r, size := regressionRPC(t, h, fields)
			if r["isError"] != false {
				t.Fatal("setup MCP", r["content"])
			}
			t.Logf("lines=%d MCP_wire=%d", n, size)
			if size > 8192 {
				t.Errorf("successful assigned MCP response exceeds max_bytes: %d>8192", size)
			}
		})
	}
}
