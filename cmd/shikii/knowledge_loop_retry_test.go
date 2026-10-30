package main

import (
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanningReflectionRetryAfterWikiUpdate(t *testing.T) {
	repo, root, calls := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic audit source", Content: "Keep existing CSV filters; no additional choice needed.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, s.ID, map[string]any{"title": "CSV filters", "lesson": "Keep existing CSV filters.", "applies_to": "CSV filters"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	_, err = planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	payload := planningPayload(root)
	payload["query"] = "CSV filters"
	payload["open"] = map[string]any{"summary": "Use the existing policy.", "evidence": []map[string]string{{"source": "lesson:" + p.ID + ":" + p.Revision, "quote": "Keep existing CSV filters."}}}
	payload["reflection"] = map[string]any{"wiki": []any{map[string]any{"id": p.ID, "sources": []string{s.ID}, "title": p.Title, "lesson": "Keep existing CSV filters and column names.", "applies_to": p.AppliesTo}, map[string]any{"sources": []string{"ffffffffffffffff"}}}}
	first, err := planningCall(t, "submit", payload)
	if err != nil {
		t.Fatal(err)
	}
	if first["reflection"].(map[string]any)["complete"] != false {
		t.Fatal("synthetic partial failure missing")
	}
	before := calls.Load()
	payload["reflection"] = map[string]any{"wiki": []any{map[string]any{"sources": []string{s.ID}}}}
	again, err := planningCall(t, "submit", payload)
	if err != nil {
		t.Fatalf("correction retry rejected after own wiki update; extra model calls=%d; error=%v", calls.Load()-before, err)
	}
	if again["reflection"].(map[string]any)["complete"] != true || calls.Load() != before {
		t.Fatal("retry must finish reflection without a new planning decision")
	}
}

func TestPlanningExplicitArticleKeepsReasonAndPremiseWarning(t *testing.T) {
	b, source := loopBook(t)
	path := filepath.Join(b.Scope, "policy.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	const why = "The local store avoids an extra approval form."
	p := loopPage(t, b, source, map[string]any{"rationale": why, "watch": []string{"policy.txt"}, "body": "Read this policy before work."})
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{"current_user": "Review this article"}
	_, k, err := preparePlanningKnowledge(b.Root, b.Scope, planningRequest{Pages: []string{p.ID}}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Read) != 1 {
		t.Fatal("article not read")
	}
	article := sources["article:"+p.ID+":"+p.Revision]
	if !strings.Contains(article, why) || !strings.Contains(article, "Recheck") {
		t.Fatalf("explicit article omitted rationale=%v or changed-premise warning=%v", !strings.Contains(article, why), !strings.Contains(article, "Recheck"))
	}
}

func TestSearchJournalBoundsEscapedText(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, nil)
	for i := 0; i < 64; i++ {
		query := fmt.Sprintf("workflow design %03d", i)
		view, err := runNotebook(b.Root, b.Scope, notebookRequest{Operation: "query", Query: query})
		if err != nil {
			paths, _ := filepath.Glob(filepath.Join(b.Root, "records", "wiki-searches", "*", ".searches.json"))
			var size int64
			for _, path := range paths {
				if st, e := os.Stat(path); e == nil {
					size = st.Size()
				}
			}
			_, recallErr := b.Recall("workflow")
			t.Fatalf("valid lookup %d failed: %v; saved_bytes=%d; recall_error=%v", i+1, err, size, recallErr)
		}
		lookup := view.(notebook.View).Lookup
		if lookup == nil {
			t.Fatalf("lookup %d returned no receipt", i+1)
		}
		d := notebook.Discovery{LookupID: lookup.ID, PageID: p.ID, Revision: p.Revision, Context: strings.Repeat("<\x01", 500), Decision: strings.Repeat("<\x01", 500), Evidence: strings.Repeat("<\x01", 500)}
		res, err := runReflect(b.Root, b.Scope, reflectRequest{Operation: "submit", Discoveries: []notebook.Discovery{d}})
		if err != nil || !res.(reflectResult).Complete {
			t.Fatalf("discovery %d failed: %v / %v", i+1, res, err)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(b.Root, "records", "wiki-searches", "*", ".searches.json"))
	if len(paths) != 1 {
		t.Fatal("missing journal")
	}
	st, err := os.Stat(paths[0])
	if err != nil || st.Size() > 1<<20 {
		t.Fatal("journal exceeded its read budget")
	}
	if _, err := b.Recall("workflow"); err != nil {
		t.Fatal(err)
	}
}

func TestArticleEditKeepsChangedPremiseWarning(t *testing.T) {
	b, source := loopBook(t)
	path := filepath.Join(b.Scope, "policy.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, source, map[string]any{"watch": []string{"policy.txt"}})
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := b.Query("lesson:" + p.ID)
	if err != nil || len(before.Lessons[0].Recheck) != 1 {
		t.Fatal("missing initial warning", err)
	}
	edited, err := b.Manage(notebook.Change{ID: p.ID, Revision: p.Revision, Action: "edit", Title: "Changed title", Lesson: p.Lesson, AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: p.Scene})
	if err != nil {
		t.Fatal(err)
	}
	after, err := b.Query("lesson:" + p.ID)
	if err != nil || len(after.Lessons[0].Recheck) != 1 {
		t.Fatal("premise state actually changed", err)
	}
	if len(edited.Recheck) != 1 {
		t.Fatal("article save reply drops changed-premise warning despite unchanged watch baseline")
	}
}
