package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func reasoningPage(t *testing.T, book notebook.Book, title, kind, body string, related ...string) notebook.Page {
	t.Helper()
	source := store.Feedback{ID: store.NewTraceID(), Request: title, Summary: "Synthetic evidence for " + title, Reply: "Keep this as a working hypothesis."}
	if err := (store.Store{Dir: book.Root}).AppendFeedback(book.Scope, source); err != nil {
		t.Fatal(err)
	}
	r, err := book.Ingest(notebook.Draft{Title: title, Lesson: title, Kind: kind, Scene: "planning", AppliesTo: title, Sources: []string{source.ID}, Body: &body, Related: related})
	if err != nil {
		t.Fatal(err)
	}
	return *r.Page
}

func TestPlanningReasoningBringsLinkedCriteriaAndWholeBodies(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	goal := reasoningPage(t, book, "Reduce repeated manual work", "criteria", "Prefer changes that remove repeated work. This does not authorize publication.")
	page := reasoningPage(t, book, "CSV export workflow", "howto", "Preserve the filters because the exported rows must match what was reviewed.", goal.ID)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	q, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"], "query": payload["query"]})
	if err != nil {
		t.Fatal(err)
	}
	sources := q["source_material"].(map[string]any)
	for _, p := range []notebook.Page{page, goal} {
		if !strings.Contains(fmt.Sprint(sources["article:"+p.ID+":"+p.Revision]), p.Body) {
			t.Errorf("the first query must include the whole reasoning body of %s", p.Title)
		}
		if !strings.Contains(fmt.Sprint(sources["lesson:"+p.ID+":"+p.Revision]), p.Kind) {
			t.Errorf("the kind must remain visible for %s", p.Title)
		}
	}
	k := q["wiki_context"].(map[string]any)
	if k["available"] != nil || k["available_count"] != float64(2) {
		t.Errorf("the reply should count the inventory without repeating it: %v", k)
	}
	key := "article:" + goal.ID + ":" + goal.Revision
	payload["agreed"] = map[string]any{"summary": "Keep the requested filters and the related project purpose in view.", "evidence": []map[string]string{{"source": "current_user", "quote": "keep the existing filters"}, {"source": key, "quote": goal.Body}}}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatal("related criteria must be citable without a second query", err)
	}
	recs, err := (store.Store{Dir: root}).Records("needs-plan", 0)
	if err != nil || len(recs) != 1 {
		t.Fatal(recs, err)
	}
	data, _ := json.Marshal(recs[0].KnowledgeContext)
	var saved map[string]any
	_ = json.Unmarshal(data, &saved)
	if len(saved["available"].([]any)) != 2 || len(saved["candidates"].([]any)) != 1 || len(saved["read"].([]any)) != 2 || len(saved["used"].([]any)) != 1 || saved["related"] == nil {
		t.Fatal("keeps search, links, reading, citations and the full inventory apart", string(data))
	}
}

