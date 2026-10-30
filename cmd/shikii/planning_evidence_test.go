package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"strings"
	"testing"
	"time"
)

func TestPlanningRejectsRelatedSupersessionDuringDecision(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	model := fakeAgentModel(t, func(string) float64 { close(entered); <-release; return .9 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	goal := reasoningPage(t, book, "Purpose", "criteria", "Keep the old workflow condition.")
	reasoningPage(t, book, "CSV export workflow", "howto", "Export procedure.", goal.ID)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	payload["agreed"] = map[string]any{"summary": "Use the condition.", "evidence": []map[string]string{{"source": "current_user", "quote": "keep the existing filters"}, {"source": "article:" + goal.ID + ":" + goal.Revision, "quote": goal.Body}}}
	done := make(chan error, 1)
	go func() { _, err := planningCall(t, "submit", payload); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("decision did not enter")
	}
	_, editErr := book.Ingest(notebook.Draft{Title: "Replacement purpose", Lesson: "Use a new workflow condition", Kind: "criteria", Scene: "planning", AppliesTo: "planning", Sources: []string{goal.Sources[0].ID}, Replaces: []string{goal.ID}})
	close(release)
	submitErr := <-done
	if editErr != nil {
		t.Fatal(editErr)
	}
	live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == goal.ID })
	if err != nil || len(live) != 0 {
		t.Fatal("fixture failed to supersede goal", err)
	}
	records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
	if err != nil {
		t.Fatal(err)
	}
	if submitErr == nil || len(records) != 0 {
		t.Fatalf("superseded related criterion was accepted: submit_error=%v decision_records=%d", submitErr, len(records))
	}
}

func TestPlanningDoesNotCountAnUndeliveredBodyAsRead(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	goal := reasoningPage(t, book, "Purpose", "criteria", strings.Repeat("x", 8001))
	reasoningPage(t, book, "CSV export workflow", "howto", "", goal.ID)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	query, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"], "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	sources := query["source_material"].(map[string]any)
	for key := range sources {
		if strings.HasPrefix(key, "article:") {
			t.Fatal("query unexpectedly supplied a body")
		}
	}
	body := "Short new condition never delivered by query."
	changed, err := book.Ingest(notebook.Draft{ID: goal.ID, Title: goal.Title, Lesson: goal.Lesson, AppliesTo: goal.AppliesTo, Sources: []string{goal.Sources[0].ID}, Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	response, err := planningCall(t, "submit", payload)
	if err != nil {
		return
	}
	if response["source_material"] != nil {
		t.Fatal("submit unexpectedly supplies source material")
	}
	records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
	if err != nil || len(records) != 1 {
		t.Fatal("missing decision", err)
	}
	for _, ref := range records[0].KnowledgeContext.Read {
		if ref.ID == changed.Page.ID && ref.Revision == changed.Page.Revision {
			t.Fatalf("never-delivered article revision counted as read: query_article_count=0 recorded_read_count=%d", len(records[0].KnowledgeContext.Read))
		}
	}
}
