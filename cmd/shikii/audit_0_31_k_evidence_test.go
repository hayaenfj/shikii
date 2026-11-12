package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"strings"
	"testing"
)

// Audit K of 0.31.0 (sixth round): the ways a work's own report still reaches the evidence of its next result.

// auditKResultID records a result with nothing observed and returns the id of its source (the report).
func auditKResultID(t *testing.T, receipt any, record func(map[string]any) (map[string]any, error)) string {
	t.Helper()
	first, err := record(map[string]any{"receipt": receipt, "result": map[string]any{"changed": "Validated the Quartz exports."}})
	if err != nil {
		t.Fatalf("precondition: the result with nothing observed is saved: %v", err)
	}
	for _, item := range append(asList(first["saved"]), asList(first["errors"])...) {
		if m, _ := item.(map[string]any); m["kind"] == "result" {
			if id, _ := m["id"].(string); id != "" {
				return id
			}
		}
	}
	t.Fatalf("precondition: the id of the result: %.400s", auditJSON(first))
	return ""
}

func asList(v any) []any { l, _ := v.([]any); return l }

// INVARIANT 10: "a result cannot cite ... a report the same work wrote about itself". The fix of audit I put selfEvidence before the
// sources of a procedure outcome. A page is the other thing a result cites ("a page you read"): wiki ingest takes the work's own
// result as a page's only source, the receipt reads that page (prepare again with query lesson:<id>), and the next result cites it.
// The result observed nothing, and it is complete and sufficient on a page whose only source is its own earlier report.
func TestAuditKAPageLaundersTheResult(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) (map[string]any, error) { return workCall(t, b, "record", fields) }
	id := auditKResultID(t, v["receipt"], record)
	ingested, err := improvementCall(t, b, map[string]any{"operation": "ingest", "sources": []string{id}, "title": "Quartz exports are valid", "lesson": "The Quartz exports were validated.", "applies_to": "Quartz exports", "kind": "fact", "scene": "when exporting from Quartz"})
	if err != nil {
		t.Fatalf("wiki ingest of a page whose only source is the work's own result: %v", err)
	}
	page := loopObject(t, ingested)["page"]
	pageID, _ := loopObject(t, page)["id"].(string)
	if pageID == "" {
		t.Fatalf("precondition: the page is saved: %.300s", auditJSON(ingested))
	}
	again, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + pageID})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, k := range asList(again["knowledge"]) {
		if m, _ := k.(map[string]any); m["id"] == pageID {
			ref, _ = m["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("precondition: the page is read with the receipt: %.400s", auditJSON(again["knowledge"]))
	}
	second, err := record(map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Validated the Quartz exports; the wiki confirms it.", "sufficient": true, "evidence": []string{ref}}})
	t.Logf("result 1 = %s (the report); page %s from it, read as %s; result 2 citing %s: err=%v complete=%v errors=%.300s", id, pageID, ref, ref, err, second["complete"], auditJSON(second["errors"]))
	if err == nil && second["complete"] == true {
		t.Errorf("INVARIANT 10: a result with nothing observed is complete and sufficient: its only evidence is a page whose only source is the work's earlier result")
	}
}

// The same with the note of a work record (the receipt's own way to write a page): notes take "result" as a source.
func TestAuditKANoteLaundersTheResult(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) (map[string]any, error) { return workCall(t, b, "record", fields) }
	id := auditKResultID(t, v["receipt"], record)
	noted, err := record(map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"target": "new", "sources": []string{"result"}, "title": "Quartz exports are valid", "lesson": "The Quartz exports were validated.", "applies_to": "Quartz exports", "kind": "fact", "scene": "when exporting from Quartz"}}})
	t.Logf("a note whose only source is the result: err=%v complete=%v saved=%.300s errors=%.300s", err, noted["complete"], auditJSON(noted["saved"]), auditJSON(noted["errors"]))
	if err != nil || strings.Contains(auditJSON(noted["errors"]), "wrote about") {
		return // refused as a report: this way is closed
	}
	pageID := ""
	for _, item := range asList(noted["saved"]) {
		if m, _ := item.(map[string]any); m["kind"] == "note" {
			pageID, _ = m["id"].(string)
		}
	}
	if pageID == "" {
		t.Fatalf("precondition: the note is saved: %.400s", auditJSON(noted))
	}
	again, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + pageID})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, k := range asList(again["knowledge"]) {
		if m, _ := k.(map[string]any); m["id"] == pageID {
			ref, _ = m["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("precondition: the page is read with the receipt: %.400s", auditJSON(again["knowledge"]))
	}
	second, err := record(map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Validated the Quartz exports; the wiki confirms it.", "sufficient": true, "evidence": []string{ref}}})
	t.Logf("result 1 = %s; note page %s, read as %s; result 2: err=%v complete=%v errors=%.300s", id, pageID, ref, err, second["complete"], auditJSON(second["errors"]))
	if err == nil && second["complete"] == true {
		t.Errorf("INVARIANT 10: a result with nothing observed is complete and sufficient: its only evidence is the page its own note wrote from its earlier result")
	}
}

// reportTitleRefusal refuses the title whatever the kind, and a source is taken for a report only with kind agent (workReport,
// markReport through prepareWorkReflection, workReportSource). A test result the agent observed under the title "Procedure
// execution" (kind test) would be citable, and it is refused at work record and at wiki capture.
func TestAuditKATestObservationRefusedForATitleItCanHave(t *testing.T) {
	if testWorkReport("test", notebook.WorkProcedure.Title()) || testWorkReport("human", notebook.WorkResult.Title()) {
		t.Fatalf("precondition: a source of kind test or human is no report")
	}
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": notebook.WorkProcedure.Title(), "content": "make test: 412 passed after the export change."}},
		"result": map[string]any{"changed": "Edited the export.", "checked": "Ran make test.", "evidence": []any{"new1"}}})
	raw := auditJSON(saved["errors"])
	t.Logf("work record, kind test, title %q: err=%v complete=%v errors=%.300s", notebook.WorkProcedure.Title(), err, saved["complete"], raw)
	if strings.Contains(raw, "is the one shikii gives its own reports") {
		t.Errorf("a test observation titled %q (kind test: never taken for a report, citable) is refused at work record", notebook.WorkProcedure.Title())
	}
	_, cerr := improvementCall(t, f.b, map[string]any{"operation": "capture", "observation": map[string]any{"kind": "test", "title": notebook.WorkProcedure.Title(), "content": "make test: 412 passed.", "reference": "make test", "version": "audit-k", "observed_at": "2026-11-12T00:00:00Z"}})
	t.Logf("wiki capture, kind test: %v", cerr)
	if cerr != nil && strings.Contains(cerr.Error(), "is the one shikii gives its own reports") {
		t.Errorf("a test observation titled %q is refused at wiki capture", notebook.WorkProcedure.Title())
	}
}

