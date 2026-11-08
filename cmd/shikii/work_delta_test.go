package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkDeltaKeepsTaskAtResponseBoundary(t *testing.T) {
	b, _ := loopBook(t)
	for _, n := range []int{100, 11000} {
		task := strings.Repeat("x", n) + " END_CONDITION: never publish; retain the unresolved owner choice."
		v, err := workCall(t, b, "prepare", map[string]any{"request": task, "query": "unmatched", "max_bytes": 8192})
		if err != nil {
			if n == 100 || !strings.Contains(err.Error(), "max_bytes") {
				t.Fatal("a response that cannot retain the task must fail explicitly", err)
			}
			continue
		}
		if v["task"] != task || v["task_context"] == nil {
			t.Fatal("successful preparation must keep the complete task and provenance", n)
		}
	}
}

func deltaMetadataFixture() map[string]any {
	return map[string]any{"ref": "knowledge1", "id": "0000000000000001", "revision": "0000000000000002", "reused": true, "body_included": false, "query": "lesson:0000000000000001@0000000000000002", "orientation": notebook.KnowledgeOrientation{Summary: strings.Repeat("Preserve the owner condition. ", 30), Conditions: "Synthetic routing only.", Current: []notebook.InterpretedExcerpt{}, History: []notebook.InterpretedExcerpt{}, Recheck: []notebook.InterpretedExcerpt{}, Counterexamples: []notebook.InterpretedExcerpt{}, Unclassified: []notebook.TextExcerpt{}}}
}

func TestWorkDeltaMetadataChecksFinalPayloadAndTransport(t *testing.T) {
	for _, change := range []string{"unchanged", "condition", "unclassified", "learning", "warning", "transport"} {
		t.Run(change, func(t *testing.T) {
			w := &workState{Nonce: "synthetic", Reasoning: &reasoningProgress{Reading: &workReadingTrace{}}}
			first := map[string]any{"knowledge": []any{deltaMetadataFixture()}}
			if _, err := limitWorkPreparation(notebook.Book{}, w, first, map[string]string{}, 65536); err != nil {
				t.Fatal(err)
			}
			rememberWorkMetadata(w, first)
			e := deltaMetadataFixture()
			o := e["orientation"].(notebook.KnowledgeOrientation)
			switch change {
			case "condition":
				o.Conditions = "The new owner condition is unresolved."
			case "unclassified":
				o.Unclassified = []notebook.TextExcerpt{{Text: "An unread exception requires checking."}}
			case "learning":
				e["learning_context"] = map[string]any{"new_evidence": "An independent counterexample."}
			case "warning":
				e["recheck"] = []notebook.PremiseChange{{}}
			}
			e["orientation"] = o
			response := map[string]any{"knowledge": []any{e}}
			size := func(v any) int {
				n := workContextSize(v)
				if change == "transport" && reusedWorkMetadata(v.(map[string]any)) {
					n += 2000
				}
				return n
			}
			if _, err := limitWorkPreparation(notebook.Book{}, w, response, map[string]string{}, 65536, size); err != nil {
				t.Fatal(err)
			}
			if (e["metadata_from"] != nil) != (change == "unchanged") {
				t.Fatal("reuse only exact delivered metadata when the whole transport shrinks", change)
			}
			if change != "unchanged" && w.Reasoning.Reading.Mixed {
				t.Fatal("a rejected candidate must not change reading eligibility")
			}
		})
	}
}

func TestWorkDeltaFailedPreparationDoesNotAdvanceDelivery(t *testing.T) {
	b, _ := readingLearningBook(t)
	first := readingLearningPrepare(t, b, "Keep the final owner choice unresolved.", "")
	_, err := prepareWork(b.Root, b.Scope, workRequest{Receipt: first["receipt"].(string), Query: "Reading source recovery", MaxBytes: 8192, responseSize: func(any) int { return 1 << 20 }})
	if err == nil {
		t.Fatal("control: the synthetic transport must exceed the budget")
	}
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Reading source recovery"})
	if err != nil || next["task"] != first["task"] || next["delivery"] != first["delivery"].(float64)+1 {
		t.Fatal("a failed preparation must preserve the prior receipt and delivery sequence", next["delivery"], err)
	}
}

