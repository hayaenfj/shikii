package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func guidancePassages(quote string) []any {
	return []any{map[string]any{"role": "current", "quote": quote, "conditions": "Quartz exports", "reason": "The cited check established this behavior for these conditions."}}
}

func TestGuidancePremiseChangesRemainVisible(t *testing.T) {
	b, s := loopBook(t)
	path := filepath.Join(filepath.Dir(b.Root), "workflow.txt")
	if err := os.WriteFile(path, []byte("Earlier behavior"), 0600); err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, s, map[string]any{"title": "Quartz knowledge", "applies_to": "Quartz", "body": "Current Quartz workflow.", "watch": []string{"workflow.txt"}, "passages": guidancePassages("Current Quartz workflow.")})
	if err := os.WriteFile(path, []byte("Changed behavior"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Read Quartz"})
	if err != nil {
		t.Fatal(err)
	}
	e := v["knowledge"].([]any)[0].(map[string]any)
	if e["orientation"].(map[string]any)["premise_changed"] != true {
		t.Fatal("a current interpretation cannot conceal changed premises", e)
	}
}

func TestGuidanceCounterexampleRejectsChangedEvidence(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz parser workflow", "applies_to": "Quartz exports", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	saved := guidanceFailedRun(t, b, ref, v["receipt"], "The structured parser preserved all quoted fields.")
	id := saved["saved"].([]any)[0].(map[string]any)["id"].(string)
	paths, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", id+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	if err := json.Unmarshal(raw, &changed); err != nil {
		t.Fatal(err)
	}
	changed["content"] = "A changed but structurally valid observation."
	data, _ := json.Marshal(changed)
	if err := os.WriteFile(paths[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "Check Quartz exports", "proposal": "Use the raw parser for Quartz exports."}); err == nil {
		t.Fatal("a counterexample must refuse changed supporting evidence")
	}
	// The maintenance list goes on without the changed evidence: the report is named as unreadable with the source to
	// recover, nothing of it is shown as if it had been checked, and its procedure cannot be marked as reviewed.
	tasks, err := b.Maintenance()
	if err != nil {
		t.Fatal("changed supporting evidence must not take the whole maintenance list down", err)
	}
	listed, _ := json.Marshal(tasks)
	var review *notebook.MaintenanceTask
	for i := range tasks {
		if tasks[i].PageID == p.ID {
			review = &tasks[i]
		}
	}
	if review == nil || len(review.Outcomes) != 1 || !strings.Contains(string(listed), "cannot be read") || !strings.Contains(string(listed), id+" (changed)") {
		t.Fatal("the procedure review must name the report whose evidence changed, and that source", string(listed))
	}
	for _, unchecked := range []string{"A changed but structurally valid observation.", "The structured parser preserved all quoted fields.", "The raw parser loses quoted fields.", "Use the structured parser"} {
		if strings.Contains(string(listed), unchecked) {
			t.Fatal("a procedure review must not present changed supporting evidence, or the report it belongs to", unchecked, string(listed))
		}
	}
	if _, err := b.Maintain(notebook.MaintenanceReview{PageID: review.PageID, Revision: review.Revision, Key: review.Key, Reason: "Checked the evidence."}); err == nil {
		t.Fatal("a procedure review must refuse changed supporting evidence")
	}
}

func TestGuidanceCounterexampleFindsOlderMatchingFailure(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Legacy field parser", "applies_to": "Field parsing", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	guidanceFailedRun(t, b, ref, v["receipt"], "The structured parser preserved all quoted fields.")
	for _, result := range []string{"First backup result.", "Second backup result.", "Third backup result."} {
		saved, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Backup check", "content": result}}, "outcomes": []any{map[string]any{"ref": ref, "conditions": "Offline archives", "action": "Copy offline archives.", "failure": "The backup destination was unavailable.", "correction": "Use the local archive destination.", "result": result, "sources": []string{"new1"}}}})
		if err != nil || saved["complete"] != true {
			t.Fatal(saved, err)
		}
	}
	value, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "Check Quartz exports", "proposal": "Use the raw parser for Quartz exports."})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range loopObject(t, value)["checks"].([]any) {
		check := raw.(map[string]any)
		outcomes, _ := check["outcomes"].([]any)
		for _, outcome := range outcomes {
			found = found || outcome.(map[string]any)["failure"] == "The raw parser loses quoted fields."
		}
	}
	if !found {
		t.Fatal("new unrelated failures must not hide an older matching counterexample", value)
	}
}

