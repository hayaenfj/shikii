package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestAuditWorkNoteReportsRemainingPreparedReview(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	id, _ := reviewSeed(t, st, scope, 1)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	pending := v["pending"].([]any)
	if len(pending) != 1 || len(pending[0].(map[string]any)["reviews"].([]any)) == 0 {
		t.Fatal("\u5408\u6210\u8a55\u4fa1\u5bfe\u8c61\u306e\u6e96\u5099\u306b\u5931\u6557")
	}
	source := pending[0].(map[string]any)["ref"].(string)
	result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"title": "README language procedure", "lesson": "Verify requested languages before completing README work.", "applies_to": "README language", "kind": "howto", "scene": "updating README", "sources": []string{source}}}})
	if err != nil {
		t.Fatal(err)
	}
	left, err := reflectLeft(st.Dir, scope, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if len(left) == 0 {
		t.Fatal("\u672a\u8a55\u4fa1\u306e\u5408\u6210\u5bfe\u8c61\u304c\u898b\u3064\u304b\u3089\u306a\u3044")
	}
	if result["complete"] == true {
		t.Fatalf("\u8a18\u4e8b\u3092\u4fdd\u5b58\u3057\u305f\u4f1a\u8a71\u306e\u8a55\u4fa1\u304c\u6b8b\u308b\u306e\u306bcomplete=true\u3092\u8fd4\u3057\u305f: remaining_sources=%d reflection_present=%t", len(left), result["reflection"] != nil)
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 1 {
		t.Fatal(pages, err)
	}
	payload := map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"title": "README language procedure", "lesson": "Verify requested languages before completing README work.", "applies_to": "README language", "kind": "howto", "scene": "updating README", "sources": []string{source}}}}
	repeated, err := workCall(t, b, "record", payload)
	if err != nil || repeated["complete"] != false || repeated["reflection"] == nil {
		t.Fatal("replay must still show the pending review", repeated, err)
	}
	review := pending[0].(map[string]any)["reviews"].([]any)[0].(map[string]any)
	settled, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "reviews": []any{map[string]any{"ref": review["ref"], "answer": "unknown", "reason": "No checked synthetic outcome."}}})
	if err != nil || settled["complete"] != true {
		t.Fatal(settled, err)
	}
	replay, err := workCall(t, b, "record", payload)
	if err != nil || replay["complete"] != true {
		t.Fatal("settled review should complete the saved note's batch", replay, err)
	}
	after, err := b.Live()
	if err != nil || len(after) != 1 || after[0].Revision != pages[0].Revision {
		t.Fatal("review recovery must not duplicate the note", after, err)
	}

}

