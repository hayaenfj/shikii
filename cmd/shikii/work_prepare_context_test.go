package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkPrepareCorrectionWithoutPendingScores(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	reviewCall(t, h, map[string]any{"operation": "submit", "source_id": id, "trace_id": trace, "truth": "yes", "reason": "AUDIT_HUMAN_CORRECTION_SYNTHETIC"})
	capture(t, func() error { return cmdLabel([]string{"--store", st.Dir, "--source", "audit", trace, "no"}) })
	direct, err := reviewQuery(st, scope)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(direct["human_corrections"])
	if !strings.Contains(string(expected), "AUDIT_HUMAN_CORRECTION_SYNTHETIC") {
		t.Fatal("synthetic setup failed")
	}
	b := notebook.Book{Root: st.Dir, Scope: scope}
	got, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	pending := got["pending"].([]any)
	if len(pending) != 1 || pending[0].(map[string]any)["wiki_pending"] != true {
		t.Fatal("expected unsorted conversation")
	}
	data, _ := json.Marshal(got["reflection_context"])
	if !strings.Contains(string(data), "AUDIT_HUMAN_CORRECTION_SYNTHETIC") {
		t.Fatal("human scoring correction was lost although wiki sorting is pending and direct review query retains it")
	}
}

func TestWorkPrepareStandaloneKnowledgeShortage(t *testing.T) {
	b, s := loopBook(t)
	for i := 0; i < 10; i++ {
		loopPage(t, b, s, map[string]any{"title": fmt.Sprintf("Quartz workflow %d", i), "lesson": "Quartz workflow knowledge", "applies_to": "quartz workflow", "body": strings.Repeat("x", 16000)})
	}
	raw, e := b.QueryPage("quartz workflow", "")
	if e != nil {
		t.Fatal(e)
	}
	got, e := workCall(t, b, "prepare", map[string]any{"query": "quartz workflow", "request": "Check the quartz workflow"})
	if e != nil {
		t.Fatal(e)
	}
	context, ok := got["knowledge_context"].(map[string]any)
	if !ok {
		t.Fatal("ordinary query metadata must be retained", got)
	}
	search := context["search"].(map[string]any)
	if int(search["total"].(float64)) != raw.Search.Total || len(search["candidates"].([]any)) != len(raw.Search.Candidates) {
		t.Fatal(search, raw.Search)
	}
	if context["lookup"] == nil {
		t.Fatal("the ordinary query lookup receipt must be retained", context)
	}
	supplied := map[string]bool{}
	for _, entry := range got["knowledge"].([]any) {
		article := entry.(map[string]any)["article"].(map[string]any)
		supplied[article["id"].(string)] = true
	}
	readOmitted := false
	for _, entry := range search["candidates"].([]any) {
		candidate := entry.(map[string]any)
		if supplied[candidate["id"].(string)] {
			continue
		}
		query := candidate["query"].(map[string]any)
		value, e := improvementCall(t, b, query)
		if e != nil {
			t.Fatal(e)
		}
		page := loopObject(t, value)["lessons"].([]any)[0].(map[string]any)
		if page["id"] != candidate["id"] || page["revision"] != candidate["revision"] {
			t.Fatal("an omitted article query must retain the supplied revision", candidate, page)
		}
		readOmitted = true
		break
	}
	if !readOmitted {
		t.Fatal("expected an omitted body among the candidates")
	}

}

func TestWorkPrepareStandaloneClassificationShortage(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Legacy quartz workflow"})
	if e := b.RewriteWithoutKindScene(p.ID); e != nil {
		t.Fatal(e)
	}
	raw, e := b.QueryPage("quartz", "")
	if e != nil {
		t.Fatal(e)
	}
	got, e := workCall(t, b, "prepare", map[string]any{"query": "quartz", "request": "Check quartz workflow"})
	if e != nil {
		t.Fatal(e)
	}
	if raw.UnclassifiedCount != 1 {
		t.Fatal("legacy fixture failed")
	}
	context, ok := got["knowledge_context"].(map[string]any)
	if !ok {
		t.Fatal(got)
	}
	if context["unclassified_count"] != float64(1) || !strings.Contains(context["unclassified_how"].(string), "wiki classify") {
		t.Fatal("ordinary prepare must retain classification guidance", context)
	}
	pages := context["unclassified"].([]any)
	if len(pages) != 1 || pages[0].(map[string]any)["id"] != p.ID {
		t.Fatal("classification must name the actual page", pages)
	}

}

func TestWorkPrepareStandaloneKnowledgeContinuation(t *testing.T) {
	b, source := loopBook(t)
	for i := 0; i < 50; i++ {
		loopPage(t, b, source, map[string]any{"title": fmt.Sprintf("Quartz continuation %02d %s", i, strings.Repeat("t", 140)), "lesson": "Quartz workflow knowledge", "applies_to": "quartz workflow " + strings.Repeat("a", 970), "body": strings.Repeat("x", 16000)})
	}
	raw, err := b.QueryPage("quartz workflow", "")
	if err != nil || raw.NextCursor == "" {
		t.Fatal("fixture must require ordinary query pagination", raw.NextCursor, err)
	}
	got, err := workCall(t, b, "prepare", map[string]any{"query": "quartz workflow", "request": "Read the quartz procedures"})
	if err != nil {
		t.Fatal(err)
	}
	context := got["knowledge_context"].(map[string]any)
	if context["next_cursor"] != raw.NextCursor || workContextSize(context) > workSourceBudget {
		t.Fatal("knowledge context must retain the bounded page and continuation", context)
	}
	query := got["knowledge_query"].(map[string]any)
	if query["query"] != "quartz workflow" || query["cursor"] != raw.NextCursor || query["operation"] != "query" {
		t.Fatal("remaining candidates need a usable query", query)
	}
	value, err := improvementCall(t, b, query)
	if err != nil {
		t.Fatal(err)
	}
	next := loopObject(t, value)["search"].(map[string]any)
	if next["total"] != float64(raw.Search.Total) || len(next["candidates"].([]any)) == 0 {
		t.Fatal("the supplied continuation must recover the remaining search candidates", next)
	}
	firstID := raw.Search.Candidates[0].ID
	for _, candidate := range next["candidates"].([]any) {
		if candidate.(map[string]any)["id"] == firstID {
			t.Fatal("the query continuation must advance rather than restart")
		}
	}
}