func TestPlanningReasoningKeepsConditionsAndDoesNotReadPartialBodies(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p := planningWikiFixture(t, root, book.Scope)
	lesson := strings.Repeat("CSV export conditions. ", 45) + "EXCEPT the archived workflow."
	condition := strings.Repeat("CSV export scope; ", 24) + "ONLY after the person selects the columns."
	body := strings.Repeat("x", 8001)
	r, err := book.Ingest(notebook.Draft{ID: p.ID, Title: p.Title, Lesson: lesson, Kind: p.Kind, Scene: p.Scene, AppliesTo: condition, Sources: []string{p.Sources[0].ID}, Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	p = *r.Page
	sources := map[string]string{}
	_, k, err := preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	summary := sources["lesson:"+p.ID+":"+p.Revision]
	if !strings.Contains(summary, lesson) || !strings.Contains(summary, condition) {
		t.Fatal("the exception or applicability condition was cut off")
	}
	if len(k.Read) != 0 || sources["article:"+p.ID+":"+p.Revision] != "" {
		t.Fatal("oversized bodies must be omitted, not partly read")
	}
	_, k, err = preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export", Pages: []string{p.ID}}, sources)
	if err != nil || len(k.Read) != 1 || !strings.Contains(sources["article:"+p.ID+":"+p.Revision], body) {
		t.Fatal("explicit reading still retrieves the complete long body", err)
	}
}

func TestPlanningReasoningRelatedCriteriaStayOneHopAndLive(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	remote := reasoningPage(t, book, "Remote purpose", "criteria", "Only reached through a second link.")
	goal := reasoningPage(t, book, "Manual work", "criteria", "First-hop purpose.", remote.ID)
	unrelated := reasoningPage(t, book, "Unrelated purpose", "criteria", "Never reached.")
	howto := reasoningPage(t, book, "Other procedure", "howto", "A link is not automatically a judgment criterion.")
	page := reasoningPage(t, book, "CSV export workflow", "howto", "Export reasoning.", goal.ID, howto.ID)
	sources := map[string]string{}
	_, _, err := preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if sources["lesson:"+goal.ID+":"+goal.Revision] == "" {
		t.Fatal("linked criterion missing")
	}
	for _, p := range []notebook.Page{remote, unrelated, howto} {
		if sources["lesson:"+p.ID+":"+p.Revision] != "" {
			t.Fatalf("unrelated or second-hop page leaked: %s", p.Title)
		}
	}
	_, err = book.Ingest(notebook.Draft{Title: "Replacement purpose", Lesson: "Replacement purpose", Kind: "criteria", Scene: "planning", AppliesTo: "other", Sources: []string{goal.Sources[0].ID}, Replaces: []string{goal.ID}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export", Pages: []string{page.ID}}, sources)
	if err != nil || sources["lesson:"+goal.ID+":"+goal.Revision] != "" {
		t.Fatal("a superseded linked criterion was delivered", err)
	}
}

func TestPlanningReasoningBudgetsPreferWholeCriteria(t *testing.T) {
	for _, sizes := range [][2]int{{4000, 4000}, {4001, 4000}, {8001, 8000}} {
		t.Run(fmt.Sprint(sizes), func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			a := reasoningPage(t, book, "First purpose", "criteria", strings.Repeat("a", sizes[0]))
			b := reasoningPage(t, book, "Second purpose", "criteria", strings.Repeat("b", sizes[1]))
			page := reasoningPage(t, book, "CSV export workflow", "howto", "procedure", a.ID, b.ID)
			_, k, err := preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export"}, map[string]string{})
			if err != nil {
				t.Fatal(err)
			}
			if len(k.Related) != 2 || len(k.Read) > 2 {
				t.Fatal("related and body counts are bounded", k)
			}
			bodies := map[string]string{a.ID: a.Body, b.ID: b.Body, page.ID: page.Body}
			total := 0
			for _, ref := range k.Read {
				total += len(bodies[ref.ID])
			}
			if total > 8000 {
				t.Fatal("automatic bodies exceed 8000 bytes", total)
			}
			// Related is kept in the article's link order; only those two may be read automatically.
			for _, ref := range k.Read {
				if ref.ID != page.ID && ref.ID != k.Related[0].ID && ref.ID != k.Related[1].ID {
					t.Fatal("read a criterion that was not selected", ref)
				}
			}
			if sizes == [2]int{4000, 4000} {
				if len(k.Read) != 2 || k.Read[0].ID == page.ID || k.Read[1].ID == page.ID {
					t.Fatal("criteria should use the exact 8000-byte budget before procedures", k.Read)
				}
			}
			c := reasoningPage(t, book, "Third purpose", "criteria", "not an extra slot")
			if _, err := book.Ingest(notebook.Draft{ID: page.ID, Title: page.Title, Lesson: page.Lesson, AppliesTo: page.AppliesTo, Sources: []string{page.Sources[0].ID}, Related: []string{a.ID, b.ID, c.ID}}); err != nil {
				t.Fatal(err)
			}
			_, k, err = preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export"}, map[string]string{})
			if err != nil || len(k.Related) != 2 {
				t.Fatal("three links must not expand the two-criterion limit", err)
			}
		})
	}
}

func TestPlanningReasoningRejectsStaleLinkedEvidence(t *testing.T) {
	repo, root, calls := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	goal := reasoningPage(t, book, "Purpose", "criteria", "The old condition.")
	reasoningPage(t, book, "CSV export workflow", "howto", "Export procedure.", goal.ID)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	key := "article:" + goal.ID + ":" + goal.Revision
	payload["agreed"] = map[string]any{"summary": "Use the condition.", "evidence": []map[string]string{{"source": "current_user", "quote": "keep the existing filters"}, {"source": key, "quote": goal.Body}}}
	newBody := "The new condition."
	if _, err := book.Ingest(notebook.Draft{ID: goal.ID, Title: goal.Title, Lesson: goal.Lesson, AppliesTo: goal.AppliesTo, Sources: []string{goal.Sources[0].ID}, Body: &newBody}); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if _, err := planningCall(t, "submit", payload); err == nil || calls.Load() != before {
		t.Fatal("stale linked evidence reached the decision model", err)
	}
	// Removing a captured source must also exclude the current linked page.
	removed := false
	err := filepath.WalkDir(filepath.Join(root, "records", "wiki-sources"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == goal.Sources[0].ID+".json" {
			removed = true
			return os.Remove(path)
		}
		return nil
	})
	if err != nil || !removed {
		t.Fatal("failed to remove the synthetic source", err)
	}
	sources := map[string]string{}
	_, k, err := preparePlanningKnowledge(root, book.Scope, planningRequest{Query: "CSV export"}, sources)
	if err != nil || len(k.Related) != 0 || strings.Contains(fmt.Sprint(sources), "The new condition.") {
		t.Fatal("a linked page without an intact source was delivered", err)
	}
}
