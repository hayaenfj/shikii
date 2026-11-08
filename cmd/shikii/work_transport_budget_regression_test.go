package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic regression fixture; the original evidence is retained.
func TestWorkRegressionR2ContractMCPBudget(t *testing.T) {
	for _, tc := range []struct{ n, budget int }{{0, 8192}, {25, 8192}, {55, 8192}, {25, 16384}} {
		n, budget := tc.n, tc.budget
		t.Run(fmt.Sprintf("lines%d-budget%d", n, budget), func(t *testing.T) {
			b, source := loopBook(t)
			if n > 0 {
				loopPage(t, b, source, map[string]any{"title": "Synthetic routing policy", "body": strings.Repeat("Routing checks the synthetic owner condition.\n", n)})
			}
			args, _ := json.Marshal(map[string]any{"operation": "prepare", "work": map[string]any{"request": "Check synthetic routing.", "query": "routing", "max_bytes": budget}})
			h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
			req := httptest.NewRequest("POST", "/v1/wiki/notebook", bytes.NewReader(args))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("\u6e96\u5099\u5931\u6557 status=%d body=%s", rec.Code, rec.Body.String())
			}
			rpc, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(args)}})
			var wire bytes.Buffer
			if e := runMCP(bufio.NewReader(bytes.NewReader(append(rpc, '\n'))), &wire, h); e != nil {
				t.Fatal(e)
			}
			var envelope map[string]any
			if e := json.Unmarshal(wire.Bytes(), &envelope); e != nil {
				t.Fatal(e)
			}
			result, ok := envelope["result"].(map[string]any)
			if !ok || result["isError"] != false || result["structuredContent"] == nil {
				t.Fatal("MCP\u6e96\u5099\u5931\u6557", envelope)
			}
			t.Logf("article_lines=%d max_bytes=%d HTTP=%d MCP_JSONRPC=%d", n, budget, rec.Body.Len(), wire.Len())
			if wire.Len() > budget {
				t.Errorf("\u6210\u529f\u3057\u305fMCP\u5b9f\u5fdc\u7b54\u304c\u6307\u5b9a\u4e0a\u9650\u3092\u8d85\u904e")
			}
		})
	}
}

func TestWorkRegressionR2ContractMCPPrecision(t *testing.T) {
	raw := []byte(`{"count":9007199254740993,"decimal":0.1234567890123456789,"text":"synthetic"}`)
	v := toolResult(200, raw, "shikii_wiki")
	got, _ := json.Marshal(v["structuredContent"])
	if !bytes.Equal(got, raw) {
		t.Fatalf("structuredContent\u306e\u578b\u307e\u305f\u306f\u6570\u5024\u304c\u5909\u5316\u3057\u305f: %s", got)
	}
	t.Logf("structuredContent\u306f\u5143\u306e\u578b\u3068\u6570\u5024\u3092\u4fdd\u6301: %s", got)
}

func TestWorkRegressionR2ContractRecoveryAndReuse(t *testing.T) {
	b, source := loopBook(t)
	body := strings.Repeat("Synthetic routing requires checking the original condition.\n", 110)
	page := loopPage(t, b, source, map[string]any{"title": "Synthetic routing reuse", "body": body})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check synthetic routing reuse.", "query": "routing", "max_bytes": 8192})
	if err != nil {
		t.Fatal(err)
	}
	row := first["knowledge"].([]any)[0].(map[string]any)
	if row["body_included"] != false {
		t.Fatalf("\u7701\u7565\u5074\u306e\u6e96\u5099\u304c\u3067\u304d\u306a\u3044: %v", row["body_included"])
	}
	second, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "routing"})
	if err != nil {
		t.Fatal(err)
	}
	row = second["knowledge"].([]any)[0].(map[string]any)
	if row["reused"] == true || row["body_included"] != true {
		t.Fatal("\u672a\u63d0\u793a\u672c\u6587\u304c\u518d\u5229\u7528\u3055\u308c\u305f")
	}
	third, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "routing"})
	if err != nil {
		t.Fatal(err)
	}
	row = third["knowledge"].([]any)[0].(map[string]any)
	if row["reused"] != true || row["body_included"] != false {
		t.Fatal("\u540c\u3058\u7248\u306e\u63d0\u793a\u6e08\u307f\u672c\u6587\u304c\u518d\u5229\u7528\u3055\u308c\u306a\u3044")
	}
	recovered, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row = recovered["knowledge"].([]any)[0].(map[string]any)
	if row["body_included"] != true || row["article"].(map[string]any)["body"] != page.Body {
		t.Fatal("\u660e\u793a\u3057\u305f\u8a18\u4e8b\u7248\u306e\u5168\u6587\u3092\u56de\u5fa9\u3067\u304d\u306a\u3044")
	}
	t.Log("\u7701\u7565\u672c\u6587\u3092\u65e2\u8aad\u306b\u305b\u305a\u3001\u5168\u6587\u63d0\u793a\u5f8c\u3060\u3051\u518d\u5229\u7528\u3057\u3001\u660e\u793a\u3057\u305f\u540c\u4e00\u7248\u306e\u5168\u6587\u3092\u56de\u5fa9\u3057\u305f")
}