func TestAuditWorkMemoryKeepsActualDeliveryContext(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	parent, _ := reviewSeed(t, st, scope, 1)
	delivery := store.MemoryDelivery{ID: "0000000000000999", Scope: scope, Request: "CHILD_REQUEST_MARKER verify invoice routing", Parent: parent, Agent: "synthetic-worker", Tool: "Edit TOOL_CONTEXT_MARKER/invoice.go", Phrase: "invoice.go", At: time.Now(), Quotes: []store.MemoryQuote{{ID: "0000000000000888", Key: "feedback:0000000000000888", Request: "Earlier policy request", Summary: "Earlier policy action", Reply: "Preserve tenant checks"}}}
	if err := st.SaveMemoryDelivery(delivery); err != nil {
		t.Fatal(err)
	}
	b := notebook.Book{Root: st.Dir, Scope: scope}
	rawReflection, err := reflectQuery(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := json.Marshal(rawReflection)
	if !strings.Contains(string(full), "CHILD_REQUEST_MARKER") || !strings.Contains(string(full), "TOOL_CONTEXT_MARKER") {
		t.Fatal("\u5f93\u6765\u306e\u53c2\u7167\u306b\u5408\u6210\u306e\u914d\u9001\u6587\u8108\u304c\u7121\u3044")
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(prepared)
	if !strings.Contains(string(raw), "CHILD_REQUEST_MARKER") || !strings.Contains(string(raw), "TOOL_CONTEXT_MARKER") {
		t.Fatal("\u914d\u9001\u3057\u305f\u5b9f\u969b\u306e\u4f9d\u983c\u3068\u9053\u5177\u306e\u6587\u8108\u304c\u6e96\u5099\u306e\u8fd4\u7b54\u304b\u3089\u6d88\u3048\u3001\u89aa\u306e\u4f1a\u8a71\u3060\u3051\u3067\u52b9\u679c\u3092\u5224\u65ad\u3055\u305b\u308b")
	}
}

func TestAudit019DocsAdvertisedQuoteLimit(t *testing.T) {
	schema := workSchema["properties"].(map[string]any)["agreed"].(map[string]any)["properties"].(map[string]any)["quotes"].(map[string]any)
	limit := schema["maxItems"].(int)
	for _, n := range []int{3, limit} {
		t.Run(string(rune('a'+n)), func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
			if err != nil {
				t.Fatal(err)
			}
			quotes := []any{}
			for i := 0; i < n; i++ {
				quotes = append(quotes, map[string]any{"ref": "current", "text": planningTestRequest})
			}
			body, _ := json.Marshal(map[string]any{"operation": "record", "work": map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Preserve CSV filters.", "quotes": quotes}}})
			h := newServer(serveOpts{storeDir: root, projectScope: b.Scope})
			code, raw, err := callTool(h, "shikii_wiki", body)
			var result map[string]any
			if err != nil || code != 200 || json.Unmarshal(raw, &result) != nil {
				t.Fatalf("MCP\u7d4c\u8def\u306e\u547c\u3073\u51fa\u3057\u306b\u5931\u6557: %d %v", code, err)
			}
			if result["complete"] != true {
				t.Fatalf("\u30b9\u30ad\u30fc\u30de\u306e\u4e0a\u9650%d\u4ee5\u5185\u306e%d\u4ef6\u3067\u4fdd\u5b58\u5931\u6557: %v", limit, n, result["errors"])
			}
		})
	}
}

func TestAudit019PlanRetryReturnsItsOriginalResult(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	firstInput := map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}
	first, err := workCall(t, b, "record", firstInput)
	if err != nil || first["complete"] != true {
		t.Fatal(first, err)
	}
	secondInput := map[string]any{"receipt": v["receipt"], "agreed": map[string]any{"summary": "Implement CSV export", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}}}}
	second, err := workCall(t, b, "record", secondInput)
	if err != nil || second["complete"] != true {
		t.Fatal(second, err)
	}
	replay, err := workCall(t, b, "record", firstInput)
	if err != nil || replay["complete"] != true {
		t.Fatal(replay, err)
	}
	before, _ := json.Marshal(first["plan"])
	after, _ := json.Marshal(replay["plan"])
	t.Logf("first recorded=%v replay recorded=%v", first["plan"].(map[string]any)["recorded"], replay["plan"].(map[string]any)["recorded"])
	if string(before) != string(after) {
		t.Fatal("the same earlier input returned another saved plan's result")
	}
}

func TestAudit019ReflectionRetryReturnsItsOriginalResult(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	reviewSeed(t, st, scope, 1)
	reviewSeed(t, st, scope, 2)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	batches := []map[string]any{}
	for _, raw := range v["pending"].([]any) {
		source := raw.(map[string]any)
		review := source["reviews"].([]any)[0].(map[string]any)
		batches = append(batches, map[string]any{"receipt": v["receipt"], "reviews": []any{map[string]any{"ref": review["ref"], "answer": "unknown", "reason": "No checked outcome for this synthetic task."}}, "reviewed": []any{source["ref"]}})
	}
	if len(batches) != 2 {
		t.Fatal(v)
	}
	first, err := workCall(t, b, "record", batches[0])
	if err != nil || first["complete"] != true {
		t.Fatal(first, err)
	}
	second, err := workCall(t, b, "record", batches[1])
	if err != nil || second["complete"] != true {
		t.Fatal(second, err)
	}
	replay, err := workCall(t, b, "record", batches[0])
	if err != nil || replay["complete"] != true {
		t.Fatal(replay, err)
	}
	before, _ := json.Marshal(first["reflection"])
	after, _ := json.Marshal(replay["reflection"])
	if string(before) != string(after) {
		t.Fatal("the same earlier input returned another reflection's result")
	}
}
