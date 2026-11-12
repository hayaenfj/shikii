package main

import (
	"encoding/json"
	"github.com/hayaenfj/shikii/internal/notebook"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCollectiveHumanMergePreviewPreservesBothWriters(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"body": "First paragraph.\n\nSecond paragraph."})
	current := loopPage(t, b, source, map[string]any{"id": p.ID, "body": "Peer's first paragraph.\n\nSecond paragraph."})
	change := notebook.Change{ID: p.ID, Revision: p.Revision, Action: "reconcile", Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Body: "First paragraph.\n\nMy second paragraph.", Kind: p.Kind, Scene: p.Scene}
	raw, _ := json.Marshal(change)
	s := &server{o: serveOpts{storeDir: b.Root, projectScope: b.Scope}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/wiki/article", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	s.handleArticle(w, req)
	var plan notebook.Reconciliation
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &plan) != nil || plan.Status != "ready" || plan.Result.Body != "Peer's first paragraph.\n\nMy second paragraph." {
		t.Fatal(w.Code, w.Body.String())
	}
	live, _ := b.Query("lesson:" + p.ID)
	if live.Lessons[0].Revision != current.Revision {
		t.Fatal("preview wrote the article")
	}
}

func TestCollectiveReconcilePreservesLearningAndRejectsCompetingResults(t *testing.T) {
	b, source := loopBook(t)
	q := func(conclusion string) *notebook.Learning {
		return &notebook.Learning{Probe: &notebook.KnowledgeProbe{Question: "Does Quartz work?", Method: "Check a synthetic case.", ReconsiderWhen: "The input changes.", Results: []notebook.ProbeResult{{Conditions: "Synthetic input", Conclusion: conclusion, Assessment: "inconclusive", Sources: []string{source}}}}}
	}
	p := loopPage(t, b, source, map[string]any{"body": "Original text.", "learning": q("Original result.")})
	current := loopPage(t, b, source, map[string]any{"id": p.ID, "learning": q("Peer result.")})
	intent := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{source}, ChangeReason: "Improve the text while preserving peer observations.", Edits: []notebook.TextEdit{{Field: "body", Before: p.Body, After: "Improved text."}}}
	v, e := b.Reconcile(intent)
	if e != nil || v.Status != "ready" || v.Result.Learning.Probe.Results[0].Conclusion != "Peer result." || v.Current.Revision != current.Revision {
		t.Fatal(v, e)
	}
	intent.Learning = q("My competing result.")
	v, e = b.Reconcile(intent)
	if e != nil || v.Status != "conflict" || v.Patch != nil || len(v.Conflicts) != 1 || v.Conflicts[0].Field != "learning" {
		t.Fatal("structured peer results must not be discarded", v, e)
	}
}

func TestCollectiveOperationsThroughCLIAndMCP(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, e := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic Quartz check", Content: "A checked synthetic result.", Reference: "test", Version: "1", ObservedAt: time.Unix(1, 0)})
	if e != nil {
		t.Fatal(e)
	}
	p := loopPage(t, b, source.ID, map[string]any{"body": "Original condition."})
	patch := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{source.ID}, ChangeReason: "Check transport of a read-only proposal.", Edits: []notebook.TextEdit{{Field: "body", Before: p.Body, After: "Revised condition."}}}
	raw, _ := json.Marshal(map[string]any{"patch": patch})
	v := notebookCommand(t, "reconcile", string(raw))
	if v["status"] != "ready" {
		t.Fatal(v)
	}
	h := newServer(serveOpts{storeDir: root, projectScope: b.Scope})
	raw, _ = json.Marshal(map[string]any{"operation": "reconcile", "patch": patch})
	code, data, e := callTool(h, "shikii_wiki", raw)
	if e != nil || code != 200 || !strings.Contains(string(data), `"status": "ready"`) {
		t.Fatal(code, string(data), e)
	}
	code, _, e = callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"reconcile","patch":{"confirmed":true}}`))
	if e != nil || code != 400 {
		t.Fatal("unknown nested fields must be refused", code, e)
	}
	live, _ := b.Query("lesson:" + p.ID)
	if live.Lessons[0].Revision != p.Revision {
		t.Fatal("transport preview saved a page")
	}
}

func TestCollectiveMergeRefusesAmbiguousTextAndRetiredArticles(t *testing.T) {
	for _, retired := range []bool{false, true} {
		t.Run(strconv.FormatBool(retired), func(t *testing.T) {
			b, source := loopBook(t)
			p := loopPage(t, b, source, map[string]any{"body": "Repeated.\n\nRepeated.\n\nLast."})
			loopPage(t, b, source, map[string]any{"id": p.ID, "body": "Peer changed the first.\n\nRepeated.\n\nLast."})
			if retired {
				regressionChangeArticle(t, b, p, "replaced")
			}
			v, e := b.Reconcile(notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{source}, ChangeReason: "Check ambiguous concurrent input.", Edits: []notebook.TextEdit{{Field: "body", Before: "Last.", After: "My last paragraph."}}})
			if e != nil || v.Patch != nil || v.Status != "conflict" && v.Status != "unavailable" {
				t.Fatal(v, e)
			}
		})
	}
}

func TestCollectiveReconcilePreservesConcurrentEdits(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"body": "First condition.\n\nSecond condition.\n", "rationale": "Keep both independent reasons."})
	current := loopPage(t, b, source, map[string]any{"id": p.ID, "body": "Changed first condition.\n\nSecond condition.\n"})
	patch := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{source}, ChangeReason: "Improve the second condition.", Edits: []notebook.TextEdit{{Field: "body", Before: "Second condition.", After: "Changed second condition."}}}
	v, err := runNotebook(b.Root, b.Scope, notebookRequest{Operation: "reconcile", Patch: &patch})
	if err != nil {
		t.Fatal(err)
	}
	plan := loopObject(t, v)
	if plan["status"] != "ready" {
		t.Fatal(plan)
	}
	live, _ := b.Query("lesson:" + p.ID)
	if live.Lessons[0].Revision != current.Revision {
		t.Fatal("the merge preview must save nothing")
	}
	raw, _ := json.Marshal(plan["patch"])
	var ready notebook.Patch
	if err = json.Unmarshal(raw, &ready); err != nil {
		t.Fatal(err)
	}
	r, err := b.Patch(ready)
	if err != nil || r.Page.Body != "Changed first condition.\n\nChanged second condition." || r.Page.Rationale != p.Rationale {
		t.Fatal(r, err)
	}
	if _, err = b.Patch(ready); err == nil {
		t.Fatal("a stale save cannot overwrite a later revision")
	}
}

func TestCollectiveReconcileExplainsConflicts(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"body": "Keep the original condition."})
	current := loopPage(t, b, source, map[string]any{"id": p.ID, "body": "Keep the first writer's condition."})
	patch := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{source}, ChangeReason: "A different condition.", Edits: []notebook.TextEdit{{Field: "body", Before: p.Body, After: "Keep the second writer's condition."}}}
	v, err := runNotebook(b.Root, b.Scope, notebookRequest{Operation: "reconcile", Patch: &patch})
	if err != nil {
		t.Fatal(err)
	}
	plan := loopObject(t, v)
	if plan["status"] != "conflict" || plan["patch"] != nil || len(plan["conflicts"].([]any)) == 0 {
		t.Fatal("overlapping edits must retain both alternatives", plan)
	}
	live, _ := b.Query("lesson:" + p.ID)
	if live.Lessons[0].Revision != current.Revision {
		t.Fatal("conflict reporting must not change knowledge")
	}
}