// The refusal of an outcome that cites the work's own result is selfEvidence's sentence for a result: "cite what you checked (an
// observation of this call as new1, a saved observation, a page you read ...)". An outcome takes "one to four captured-observation
// references": the page the refusal offers is refused in turn.
func TestAuditKTheOutcomeRefusalOffersAPage(t *testing.T) {
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Quartz export checks", "lesson": "Check Quartz exports with tenant filters.", "applies_to": "Quartz exports", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Validated the Quartz exports."}}); err != nil {
		t.Fatal(err)
	}
	outcome := func(sources []string) string {
		got, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Run the Quartz export check.", "result": "The check passed.", "sources": sources}}})
		if err != nil {
			return err.Error()
		}
		return auditJSON(got["errors"])
	}
	refused := outcome([]string{"result"})
	t.Logf("outcome citing result (a result that cites nothing): %.400s", refused)
	if !strings.Contains(refused, "an outcome cites the observations it rests on") || strings.Contains(refused, "a page you read") || strings.Contains(refused, "leave evidence out") {
		t.Errorf("the refusal of an outcome does not say what an outcome takes, or offers what it refuses: %.300s", refused)
	}
	// as the refusal says: an observation of this call
	got, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Quartz export check", "content": "The tenant filter kept only tenant A's rows."}},
		"outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Run the Quartz export check.", "result": "The check passed.", "sources": []string{"new1"}}}})
	// the outcome itself is saved (the work changed by the new observation asks for its ending review, which is another matter)
	saved := auditJSON(got["saved"])
	if err != nil || strings.Contains(auditJSON(got["errors"]), `"kind":"outcome"`) || !strings.Contains(saved, `"kind":"outcome"`) {
		t.Errorf("an outcome citing an observation of the call, as the refusal says, is not saved: %v saved=%.300s errors=%.300s", err, saved, auditJSON(got["errors"]))
	}
}

// INVARIANT 14: skills/shikii/references/recording.md says "`result` references the immutable agent report and can supply sources
// for notes/outcomes". The fix of audit I refuses "result" as the source of an outcome, also after a result with observed evidence:
// an agent that records the execution as the reference says is refused.
func TestAuditKTheReferenceSaysResultSuppliesOutcomeSources(t *testing.T) {
	doc := string(mustRead(t, "../../skills/shikii/references/recording.md"))
	if !strings.Contains(strings.Join(strings.Fields(doc), " "), "`result` references the immutable agent report and can supply sources for notes, and for outcomes when the result cites evidence of its own") {
		t.Fatalf("the reference does not say when result supplies the sources of outcomes")
	}
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Quartz export checks", "lesson": "Check Quartz exports with tenant filters.", "applies_to": "Quartz exports", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	done, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Quartz export check", "content": "The tenant filter kept only tenant A's rows."}},
		"result": map[string]any{"changed": "Validated the Quartz exports.", "checked": "Ran the export check.", "evidence": []string{"new1"}}})
	if err != nil {
		t.Fatalf("precondition: the result with observed evidence: %v", err)
	}
	t.Logf("result citing new1: complete=%v errors=%.200s", done["complete"], auditJSON(done["errors"]))
	outcome, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Run the Quartz export check.", "result": "The check passed.", "sources": []string{"result"}}}})
	raw := auditJSON(outcome["errors"])
	t.Logf("outcome with sources [result], as the reference says: err=%v errors=%.300s", err, raw)
	if err != nil || strings.Contains(raw, "wrote about itself") {
		t.Errorf("INVARIANT 14: the reference says result can supply the sources of outcomes, and an outcome citing result is refused: %v %.300s", err, raw)
	}
}
