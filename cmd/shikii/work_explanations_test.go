package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkSharedExplanations(t *testing.T) {
	notice := "Exact passages of this revision, not a complete body. Check conditions, reasons and exceptions; read query only when a material condition remains unresolved. Excerpts do not establish support or permission."
	for _, count := range []int{1, 4} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			entries := []any{}
			for i := 0; i < count; i++ {
				entries = append(entries, map[string]any{"ref": "knowledge", "reading_notice": notice, "body_included": false, "text": "Exact source text, without shared substitutions.", "query": "lesson:0000000000000001@0000000000000002"})
			}
			reply := map[string]any{"knowledge": entries, "source": map[string]any{"reading_notice": notice}}
			before, _ := json.Marshal(reply)
			if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 65536); err != nil {
				t.Fatal(err)
			}
			if count == 1 {
				if reply["reading_notices"] != nil {
					t.Fatal("single notices must not gain indirection")
				}
				return
			}
			pool, ok := reply["reading_notices"].(map[string]any)
			if !ok || len(pool) != 1 || pool["notice1"] != notice {
				t.Fatal("identical host explanations must be shared verbatim", pool)
			}
			for _, raw := range entries {
				e := raw.(map[string]any)
				if e["reading_notice"] != "Read /reading_notices/notice1 (shared reading notice)." || e["body_included"] != false || e["text"] != "Exact source text, without shared substitutions." || e["query"] == nil {
					t.Fatal("keep exact evidence, incomplete status and recovery", e)
				}
			}
			after, _ := json.Marshal(reply)
			if len(after) >= len(before) || reply["source"].(map[string]any)["reading_notice"] != notice {
				t.Fatal("share only host instructions with a net byte reduction")
			}
			t.Logf("complete response UTF-8 JSON bytes: %d -> %d", len(before), len(after))
			mcp := markdownMCPCall(t, "shikii_wiki", http.StatusOK, after)
			text := markdownMCPText(t, mcp)
			var restored any
			if strings.HasPrefix(text, "# Knowledge\n") {
				restored = restoreMarkdownMCP(t, text)
			} else if err := json.Unmarshal([]byte(text), &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored, mcp["structuredContent"]) {
				t.Fatal("MCP text must retain the same shared notice and exact evidence")
			}
			// A second sizing pass must preserve the exact representation.
			if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 65536); err != nil {
				t.Fatal(err)
			}
			again, _ := json.Marshal(reply)
			if string(after) != string(again) {
				t.Fatal("sharing must be deterministic across sizing passes")
			}
		})
	}
	t.Run("sharing includes reading metadata cost", func(t *testing.T) {
		for _, length := range []int{128, 140, 144, 148, 160} {
			entries := []any{}
			for range 2 {
				entries = append(entries, map[string]any{"reading_notice": strings.Repeat("x", length), "body_included": false})
			}
			reading := &workReadingTrace{Bytes: workContextSize(entries)}
			w := &workState{Reasoning: &reasoningProgress{Reading: reading}}
			reply := map[string]any{"knowledge": entries, "reading": reading}
			before := workContextSize(reply)
			if _, err := limitWorkPreparation(notebook.Book{}, w, reply, map[string]string{}, 65536); err != nil {
				t.Fatal(err)
			}
			if after := workContextSize(reply); after > before {
				t.Fatalf("sharing must include its reading metadata cost: notice=%d, %d -> %d", length, before, after)
			}
			if reply["reading_notices"] == nil && reading.Mixed {
				t.Fatal("a rejected sharing candidate must not change the reading classification")
			}
		}
	})
	t.Run("transport cost and reading accounting", func(t *testing.T) {
		entries := []any{}
		for range 4 {
			entries = append(entries, map[string]any{"reading_notice": notice, "body_included": false})
		}
		reply := map[string]any{"knowledge": entries}
		before := suppliedPreparationBytes(reply)
		w := &workState{Reasoning: &reasoningProgress{Reading: &workReadingTrace{Bytes: before}}}
		quoted := map[string]string{"knowledge1": "Exact source quotation."}
		if _, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, 65536); err != nil {
			t.Fatal(err)
		}
		if w.Reasoning.Reading.Bytes != suppliedPreparationBytes(reply) || !w.Reasoning.Reading.Mixed || quoted["knowledge1"] != "Exact source quotation." {
			t.Fatal("measure shared delivery without changing quoted evidence or claiming reading quality")
		}
		// A transport-specific envelope can make sharing more expensive.
		size := func(v any) int {
			n := workContextSize(v)
			if v.(map[string]any)["reading_notices"] != nil {
				n += 4096
			}
			return n
		}
		if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, quoted, 65536, size); err != nil {
			t.Fatal(err)
		}
		if reply["reading_notices"] != nil {
			t.Fatal("do not share when the actual transport would grow")
		}
		for _, raw := range entries {
			if raw.(map[string]any)["reading_notice"] != notice {
				t.Fatal("restore every original notice when sharing costs more")
			}
		}
	})
	t.Run("bounded response has no orphan references", func(t *testing.T) {
		b, source := loopBook(t)
		for _, title := range []string{"Routing policy", "Routing exception", "Routing history"} {
			loopPage(t, b, source, map[string]any{"title": title, "body": strings.Repeat("Routing retains its original conditions and exceptions.\n\n", 220)})
		}
		v, err := workCall(t, b, "prepare", map[string]any{"request": "Check routing", "query": "routing", "max_bytes": 8192})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(v)
		if len(data) > 8192 {
			t.Fatal("bound the complete reply", len(data))
		}
		pool, _ := v["reading_notices"].(map[string]any)
		for _, raw := range v["knowledge"].([]any) {
			e := raw.(map[string]any)
			n, _ := e["reading_notice"].(string)
			if strings.HasPrefix(n, "Read /reading_notices/") {
				id := strings.TrimSuffix(strings.TrimPrefix(n, "Read /reading_notices/"), " (shared reading notice).")
				if pool[id] == nil {
					t.Fatal("budget changes must not leave dangling instructions", e, pool)
				}
			}
			if e["body_included"] != false || e["query"] == nil {
				t.Fatal("retain omitted-body recovery", e)
			}
		}
	})
}
