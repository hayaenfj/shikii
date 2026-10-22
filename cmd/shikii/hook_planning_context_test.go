package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func planningWikiFixture(t *testing.T, root, scope string) notebook.Page {
	t.Helper()
	st := store.Store{Dir: root}
	source := store.Feedback{ID: store.NewTraceID(), Request: "CSV export columns and delimiter", Summary: "Proposed name and status columns with a comma delimiter.", Reply: "Use those columns and commas."}
	if err := st.AppendFeedback(scope, source); err != nil {
		t.Fatal(err)
	}
	result, err := (notebook.Book{Root: root, Scope: scope}).Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export columns and delimiter", Lesson: "CSV export uses name and status columns with commas; preserve existing filters.", AppliesTo: "CSV export implementation and tests"})
	if err != nil {
		t.Fatal(err)
	}
	return *result.Page
}

func planningSources(t *testing.T, root string) map[string]any {
	t.Helper()
	q, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV export columns and delimiter"})
	if err != nil {
		t.Fatal(err)
	}
	return q["source_material"].(map[string]any)
}

func TestPlanningKeepsGoalAndWikiAcrossSteering(t *testing.T) {
	repo, root, _ := planningFixture(t)
	page := planningWikiFixture(t, root, feedbackScope(repo))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Implement the agreed CSV export, preserving filters and the chosen columns."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "The CSV export approach is ready; I will preserve the filters."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Also cover the empty list."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Please continue."})
	sources := planningSources(t, root)
	wiki := "lesson:" + page.ID + ":" + page.Revision
	if !strings.Contains(fmt.Sprint(sources[wiki]), page.Lesson) {
		t.Fatal("planning lost the applicable wiki after a short reply or repeated-delivery suppression")
	}
	found := false
	for key, value := range sources {
		if strings.HasPrefix(key, "history:") && strings.Contains(fmt.Sprint(value), "chosen columns") {
			found = true
		}
	}
	if !found {
		t.Fatal("the original goal was lost after steering messages")
	}
	if sources["previous_assistant"] != nil {
		t.Fatal("a historical answer must not become the previous answer")
	}
	payload := map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV export columns and delimiter", "agreed": map[string]any{"summary": "Continue the export with the existing column and filter conditions.", "evidence": []map[string]string{{"source": "current_user", "quote": "Please continue."}, {"source": wiki, "quote": page.Lesson}}}, "open": map[string]any{"unknown": true}}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatal(err)
	}
	records, _ := (store.Store{Dir: root}).Records("needs-plan", 0)
	if len(records) != 1 || !strings.Contains(records[0].Inputs["agreed"], page.Lesson) {
		t.Fatal("the decision did not retain its wiki evidence")
	}
}

func TestPlanningWikiIsNotHumanAgreement(t *testing.T) {
	repo, root, calls := planningFixture(t)
	page := planningWikiFixture(t, root, feedbackScope(repo))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	sources := planningSources(t, root)
	key := "lesson:" + page.ID + ":" + page.Revision
	if sources[key] == nil {
		t.Fatal("wiki context missing")
	}
	payload := planningPayload(root)
	payload["query"] = "CSV export columns and delimiter"
	payload["agreed"] = map[string]any{"summary": "Approved by the wiki.", "evidence": []map[string]string{{"source": key, "quote": page.Lesson}}}
	before := calls.Load()
	if _, err := planningCall(t, "submit", payload); err == nil {
		t.Fatal("wiki alone became human agreement")
	}
	if calls.Load() != before {
		t.Fatal("unsupported agreement reached the model")
	}
}

func TestPlanningHistoryIsBoundedAndSessionScoped(t *testing.T) {
	repo, root, _ := planningFixture(t)
	for i := 0; i < 9; i++ {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": fmt.Sprintf("Unique goal number %d: %s", i, strings.Repeat("bounded context ", 100))})
	}
	sources := planningSources(t, root)
	n := 0
	for key, value := range sources {
		if strings.HasPrefix(key, "history:") {
			n++
			if len(fmt.Sprint(value)) > 1200 {
				t.Fatal("history field exceeds the bound")
			}
			if strings.Contains(fmt.Sprint(value), "Unique goal number 0:") {
				t.Fatal("unbounded history")
			}
		}
	}
	if n == 0 || n > 8 {
		t.Fatalf("expected bounded recent context, got %d fields", n)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "another-session", "cwd": repo, "prompt": "An unrelated document review."})
	q, err := planningCall(t, "query", map[string]string{"session_id": "another-session", "turn_id": loadSession(root, "another-session").TurnID})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(q), "Unique goal number") {
		t.Fatal("history crossed session boundaries")
	}
}

func TestPlanningWikiSearchIsExplicitAndChecksRevision(t *testing.T) {
	repo, root, calls := planningFixture(t)
	page := planningWikiFixture(t, root, feedbackScope(repo))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	id := loadSession(root, "planning").TurnID
	q, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": id})
	if err != nil {
		t.Fatal(err)
	}
	for key := range q["source_material"].(map[string]any) {
		if strings.HasPrefix(key, "lesson:") {
			t.Fatal("the program selected a topic without an agent query")
		}
	}
	key := "lesson:" + page.ID + ":" + page.Revision
	if planningSources(t, root)[key] == nil {
		t.Fatal("explicit search did not retrieve its source")
	}
	ids := []string{}
	for _, source := range page.Sources {
		ids = append(ids, source.ID)
	}
	_, err = (notebook.Book{Root: root, Scope: feedbackScope(repo)}).Ingest(notebook.Draft{ID: page.ID, Title: page.Title, Lesson: page.Lesson + " Use the revised conditions.", AppliesTo: page.AppliesTo, Sources: ids})
	if err != nil {
		t.Fatal(err)
	}
	payload := planningPayload(root)
	payload["query"] = "CSV export columns and delimiter"
	payload["agreed"] = map[string]any{"summary": "Use the earlier evidence.", "evidence": []map[string]string{{"source": "current_user", "quote": "agreed CSV columns"}, {"source": key, "quote": page.Lesson}}}
	before := calls.Load()
	if _, err := planningCall(t, "submit", payload); err == nil {
		t.Fatal("a changed wiki revision was accepted without re-reading")
	}
	if calls.Load() != before {
		t.Fatal("a stale citation reached the model")
	}
}
