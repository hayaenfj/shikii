package main

import "testing"

func TestGuidanceUpdatedProcedureReturnsCurrentReview(t *testing.T) {
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Quartz workflow", "applies_to": "Quartz exports", "kind": "howto", "body": "Use the raw parser for Quartz exports."})
	auditRound2SettleMaintenance(t, b)
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz workflow", "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := prepared["knowledge"].([]any)[0].(map[string]any)["ref"]
	first := guidanceFailedRun(t, b, ref, prepared["receipt"], "The structured parser retained all quoted fields.")
	old := first["procedure_reviews"].([]any)[0].(map[string]any)
	patched, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "notes": []any{map[string]any{"target": ref, "sources": []string{"observed1"}, "change_reason": "Use the checked correction for quoted inputs.", "edits": []any{map[string]any{"field": "body", "before": "Use the raw parser for Quartz exports.", "after": "Use the structured parser for Quartz exports."}}}}})
	if err != nil || patched["complete"] != true {
		t.Fatal(patched, err)
	}
	reviews, ok := patched["procedure_reviews"].([]any)
	if !ok || len(reviews) != 1 {
		t.Fatal("a supported update must return its current review for explicit completion", patched)
	}
	current := reviews[0].(map[string]any)
	if current["ref"] == old["ref"] || current["revision"] == old["revision"] {
		t.Fatal("the current review must not reuse a stale revision or reference", current, old)
	}
	stale, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": []any{map[string]any{"ref": old["ref"], "reason": "Do not accept an old review after editing."}}})
	if err != nil || stale["complete"] != false {
		t.Fatal("the old review remains invalid", stale, err)
	}
	settled, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": []any{map[string]any{"ref": current["ref"], "reason": "Checked the updated procedure against the cited result; the correction is now included."}}})
	if err != nil || settled["complete"] != true {
		t.Fatal(settled, err)
	}
	again := guidanceFailedRun(t, b, ref, prepared["receipt"], "The structured parser retained all quoted fields.")
	if reviews, _ := again["procedure_reviews"].([]any); len(reviews) != 0 {
		t.Fatal("explicitly completed evidence must not recur", again)
	}
}
