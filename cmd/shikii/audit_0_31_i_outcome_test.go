package main

import (
	"strings"
	"testing"
)

// INVARIANT 10: "a result cannot cite the result itself or a report the same work wrote about itself ... the evidence of the plan
// and of the result of a trial cannot be a report either". A procedure outcome is another record that cites evidence and is
// cited in turn: workSourceTitles makes "Procedure execution with cited evidence" citable, and recordWorkOutcome takes the
// work's own result as the outcome's source (resolve only; no selfEvidence). A result with nothing observed is then complete
// through the outcome.
func TestAuditIAnOutcomeLaundersTheResult(t *testing.T) {
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Quartz export checks", "lesson": "Check Quartz exports with tenant filters.", "applies_to": "Quartz exports", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	first, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Validated the Quartz exports."}})
	if err != nil {
		t.Fatalf("precondition: the result with nothing observed is saved: %v", err)
	}
	t.Logf("result with nothing observed: complete=%v errors=%.200s", first["complete"], auditJSON(first["errors"]))
	outcome, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Run the Quartz export check.", "result": "The check passed.", "sources": []string{"result"}}}})
	raw := auditJSON(outcome)
	t.Logf("an outcome whose only source is the work's own result: err=%v complete=%v errors=%.300s", err, outcome["complete"], auditJSON(outcome["errors"]))
	if strings.Contains(raw, "wrote about") {
		return // refused as a report: the finding does not hold
	}
	id := ""
	items, _ := outcome["saved"].([]any)
	for _, item := range items {
		m := item.(map[string]any)
		if m["kind"] == "outcome" && m["error"] == nil {
			id, _ = m["id"].(string)
		}
	}
	t.Logf("outcome items: %.600s", auditJSON(items))
	if id == "" {
		t.Fatalf("precondition: the outcome's source id: %s", raw)
	}
	second, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Validated the Quartz exports; the procedure outcome confirms it.", "sufficient": true, "evidence": []string{id}}})
	t.Logf("a result citing that outcome (%s): err=%v complete=%v errors=%.300s", id, err, second["complete"], auditJSON(second["errors"]))
	if err == nil && second["complete"] == true {
		t.Errorf("INVARIANT 10: a result with nothing observed is complete and sufficient: its only evidence is a procedure outcome whose only source is the work's earlier result")
	}
}

// The same through a measured reading: "Measured reading execution" is citable, and the reading takes the work's own result as its
// evidence (prepareWorkReadingDetails resolves the references; no selfEvidence).
func TestAuditIAReadingLaundersTheResult(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Check recovery after a lost acknowledgement.", "")
	first, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the recovery after a lost acknowledgement."}})
	if err != nil {
		t.Fatalf("precondition: the result with nothing observed is saved: %v", err)
	}
	t.Logf("result with nothing observed: complete=%v errors=%.200s", first["complete"], auditJSON(first["errors"]))
	reading, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": map[string]any{"check": "Recover the exact source revision.", "sufficient": true, "evidence": []string{"result"}}})
	raw := auditJSON(reading)
	t.Logf("a reading whose only evidence is the work's own result: err=%v complete=%v errors=%.300s", err, reading["complete"], auditJSON(reading["errors"]))
	if strings.Contains(raw, "wrote about") {
		return
	}
	id := ""
	items, _ := reading["saved"].([]any)
	for _, item := range items {
		m := item.(map[string]any)
		if m["kind"] == "reading" && m["error"] == nil {
			id, _ = m["id"].(string)
		}
	}
	t.Logf("reading items: %.600s", auditJSON(items))
	if id == "" {
		// checked: the reading refuses the result as evidence (it needs a structured quality observation), so this way is closed
		t.Logf("the reading with the result as its evidence is not saved: %.300s", auditJSON(reading["errors"]))
		return
	}
	second, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the recovery; the measured reading confirms it.", "evidence": []string{id}}})
	t.Logf("a result citing that reading (%s): err=%v complete=%v errors=%.300s", id, err, second["complete"], auditJSON(second["errors"]))
	if err == nil && second["complete"] == true {
		t.Errorf("INVARIANT 10: a result with nothing observed is complete and sufficient: its only evidence is a measured reading whose only evidence is the work's earlier result")
	}
}
