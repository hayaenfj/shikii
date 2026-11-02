package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestAuditGrowthDocsInitialPreparationBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, s := loopBook(t)
	learning := map[string]any{"probe": map[string]any{"question": strings.Repeat("q", 500), "method": strings.Repeat("m", 900), "reconsider_when": strings.Repeat("r", 500), "results": []any{map[string]any{"conditions": strings.Repeat("c", 500), "conclusion": strings.Repeat("x", 900), "assessment": "inconclusive", "sources": []string{s}}}}}
	p := loopPage(t, b, s, map[string]any{"title": "Synthetic growth hypothesis", "body": "Keep the synthetic condition.", "learning": learning})
	_ = p
	raw, _ := json.Marshal(learning)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Synthetic growth hypothesis", "request": "Inspect synthetic learning"})
	if err != nil {
		t.Fatal(err)
	}
	e := v["knowledge"].([]any)[0].(map[string]any)
	context := e["learning_context"].(map[string]any)
	if context["omitted"] != true {
		t.Fatal("fixture must exceed inline budget", context)
	}
	article := e["article"].(map[string]any)
	t.Logf("learning JSON bytes=%d, learning_context.omitted=%v, full learning present in article=%v", len(raw), context["omitted"], article["learning"] != nil)
	if article["learning"] != nil {
		t.Fatal("an omitted learning context must not duplicate the full metadata in the same preparation")
	}
	full, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + p.ID + "@" + p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	entry := full["knowledge"].([]any)[0].(map[string]any)
	complete := entry["learning_context"].(map[string]any)
	if complete["learning"] == nil || complete["omitted"] == true || entry["article"].(map[string]any)["learning"] != nil {
		t.Fatal("an explicit pinned read must recover the full learning once", full)
	}
}

func TestLearningPreparationRechecksConnectedChanges(t *testing.T) {
	for _, mode := range []string{"revision", "deletion", "replacement", "watched premise"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			b, s := loopBook(t)
			path := filepath.Join(b.Scope, "policy.txt")
			if err := os.WriteFile(path, []byte("original policy"), 0600); err != nil {
				t.Fatal(err)
			}
			a := loopPage(t, b, s, map[string]any{"title": "Connected policy", "body": "Keep the original policy condition.", "watch": []string{"policy.txt"}})
			other := loopPage(t, b, s, map[string]any{"title": "Connected exception", "body": "Keep the exception's separate condition."})
			p := loopPage(t, b, s, map[string]any{"title": "Rechecked project insight", "learning": map[string]any{"connections": learningConnections(a, other)}})
			prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check project insight"})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "revision":
				loopPage(t, b, s, map[string]any{"id": a.ID, "title": a.Title, "body": "The policy condition changed."})
			case "deletion":
				if _, err := b.Manage(notebook.Change{Action: "delete", ID: a.ID, Revision: a.Revision}); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				loopPage(t, b, s, map[string]any{"title": "Replacement policy", "body": "Use the revised policy.", "replaces": []string{a.ID}})
			case "watched premise":
				if err := os.WriteFile(path, []byte("changed policy"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			current, err := workCall(t, b, "prepare", map[string]any{"receipt": prepared["receipt"], "query": "lesson:" + p.ID})
			if err != nil {
				t.Fatal(err)
			}
			entry := current["knowledge"].([]any)[0].(map[string]any)
			context, ok := entry["learning_context"].(map[string]any)
			if !ok || len(context["recheck"].([]any)) != 1 || context["query"] != "lesson:"+p.ID+"@"+p.Revision {
				t.Fatal("every connected change must remain an explicit recheck", mode, entry)
			}
		})
	}
}

func TestAuditGrowthDocsExtendedPreparationContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, s := loopBook(t)
	a := loopPage(t, b, s, map[string]any{"title": "First synthetic experience", "body": "Keep the first source condition."})
	other := loopPage(t, b, s, map[string]any{"title": "Second synthetic experience", "body": "Keep the other source condition."})
	p := loopPage(t, b, s, map[string]any{"title": "Synthetic growth hypothesis", "body": "Keep the synthetic condition.", "learning": map[string]any{"connections": learningConnections(a, other)}})
	loopPage(t, b, s, map[string]any{"id": a.ID, "title": a.Title, "body": "The first source condition changed."})
	first, err := workCall(t, b, "prepare", map[string]any{"query": "Synthetic growth hypothesis", "request": "Inspect synthetic learning"})
	if err != nil {
		t.Fatal(err)
	}
	initial := first["knowledge"].([]any)[0].(map[string]any)["learning_context"].(map[string]any)
	if len(initial["recheck"].([]any)) != 1 {
		t.Fatal("changed connected experience must be visible in initial context", initial)
	}
	_ = p
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Synthetic growth hypothesis"})
	if err != nil {
		t.Fatal(err)
	}
	e := next["knowledge"].([]any)[0].(map[string]any)
	t.Logf("extended preparation learning_context present=%v, article present=%v", e["learning_context"] != nil, e["article"] != nil)
	if e["learning_context"] == nil {
		t.Fatal("same-receipt query preparation needs the documented learning context and rechecks")
	}
}
