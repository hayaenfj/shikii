package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkPreparationBudgetKeepsRecoveryAndConditions(t *testing.T) {
	b, source := loopBook(t)
	for _, title := range []string{"Routing policy", "Routing exception", "Routing history"} {
		loopPage(t, b, source, map[string]any{"title": title, "body": strings.Repeat("Routing stays within the configured tenant boundary.\n\n", 220)})
	}
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Check routing", "query": "routing", "max_bytes": 8192})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if len(raw) > 8192 {
		t.Fatal("honor the caller's complete response budget", len(raw))
	}
	if v["receipt"] == "" || v["output"] == nil {
		t.Fatal("retain the receipt and an explicit omission/recovery boundary", v)
	}
	for _, raw := range v["knowledge"].([]any) {
		e := raw.(map[string]any)
		if e["query"] == nil || e["body_included"] != false || e["reading_notice"] == "" {
			t.Fatal("omitted original text needs its exact revision and incomplete status", e)
		}
	}
	t.Run("bounded original recovery", func(t *testing.T) {
		body := strings.Repeat("<routing condition>", 2048)
		o, err := b.Capture(notebook.Observation{Kind: "test", Title: "Long original", Content: body, Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		cursor, recovered := "", ""
		for page := 0; page < 64; page++ {
			r, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": o.ID, "cursor": cursor, "max_bytes": 8192})
			if err != nil {
				t.Fatal("a small budget must still permit original recovery", err)
			}
			raw, _ := json.Marshal(r)
			if len(raw) > 8192 {
				t.Fatal("bound every original part", len(raw))
			}
			source := r["source"].(map[string]any)
			if part, ok := source["observation_part"].(map[string]any); ok && int(part["from"].(float64)) != len(recovered) {
				t.Fatal("each part must advance the same original", part)
			}
			recovered += source["observations"].([]any)[0].(map[string]any)["content"].(string)
			next, _ := source["next_cursor"].(string)
			if next == "" {
				if recovered != body {
					t.Fatal("the bounded parts must recover the exact original")
				}
				return
			}
			if next == cursor {
				t.Fatal("a cursor must progress")
			}
			cursor = next
		}
		t.Fatal("bounded recovery did not finish")
	})
}
