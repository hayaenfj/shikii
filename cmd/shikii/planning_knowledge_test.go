package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestPlanningReadsFullArticleAndPinsEvidenceVersion(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p := planningWikiFixture(t, root, feedbackScope(repo))
	body := "CSV exports retain the existing filter even when the list is empty."
	d := notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson, Kind: "rule", Scene: "テストの時", AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}, Body: &body}
	r, err := book.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	p = *r.Page
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	payload["pages"] = []string{p.ID}
	q, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"], "query": payload["query"], "pages": payload["pages"]})
	if err != nil {
		t.Fatal("the needed body can be chosen and passed to the decision", err)
	}
	key := "article:" + p.ID + ":" + p.Revision
	if !strings.Contains(q["source_material"].(map[string]any)[key].(string), body) {
		t.Fatal("passes the raw text and revision of the chosen body")
	}
	payload["agreed"] = map[string]any{"summary": "Keep filters for empty export results.", "evidence": []map[string]string{{"source": "current_user", "quote": "keep the existing filters"}, {"source": key, "quote": body}}}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatal(err)
	}
	records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	data, _ := json.Marshal(records[0])
	var record map[string]any
	_ = json.Unmarshal(data, &record)
	context, ok := record["knowledge_context"].(map[string]any)
	if !ok || len(context["used"].([]any)) != 1 || len(context["available"].([]any)) != 1 {
		t.Fatal("keeps the candidates, the actual quotes and the revisions available at the time separately", string(data))
	}
	stats, err := (store.Store{Dir: root}).KnowledgeReport()
	if err != nil || stats.Prepared != 1 || stats.WithMatches != 1 || stats.WithCitations != 1 || stats.Citations != 1 || stats.ContextBytes < len(body) {
		t.Fatal("search candidates and quotes are counted apart from the effect assessment", stats, err)
	}
	newBody := "CSV exports have a revised behavior."
	d.Body = &newBody
	if _, err := book.Ingest(d); err != nil {
		t.Fatal(err)
	}
	old, err := book.Query("lesson:" + p.ID + "@" + p.Revision)
	if err != nil || len(old.Lessons) != 1 || old.Lessons[0].Body != body {
		t.Fatal("the assessment can go back to the body at the time, without mixing in later knowledge", old, err)
	}
}

func TestPlanningRejectsArticleChangesDuringDecision(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	model := fakeAgentModel(t, func(string) float64 { close(entered); <-release; return .9 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	p := planningWikiFixture(t, root, feedbackScope(repo))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	payload["agreed"] = map[string]any{"summary": "Keep the agreed CSV conditions.", "evidence": []map[string]string{{"source": "current_user", "quote": "agreed CSV columns"}, {"source": "lesson:" + p.ID + ":" + p.Revision, "quote": p.Lesson}}}
	done := make(chan error, 1)
	go func() { _, err := planningCall(t, "submit", payload); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("cannot start the decision")
	}
	d := notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson + " Recheck the delimiter.", Kind: "rule", Scene: "テストの時", AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}}
	_, editErr := (notebook.Book{Root: root, Scope: feedbackScope(repo)}).Ingest(d)
	close(release)
	err := <-done
	if editErr != nil {
		t.Fatal(editErr)
	}
	if err == nil {
		t.Fatal("detects a change to an article quoted during the decision")
	}
	if records, _ := (store.Store{Dir: root}).Records("needs-plan", 0); len(records) != 0 {
		t.Fatal("a decision on stale evidence is not saved")
	}
}

// 0.8.1: the page's "records of knowledge used in decisions" stayed at 0 (agents were not giving search words). The agent still chooses the topic,
// but a query without search words returns "not searched yet", and the hook text becomes the step "search by adding topic words to query".
func TestPlanningTellsTheAgentWhenTheWikiWasNotSearched(t *testing.T) {
	repo, root, _ := planningFixture(t)
	planningWikiFixture(t, root, feedbackScope(repo))
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if ctx := feedbackContext(t, out); !strings.Contains(ctx, "topic words in query") || strings.Contains(ctx, "必要なwikiは") {
		t.Fatalf("the hook asks for it as the step of searching the wiki: %s", ctx)
	}
	payload := planningPayload(root)
	q, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"]})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(q["context_notice"]), "The wiki has not been searched yet") || q["wiki_context"] != nil {
		t.Fatalf("without search words it does not search, and says it has not searched: %v %v", q["context_notice"], q["wiki_context"])
	}
	q, err = planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"], "query": "CSV export columns"})
	if err != nil || q["wiki_context"] == nil || strings.Contains(fmt.Sprint(q["context_notice"]), "not been searched yet") {
		t.Fatalf("with search words it searches: %v %v", q, err)
	}
}

// 0.8.1: even when the decision model is busy or down, sorting out the plan is saved and returns success (the ritual check does not block it). The decision is skipped.
func TestPlanningSubmitSavesTheContextEvenWhenTheModelIsDown(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	hookLocalURL = "http://127.0.0.1:1" // the decision model cannot be reached
	res, err := planningCall(t, "submit", planningPayload(root))
	if err != nil {
		t.Fatalf("saves the sorted plan even when the model cannot be reached: %v", err)
	}
	if res["recorded"] == true || !strings.Contains(fmt.Sprint(res["reason"]), "the decision model could not be reached") {
		t.Fatalf("says the decision was skipped: %v", res)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}); strings.Contains(out, "plan has not been sorted out") {
		t.Fatalf("the plan was saved, so the ritual check does not block: %s", out)
	}
}