func TestWorkRegressionR2ContractCorrectionEvidence(t *testing.T) {
	cases := []struct {
		name, result, correction, decision, evidence string
		complete                                     bool
	}{
		{"unused", "unused", "", "", "", true},
		{"unknown", "unknown", "", "", "", true},
		{"repeated-empty", "unused", "repeated", "", "", false},
		{"repeated-no-decision", "unknown", "repeated", "", "\u5408\u6210\u691c\u67fb\u3067\u518d\u767a\u3092\u78ba\u8a8d\u3057\u305f\u3002", false},
		{"helpful-empty", "helpful", "", "", "", false},
		{"harmful-empty", "harmful", "", "", "", false},
		{"repeated-evidenced", "unused", "repeated", "\u5408\u6210\u306e\u8a02\u6b63\u518d\u767a\u3092\u7167\u5408\u3059\u308b\u3002", "\u5408\u6210\u691c\u67fb\u3067\u540c\u3058\u6761\u4ef6\u306e\u518d\u767a\u3092\u78ba\u8a8d\u3057\u305f\u3002", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, scope, h, key := correctionSeed(t)
			report, e := st.MemoryReport(scope)
			if e != nil || len(report.All) == 0 {
				t.Fatal("\u6e96\u5099\u5931\u6557", e)
			}
			entry := map[string]string{"source_id": report.All[0].SourceID, "key": key, "result": tc.result, "correction_result": tc.correction, "decision": tc.decision, "evidence": tc.evidence}
			result := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{entry}})
			if result["complete"] != tc.complete {
				t.Fatalf("\u8a55\u4fa1\u306e\u8a3c\u62e0\u5883\u754c\u304c\u7570\u306a\u308b: complete=%v expected=%v", result["complete"], tc.complete)
			}
			t.Logf("result=%s correction=%s decision=%v evidence=%v complete=%v", tc.result, tc.correction, tc.decision != "", tc.evidence != "", result["complete"])
		})
	}
}

func TestWorkRegressionR2ContractCLI(t *testing.T) {
	b, source := loopBook(t)
	t.Chdir(b.Scope)
	loopPage(t, b, source, map[string]any{"title": "Synthetic routing policy", "body": strings.Repeat("Routing checks the synthetic owner condition.\n", 55)})
	input, _ := json.Marshal(map[string]any{"request": "Check synthetic routing.", "query": "routing", "max_bytes": 8192})
	oldIn, oldOut := stdin, stdout
	var output bytes.Buffer
	stdin, stdout = bytes.NewReader(input), &output
	defer func() { stdin, stdout = oldIn, oldOut }()
	if err := cmdWork([]string{"prepare"}); err != nil {
		t.Fatal(err)
	}
	if output.Len() > 8192 {
		t.Fatalf("CLI\u5fdc\u7b54\u304c\u4e0a\u9650\u8d85\u904e: %d", output.Len())
	}
	t.Logf("CLI\u306e\u5b9f\u5fdc\u7b54\u306f\u4e0a\u9650\u5185: max_bytes=8192 bytes=%d", output.Len())
}

func TestWorkRegressionR2ContractConversationPaging(t *testing.T) {
	b, id := workPrepareSourceFixture(t, strings.Repeat("\u65e5\u672c\u8a9e<&>\u3068\u5f15\u7528\\\"\u306e\u4f1a\u8a71\u3002\n", 1700), false)
	all, e := b.ReadSources([]string{id})
	if e != nil {
		t.Fatal(e)
	}
	canonical := loopObject(t, all)["originals"].(map[string]any)[id]
	want, _ := json.Marshal(canonical)
	joined, cursor := "", ""
	pages := 0
	for ; pages < 100; pages++ {
		v, e := b.ReadSourcesPage([]string{id}, cursor, 4096)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(v)
		if len(raw) > 4096 {
			t.Fatalf("\u4f1a\u8a71\u539f\u6587\u304c\u4e0a\u9650\u8d85\u904e: %d", len(raw))
		}
		part, ok := v["original_part"].(map[string]any)
		if !ok {
			t.Fatal("\u4f1a\u8a71\u539f\u6587\u306e\u7d99\u7d9a\u60c5\u5831\u304c\u7121\u3044")
		}
		if part["from"] != len(joined) {
			t.Fatalf("\u4f1a\u8a71\u539f\u6587\u306e\u4f4d\u7f6e\u304c\u4e0d\u9023\u7d9a: %v/%d", part["from"], len(joined))
		}
		joined += part["json"].(string)
		next, _ := v["next_cursor"].(string)
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatal("\u4f1a\u8a71cursor\u304c\u9032\u307e\u306a\u3044")
		}
		cursor = next
	}
	var decoded any
	if pages >= 100 || json.Unmarshal([]byte(joined), &decoded) != nil {
		t.Fatal("\u4f1a\u8a71\u539f\u6587\u306eJSON\u5168\u56de\u5fa9\u306b\u5931\u6557")
	}
	got, _ := json.Marshal(decoded)
	if !bytes.Equal(got, want) {
		t.Fatal("\u4f1a\u8a71\u539f\u6587\u306e\u5168\u56de\u5fa9\u306b\u5dee\u304c\u3042\u308b")
	}
	t.Logf("Unicode\u3068JSON\u30a8\u30b9\u30b1\u30fc\u30d7\u3092\u542b\u3080\u4f1a\u8a71\u539f\u6587\u3092\u5168\u56de\u5fa9: pages=%d JSON_bytes=%d", pages+1, len(joined))
}
