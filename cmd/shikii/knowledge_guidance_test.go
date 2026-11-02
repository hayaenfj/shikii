package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestGuidanceCurrentPassages(t *testing.T) {
	b, s := loopBook(t)
	body := "Earlier audit: Quartz exports are unresolved.\n\n" + strings.Repeat("Earlier unrelated details.\n\n", 130) + "Current check: Quartz exports preserve tenant filters."
	p := loopPage(t, b, s, map[string]any{"title": "Quartz export knowledge", "lesson": "Preserve Quartz tenant filters.", "applies_to": "Quartz exports", "body": body, "passages": []any{
		map[string]any{"role": "current", "quote": "Current check: Quartz exports preserve tenant filters.", "conditions": "Current Quartz exports", "reason": "The later verification supersedes the earlier unresolved audit under these conditions."},
		map[string]any{"role": "history", "quote": "Earlier audit: Quartz exports are unresolved.", "conditions": "The earlier audit", "reason": "This records the earlier state, not the current result."},
	}})
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Check Quartz exports", "query": "Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	entry := v["knowledge"].([]any)[0].(map[string]any)
	orientation, ok := entry["orientation"].(map[string]any)
	if !ok {
		t.Fatal("preparation must put interpreted current knowledge before the mixed chronology", entry)
	}
	current := orientation["current"].([]any)
	history := orientation["history"].([]any)
	if len(current) != 1 || len(history) != 1 || orientation["revision"] != p.Revision || orientation["needs_interpretation"] != true {
		t.Fatal("the unclassified remainder, current statement and earlier state must stay distinct", orientation)
	}
	for _, group := range [][]any{current, history} {
		e := group[0].(map[string]any)
		if body[int(e["start"].(float64)):int(e["end"].(float64))] != e["text"] {
			t.Fatal("a reading role must retain its exact body passage", e)
		}
	}
}

func guidanceFailedRun(t *testing.T, b notebook.Book, ref, receipt any, result string) map[string]any {
	t.Helper()
	v, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "observations": []any{map[string]any{"kind": "test", "title": "Quartz parser check", "content": result}}, "outcomes": []any{map[string]any{"ref": ref, "conditions": "Quartz exports on Linux", "action": "Use the raw parser for Quartz exports.", "failure": "The raw parser loses quoted fields.", "correction": "Use the structured parser and retain quoted fields.", "result": result, "sources": []string{"new1"}}}})
	if err != nil || v["complete"] != true {
		t.Fatal(v, err)
	}
	return v
}

func TestGuidanceCounterexampleBeyondTopicCandidates(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Legacy parser incident", "lesson": "Retain quoted fields.", "applies_to": "Legacy parser", "kind": "howto", "body": "Run the legacy parser."})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	guidanceFailedRun(t, b, ref, v["receipt"], "The structured parser preserved all quoted fields.")
	for _, title := range []string{"Quartz exports raw parser guide", "Quartz exports raw parser format", "Quartz exports raw parser checks", "Quartz exports raw parser setup"} {
		loopPage(t, b, s, map[string]any{"title": title, "lesson": "Use the raw parser for Quartz exports.", "applies_to": "Quartz exports raw parser", "body": "Quartz exports raw parser instructions."})
	}
	value, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "Check Quartz exports", "proposal": "Use the raw parser for Quartz exports."})
	if err != nil {
		t.Fatal(err)
	}
	view := loopObject(t, value)
	found := false
	for _, raw := range view["checks"].([]any) {
		check := raw.(map[string]any)
		if check["relation"] != "counterexample_candidate" {
			continue
		}
		found = true
		if check["page"].(map[string]any)["id"] != p.ID || len(check["outcomes"].([]any)) != 1 {
			t.Fatal("the independently retrieved failure must retain its procedure and evidence", check)
		}
	}
	if !found {
		t.Fatal("a relevant recorded failure must not be buried by ordinary topic candidates", view)
	}
}

func TestGuidanceOutcomeReopensProcedureReview(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz parser workflow", "lesson": "Check Quartz exports.", "applies_to": "Quartz exports", "kind": "howto", "body": "Use the raw parser for Quartz exports."})
	auditRound2SettleMaintenance(t, b)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz parser workflow", "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	saved := guidanceFailedRun(t, b, ref, v["receipt"], "The structured parser preserved all quoted fields.")
	reviews, ok := saved["procedure_reviews"].([]any)
	if !ok || len(reviews) != 1 {
		t.Fatal("recording a corrected failure must immediately offer a procedure review", saved)
	}
	var task notebook.MaintenanceTask
	raw, _ := json.Marshal(reviews[0])
	if err := json.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	if task.PageID != p.ID || !strings.Contains(string(raw), "outcome-feedback") || !strings.Contains(string(raw), "Use the structured parser") {
		t.Fatal("the review must retain the proposed correction and the source", string(raw))
	}
	if _, err := b.Maintain(notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Reason: "Checked the evidence; retain the earlier procedure for its separate conditions."}); err != nil {
		t.Fatal(err)
	}
	again := guidanceFailedRun(t, b, ref, v["receipt"], "The structured parser preserved all quoted fields.")
	if reviews, _ := again["procedure_reviews"].([]any); len(reviews) != 0 {
		t.Fatal("an unchanged reviewed outcome must not recur", again)
	}
	newEvidence := guidanceFailedRun(t, b, ref, v["receipt"], "A second checked input preserved nested quoted fields.")
	if len(newEvidence["procedure_reviews"].([]any)) != 1 {
		t.Fatal("new checked evidence must reopen the same procedure review", newEvidence)
	}
}