func BenchmarkWorkDeltaDelivery(b *testing.B) {
	for _, name := range []string{"baseline", "delta"} {
		b.Run(name, func(b *testing.B) {
			w := &workState{Nonce: "synthetic"}
			first := map[string]any{"knowledge": []any{deltaMetadataFixture()}}
			if _, err := limitWorkPreparation(notebook.Book{}, w, first, map[string]string{}, 65536); err != nil {
				b.Fatal(err)
			}
			rememberWorkMetadata(w, first)
			if name == "baseline" {
				w.Nonce = "" // The previous delivery path had no metadata references.
			}
			b.ResetTimer()
			for range b.N {
				reply := map[string]any{"knowledge": []any{deltaMetadataFixture()}, "task": "Check routing; never publish; owner choice unresolved."}
				if _, err := limitWorkPreparation(notebook.Book{}, w, reply, map[string]string{}, 65536); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func deltaPrepare(t *testing.T) (map[string]any, map[string]any, map[string]any) {
	t.Helper()
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Delta routing", "lesson": strings.Repeat("Retain the owner condition and its exception. ", 12), "applies_to": strings.Repeat("When checking the routing owner condition. ", 10), "body": strings.Repeat("Inspect the exact source before applying this routing condition.\n\n", 80)})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check routing; do not publish. The owner choice is unresolved.", "query": "Delta routing"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Delta routing"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Delta routing"})
	if err != nil {
		t.Fatal(err)
	}
	return first, second, third
}

func TestWorkDeltaReferencesDeliveredMetadata(t *testing.T) {
	first, second, third := deltaPrepare(t)
	f := first["knowledge"].([]any)[0].(map[string]any)
	for _, response := range []map[string]any{second, third} {
		e := response["knowledge"].([]any)[0].(map[string]any)
		from, ok := e["metadata_from"].(map[string]any)
		if !ok || from["delivery"] != first["delivery"] || e["ref"] != f["ref"] || e["revision"] != f["revision"] || !reflect.DeepEqual(e["query"], f["query"]) {
			t.Fatal("unchanged metadata must name the actual earlier delivery and exact article", from)
		}
		o := e["orientation"].(map[string]any)
		if o["summary"] != nil || o["conditions"] != nil || response["task"] != first["task"] || !reflect.DeepEqual(response["task_context"], first["task_context"]) {
			t.Fatal("reuse metadata while retaining current task and provenance")
		}
		original := f["orientation"].(map[string]any)
		for _, key := range []string{"unclassified", "counterexamples", "recheck", "needs_interpretation", "premise_changed", "omitted"} {
			if !reflect.DeepEqual(o[key], original[key]) {
				t.Fatal("unresolved material must remain visible", key)
			}
		}
		if response["reading"].(map[string]any)["mixed"] != true {
			t.Fatal("metadata reuse must not teach a reading-depth preference")
		}
		// Reconstruct only the fields declared reusable, independently of the producer.
		baseline := map[string]any{}
		for k, v := range e {
			baseline[k] = v
		}
		delete(baseline, "metadata_from")
		baseline["orientation"] = original
		for _, key := range []string{"title", "summary", "applies_to", "rationale"} {
			if value, exists := f[key]; exists {
				baseline[key] = value
			}
		}
		if workContextSize(e) >= workContextSize(baseline) {
			t.Fatal("references must save complete entry bytes")
		}
		t.Logf("entry UTF-8 JSON bytes: %d -> %d", workContextSize(baseline), workContextSize(e))
		wire, _ := json.Marshal(response)
		mcp := markdownMCPCall(t, "shikii_wiki", 200, wire)
		text := markdownMCPText(t, mcp)
		var restored any
		if strings.HasPrefix(text, "# Knowledge\n") {
			restored = restoreMarkdownMCP(t, text)
		} else {
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if err := decoder.Decode(&restored); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(restored, mcp["structuredContent"]) {
			t.Fatal("MCP text must preserve every delta field and recovery reference")
		}
	}
}

func TestWorkDeltaNeverReusesUndeliveredOrChangedMetadata(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"title": "Delta routing", "lesson": strings.Repeat("Retain the owner condition. ", 25), "body": strings.Repeat("Routing has a condition.\n\n", 200)})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check routing.", "query": "Delta routing", "max_bytes": 8192})
	if err != nil {
		t.Fatal(err)
	}
	full, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID + "@" + p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if full["knowledge"].([]any)[0].(map[string]any)["metadata_from"] != nil {
		t.Fatal("explicit recovery must supply complete metadata")
	}
	regressionChangeArticle(t, b, p, "updated")
	changed, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Delta routing"})
	if err != nil {
		t.Fatal(err)
	}
	e := changed["knowledge"].([]any)[0].(map[string]any)
	if e["metadata_from"] != nil || e["revision"] == p.Revision || e["orientation"].(map[string]any)["summary"] == nil {
		t.Fatal("changed metadata must be supplied")
	}
	fresh, err := workCall(t, b, "prepare", map[string]any{"request": "Another routing decision.", "query": "Delta routing"})
	if err != nil || fresh["knowledge"].([]any)[0].(map[string]any)["metadata_from"] != nil {
		t.Fatal("another receipt cannot inherit delivery state", err)
	}
}
