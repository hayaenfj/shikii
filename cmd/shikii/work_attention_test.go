package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkAttentionBoundsExcerptsAndRecoversFullRevision(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	start := reasoningInput("start", "current")
	start["depth"] = "brief"
	saveReasoning(t, b, p["receipt"], start, nil)
	o, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "CSV depth evidence", Content: "A synthetic owner condition was checked.", Reference: "depth-fixture", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("CSV owner condition remains relevant under this synthetic input.\n\n", 100)
	page, err := b.Ingest(notebook.Draft{Title: "CSV depth fixture", Lesson: "Check the synthetic owner condition.", Body: &body, AppliesTo: "CSV", Kind: "howto", Scene: "CSV", Sources: []string{o.ID}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "CSV depth"})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v["attention"].(map[string]any)
	if !ok || a["depth"] != "brief" {
		t.Fatalf("the current depth must select an explicit reading budget: %v", v)
	}
	for _, raw := range v["knowledge"].([]any) {
		k := loopObject(t, raw)
		if k["body_included"] == true {
			t.Fatal("a broad brief search must not duplicate whole article bodies")
		}
	}
	v, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.Page.ID + "@" + page.Page.Revision})
	if err != nil {
		t.Fatal(err)
	}
	article := loopObject(t, loopObject(t, v["knowledge"].([]any)[0])["article"])
	if article["body"] != page.Page.Body {
		t.Fatal("an exact pinned read must still recover all original text")
	}
	bad := reasoningInput("update", "current")
	bad["depth"] = "unknown-depth"
	if v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": bad}); err == nil && v["complete"] == true {
		t.Fatal("an unknown reading depth was accepted")
	}
}

func TestWorkAttentionBriefReflectionKeepsHumanEvidence(t *testing.T) {
	view := reflectView{Lessons: []notebook.Page{{ID: "1111111111111111", Revision: "2222222222222222", Title: "Synthetic supporting article", Lesson: "Check the owner condition.", Body: strings.Repeat("Earlier checked conditions.\n", 400)}}, Corrections: []any{map[string]any{"human_truth": "yes", "source_material": map[string]any{"reply": "Synthetic explicit human answer."}}}}
	full, err := preparedReflectionContext(view)
	if err != nil {
		t.Fatal(err)
	}
	brief, err := preparedReflectionContext(view, true)
	if err != nil {
		t.Fatal(err)
	}
	pages := brief["lessons"].([]any)
	page := loopObject(t, pages[0])
	if page["body"] != nil || page["incomplete"] != true || page["query"] == nil || workContextSize(brief) >= workContextSize(full) {
		t.Fatal("brief supporting knowledge must retain exact revision recovery without repeating the body")
	}
	corrections := brief["human_corrections"].([]any)
	if loopObject(t, loopObject(t, corrections[0])["source_material"])["reply"] != "Synthetic explicit human answer." {
		t.Fatal("human evidence must never be shortened by reading depth")
	}
}

func TestWorkAttentionDeepensUnresolvedJudgment(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	start := reasoningInput("start", "current")
	start["depth"], start["assessment"] = "brief", "unresolved"
	saveReasoning(t, b, p["receipt"], start, nil)
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "CSV"})
	if err != nil || loopObject(t, v["attention"])["depth"] != "deep" {
		t.Fatal("an unresolved decision must not stay hidden behind a brief reading policy", err)
	}
}

func TestWorkAttentionInitialBriefReadKeepsRequiredReview(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	page := reasoningPage(t, b, "CSV initial brief reading", "howto", strings.Repeat("CSV owner conditions require a focused check.\n\n", 100))
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": planningTestRequest})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV initial brief reading", "reasoning": map[string]any{"depth": "brief"}})
	if err != nil {
		t.Fatalf("initial preparation must accept a reading intent without fabricating a judgment: %v", err)
	}
	status := loopObject(t, v["reasoning"])
	if loopObject(t, v["attention"])["depth"] != "brief" || status["required"] != true || status["started"] != true {
		t.Fatal("a brief initial read must retain the required unsaved start")
	}
	found := false
	for _, raw := range v["knowledge"].([]any) {
		entry := loopObject(t, raw)
		if entry["id"] == page.ID {
			found = true
			if entry["body_included"] != false {
				t.Fatal("a brief first read must not supply a whole article")
			}
		}
	}
	if !found {
		t.Fatal("the relevant article must remain available with exact excerpts and its revision query")
	}
	state := loadSession(root, "reasoning")
	for _, read := range state.Planning.Knowledge.Read {
		if read.ID == page.ID {
			t.Fatal("an excerpt-only initial read must not be recorded as a supplied full body")
		}
	}
	for _, quoteable := range state.Planning.Sources {
		if strings.Contains(quoteable, page.Body) {
			t.Fatal("unreturned full text must not remain quoteable through the new receipt")
		}
	}
	saveReasoning(t, b, v["receipt"], reasoningInput("start", "current"), nil)
	saveReasoning(t, b, v["receipt"], reasoningInput("finish", "current"), nil)
}

func TestWorkAttentionStandaloneBriefHasRecoveryQuery(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p := reasoningPage(t, b, "CSV standalone brief recovery", "howto", strings.Repeat("CSV owner condition requires a complete source recovery.\n\n", 100))
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Check the CSV owner condition.", "query": "CSV standalone brief recovery", "reasoning": map[string]any{"depth": "brief"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range v["knowledge"].([]any) {
		k := loopObject(t, raw)
		if k["id"] != p.ID {
			continue
		}
		if k["body_included"] != false {
			t.Fatal("setup did not omit the body")
		}
		if k["query"] == nil {
			t.Fatal("standalone brief article has no exact revision recovery query")
		}
		return
	}
	t.Fatal("setup did not return the matching article")
}
