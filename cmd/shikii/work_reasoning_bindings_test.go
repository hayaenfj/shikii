package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"testing"
	"time"
)

func interimAddArticle(t *testing.T, b notebook.Book, title, body string) notebook.Page {
	t.Helper()
	source, e := b.Capture(notebook.Observation{Kind: "test", Title: title + " evidence", Content: "A bounded synthetic condition.", Reference: "synthetic", Version: "interim", ObservedAt: time.Now()})
	if e != nil {
		t.Fatal("source fixture failed")
	}
	result, e := b.Ingest(notebook.Draft{Title: title, Lesson: title, Body: &body, AppliesTo: title, Kind: "howto", Scene: "synthetic review", Sources: []string{source.ID}})
	if e != nil {
		t.Fatal("article fixture failed")
	}
	return *result.Page
}

func interimArticleRef(t *testing.T, b notebook.Book, receipt any, id string) string {
	t.Helper()
	v, e := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "query": "lesson:" + id})
	if e != nil {
		t.Fatal("read fixture failed")
	}
	for _, raw := range v["knowledge"].([]any) {
		k := raw.(map[string]any)
		if k["id"] == id {
			return k["ref"].(string)
		}
	}
	t.Fatal("article missing")
	return ""
}

func TestReasoningRelatedBindingsAcrossPrepare(t *testing.T) {
	for _, renew := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-preparation", true: "new-preparation"}[renew], func(t *testing.T) {
			_, root, b, p := reasoningFixture(t)
			a := interimAddArticle(t, b, "Quartz owner procedure A", "Check the owner condition.")
			other := interimAddArticle(t, b, "Display palette B", "Check the display color.")
			ar := interimArticleRef(t, b, p["receipt"], a.ID)
			saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"title": "Quartz owner procedure refinement", "lesson": "Check the Quartz owner condition before export.", "applies_to": "Quartz owner procedure", "kind": "howto", "scene": "synthetic review", "sources": []string{"current"}, "related": []string{ar}}}})
			if e != nil || first["complete"] != false {
				t.Fatal("pending fixture failed")
			}
			pending, ok := first["pending"].([]any)
			if !ok || len(pending) != 1 {
				t.Fatal("pending placement fixture missing")
			}
			ref := pending[0].(map[string]any)["ref"]
			receipt := p["receipt"]
			if renew {
				next, e := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "lesson:" + other.ID})
				if e != nil {
					t.Fatal("reprepare failed")
				}
				receipt = next["receipt"]
				for r := range loopObject(t, next["reasoning"])["pending_notes"].(map[string]any) {
					ref = r
					break
				}
			}
			saved, e := workCall(t, b, "record", map[string]any{"receipt": receipt, "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
			if e != nil {
				t.Fatal("ref recovery failed")
			}
			if saved["complete"] == false {
				pending, ok := saved["pending"].([]any)
				if !ok || len(pending) != 1 {
					t.Fatal("ref recovery could not place original draft")
				}
				ref = pending[0].(map[string]any)["ref"]
				saved, e = workCall(t, b, "record", map[string]any{"receipt": receipt, "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
			}
			if e != nil || saved["complete"] != true {
				t.Fatal("pending resolution failed")
			}
			pages, e := b.Live()
			if e != nil {
				t.Fatal("read published draft failed")
			}
			for _, page := range pages {
				if page.Title == "Quartz owner procedure refinement" {
					wanted, wrong := false, false
					for _, id := range page.Related {
						wanted = wanted || id == a.ID
						wrong = wrong || id == other.ID
					}
					t.Logf("renew=%t original_relation=%t unrelated_relation=%t", renew, wanted, wrong)
					if !wanted || wrong {
						t.Error("required-note recovery rebound a supplied article alias")
					}
					return
				}
			}
			t.Fatal("published draft missing")
		})
	}
}

func TestReasoningNestedSourceRecovery(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	interimAddArticle(t, b, "Quartz owner procedure", "Check ownership.")
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Original probe check", "content": "The owner condition was checked."}}, "notes": []any{map[string]any{"title": "Quartz owner procedure refinement", "lesson": "Check the Quartz owner condition before export.", "applies_to": "Quartz owner procedure", "kind": "howto", "scene": "synthetic review", "sources": []string{"new1"}, "learning": map[string]any{"probe": map[string]any{"question": "Does the owner condition hold?", "method": "Compare a bounded synthetic input.", "reconsider_when": "The input changes.", "results": []any{map[string]any{"conditions": "Synthetic input", "assessment": "supports", "conclusion": "The owner condition held.", "sources": []string{"new1"}}}}}}}})
	if e != nil || first["complete"] != false || len(first["pending"].([]any)) != 1 {
		t.Fatal("probe pending fixture failed")
	}
	next, e := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "Quartz"})
	if e != nil {
		t.Fatal("probe reprepare failed")
	}
	required := ""
	for ref := range loopObject(t, next["reasoning"])["pending_notes"].(map[string]any) {
		required = ref
		break
	}
	recovered, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{map[string]any{"ref": required, "target": "new"}}})
	if e != nil {
		t.Fatal("probe ref recovery failed")
	}
	if recovered["complete"] == false {
		pending, ok := recovered["pending"].([]any)
		if !ok || len(pending) != 1 {
			for _, raw := range recovered["errors"].([]any) {
				t.Logf("item_error=%v", raw.(map[string]any)["error"])
			}
			t.Log("ref-only probe recovery returned an item error instead of the original draft")
			t.Fail()
			return
		}
		recovered, e = workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{map[string]any{"ref": pending[0].(map[string]any)["ref"], "target": "new"}}})
	}
	if e != nil || recovered["complete"] != true {
		t.Error("original nested source binding did not survive preparation")
	}
}