func TestGuidancePassageIntegrityAndAtomicEdits(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"body": "Current Quartz workflow. Earlier audit.", "passages": guidancePassages("Current Quartz workflow.")})
	patch := map[string]any{"id": p.ID, "revision": p.Revision, "sources": []string{s}, "change_reason": "Update the supported condition.", "edits": []any{map[string]any{"field": "body", "before": "Current Quartz workflow.", "after": "Updated Quartz workflow."}}}
	if _, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch}); err == nil {
		t.Fatal("body edits cannot leave an interpreted quote pointing to absent text")
	}
	for _, bad := range []any{
		guidancePassages("missing"),
		[]any{map[string]any{"role": "truth", "quote": "Current Quartz workflow.", "conditions": "Quartz", "reason": "Unsupported truth role."}},
		[]any{map[string]any{"role": "current", "quote": "Current Quartz workflow.", "conditions": "", "reason": "Missing conditions."}},
		append(guidancePassages("Current Quartz workflow."), guidancePassages("Quartz workflow.")...),
	} {
		patch["edits"], patch["passages"] = nil, bad
		if _, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch}); err == nil {
			t.Fatal("invalid interpretations must be rejected atomically", bad)
		}
		v, err := b.Query("lesson:" + p.ID)
		if err != nil || v.Lessons[0].Revision != p.Revision {
			t.Fatal("refusal changed the article", v, err)
		}
	}
	patch["edits"] = []any{map[string]any{"field": "body", "before": "Current Quartz workflow.", "after": "Updated Quartz workflow."}}
	patch["passages"] = guidancePassages("Updated Quartz workflow.")
	v, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	page := loopObject(t, v)["page"].(map[string]any)
	if page["body"] != "Updated Quartz workflow. Earlier audit." || len(page["passages"].([]any)) != 1 {
		t.Fatal(page)
	}
	if _, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch}); err == nil {
		t.Fatal("old interpretations cannot overwrite a later revision")
	}
	delete(patch, "edits")
	if _, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch}); err == nil {
		t.Fatal("a passage-only edit still requires the exact read revision")
	}
}

func TestGuidanceWorkPassageOnlyPatchAndPreservation(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz knowledge", "applies_to": "Quartz", "body": "Current Quartz workflow. Earlier audit."})
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Read Quartz", "query": "Quartz"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	saved, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"target": ref, "sources": []string{"current"}, "passages": guidancePassages("Current Quartz workflow."), "change_reason": "Interpret the checked current condition."}}})
	// Standalone requests are not human evidence; use a captured observation.
	if err != nil {
		t.Fatal(err)
	}
	if saved["complete"] == true {
		t.Fatal("standalone context must not invent human evidence", saved)
	}
	payload := map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Quartz check", "content": "The current workflow was checked."}}, "notes": []any{map[string]any{"target": ref, "sources": []string{"new1"}, "passages": guidancePassages("Current Quartz workflow."), "change_reason": "Interpret the checked current condition."}}}
	saved, err = workCall(t, b, "record", payload)
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	current, err := b.Query("lesson:" + p.ID)
	if err != nil {
		t.Fatal(err)
	}
	page := current.Lessons[0]
	if page.Body != p.Body || len(loopObject(t, page)["passages"].([]any)) != 1 || len(page.Sources) != len(p.Sources)+1 {
		t.Fatal("passage-only work edits must preserve text and prior evidence", page)
	}
	updated := loopPage(t, b, s, map[string]any{"id": page.ID, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo, "rationale": "Additional context."})
	if len(loopObject(t, updated)["passages"].([]any)) != 1 {
		t.Fatal("omitting passages must preserve the earlier interpretation")
	}
	cleared := loopPage(t, b, s, map[string]any{"id": page.ID, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo, "passages": []any{}})
	if _, ok := loopObject(t, cleared)["passages"]; ok {
		t.Fatal("an explicit empty array must clear the interpretation")
	}
}

func TestGuidanceArticleHTTPInterpretedEdit(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"body": "Current Quartz workflow.", "passages": guidancePassages("Current Quartz workflow.")})
	h := newServer(serveOpts{storeDir: b.Root, projectScope: b.Scope})
	data, _ := json.Marshal(map[string]any{"id": p.ID, "revision": p.Revision, "action": "edit", "title": p.Title, "lesson": p.Lesson, "applies_to": p.AppliesTo, "body": "Updated Quartz workflow.", "kind": p.Kind, "scene": p.Scene, "passages": guidancePassages("Updated Quartz workflow.")})
	r := httptest.NewRequest("POST", "/v1/wiki/article", strings.NewReader(string(data)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Updated Quartz workflow.") {
		t.Fatal("the article editor must be able to revise classified text together with its interpretation", w.Code, w.Body.String())
	}
}
