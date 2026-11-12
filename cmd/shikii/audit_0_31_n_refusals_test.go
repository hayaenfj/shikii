package main

import (
	"strings"
	"testing"
)

// Audit N of 0.31.0: the refusals the redesign changed, followed the way they say.

// auditNPageOfTheResult records a result with nothing observed, writes a page whose only source is that result (wiki ingest), and
// reads the page with the same receipt. It returns the receipt and the reference of the page.
func auditNPageOfTheResult(t *testing.T) (map[string]any, string, func(map[string]any) (map[string]any, error)) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "invoice rounding", "request": "Look at invoice rounding"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) (map[string]any, error) {
		fields["receipt"] = v["receipt"]
		return workCall(t, b, "record", fields)
	}
	id := auditKResultID(t, v["receipt"], func(fields map[string]any) (map[string]any, error) { return workCall(t, b, "record", fields) })
	ingested, err := improvementCall(t, b, map[string]any{"operation": "ingest", "sources": []string{id}, "title": "Invoices round per line", "lesson": "Invoices round per line.", "applies_to": "invoice rounding", "kind": "rule", "scene": "when computing invoices"})
	if err != nil {
		t.Fatalf("precondition: a page from the work's own result: %v", err)
	}
	pageID, _ := loopObject(t, loopObject(t, ingested)["page"])["id"].(string)
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
		t.Fatalf("precondition: the page is read with the receipt: %.300s", auditJSON(again["knowledge"]))
	}
	return v, ref, record
}

// The refusal of a result that cites a page written only from the work's own report says: "cite what you checked (an observation of
// this call as new1, a saved observation, a page you read), or leave evidence out". Each way it offers, sent as the next call, is
// refused again: the refused result is saved and waits for a ref-only repair with actual evidence, which the refusal does not name.
// "Leave evidence out" cannot end it at all (a ref-only repair needs actual evidence refs).
func TestAuditNPageRefusalFollowedAsItSays(t *testing.T) {
	for _, way := range []string{"cite new1", "leave evidence out"} {
		t.Run(way, func(t *testing.T) {
			_, ref, record := auditNPageOfTheResult(t)
			refused, err := record(map[string]any{"result": map[string]any{"changed": "Invoices round per line.", "checked": "Checked.", "evidence": []string{ref}}})
			raw := auditJSON(refused["errors"])
			if err != nil || !strings.Contains(raw, "a page whose every source is a report") || !strings.Contains(raw, "or leave evidence out") {
				t.Fatalf("precondition: the page of reports is refused with the two ways: %v %.400s", err, raw)
			}
			next := map[string]any{"result": map[string]any{"changed": "Invoices round per line.", "checked": "Checked."}}
			if way == "cite new1" {
				next["observations"] = []any{map[string]any{"kind": "test", "title": "Invoice rounding fixture", "content": "3 lines of 0.005 round to 0.03."}}
				next["result"].(map[string]any)["evidence"] = []string{"new1"}
			}
			got, err := record(next)
			t.Logf("refusal: %.300s", raw)
			t.Logf("%s, as the refusal says: err=%v complete=%v errors=%.400s", way, err, got["complete"], auditJSON(got["errors"]))
			if err != nil || got["complete"] != true {
				t.Errorf("the refusal offers %q, and the call sent that way is refused again: %.300s", way, auditJSON(got["errors"]))
			}
		})
	}
}

// how_to_record, the short contract in every reply of prepare, says "result and observations supply source refs for notes/outcomes"
// without the condition recording.md and the code have (an outcome cites result only when the result cites evidence of its own). An
// agent that follows how_to_record with a result that cites nothing is refused.
func TestAuditNHowToRecordPromisesResultAsOutcomeSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Invoice rounding check", "lesson": "Run the invoice fixture.", "applies_to": "invoice rounding", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "invoice rounding", "request": "Run the invoice rounding check"})
	if err != nil {
		t.Fatal(err)
	}
	how, _ := v["how_to_record"].(string)
	promised := strings.Contains(how, "result and observations supply source refs for notes/outcomes")
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	got, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Ran the invoice rounding check."},
		"outcomes": []any{map[string]any{"ref": ref, "conditions": "two lines", "action": "Ran the fixture.", "result": "Rounded per line.", "sources": []string{"result"}}}})
	raw := auditJSON(got["errors"])
	t.Logf("how_to_record: %.200s", how)
	t.Logf("outcome citing result, as how_to_record says: err=%v errors=%.300s", err, raw)
	if promised && strings.Contains(raw, `"kind":"outcome"`) {
		t.Errorf("how_to_record says result supplies the sources of outcomes, and an outcome citing result is refused (recording.md and the code add: when the result cites evidence of its own)")
	}
}

// The refusal of an outcome that cites this work's result (sent in the same call, a result that cites nothing) calls it "an earlier
// result": the result is the one of this very call.
func TestAuditNOutcomeRefusalCallsThisResultEarlier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Invoice rounding check", "lesson": "Run the invoice fixture.", "applies_to": "invoice rounding", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "invoice rounding", "request": "Run the invoice rounding check"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	got, _ := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Ran the invoice rounding check."},
		"outcomes": []any{map[string]any{"ref": ref, "conditions": "two lines", "action": "Ran the fixture.", "result": "Rounded per line.", "sources": []string{"result"}}}})
	raw := auditJSON(got["errors"])
	t.Logf("%.400s", raw)
	if strings.Contains(raw, "an earlier result") {
		t.Errorf("the refusal calls the result of this very call an earlier result: %.300s", raw)
	}
}

// An observation of a work record that is refused for its title comes back with no ref (before the redesign: "new1"), while the
// refusal of the result that cites it says "observation 1 of this call was not saved (see its error)".
func TestAuditNRefusedObservationNamesItsRef(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	got, err := f.record(first, map[string]any{"observations": []any{
		map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."},
		map[string]any{"kind": "agent", "title": "Work result and remaining conditions", "content": "Ran make test: 412 passed."},
	}, "result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"new2"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("errors: %.500s", auditJSON(got["errors"]))
	for _, item := range asList(got["errors"]) {
		m, _ := item.(map[string]any)
		if m["kind"] == "observation" && m["ref"] != "new2" {
			t.Errorf("the refused observation (the second of the call) comes back under ref %q, not new2: the agent is told to see its error and retry the failed refs", m["ref"])
		}
	}
}

// The same refusal under a hook: the agent leaves evidence out, as the refusal says, and its turn is stopped at the Stop until it
// cites actual evidence for the refused result (the refused result waits for a ref-only repair with evidence refs).
func TestAuditNLeavingEvidenceOutAsToldIsStoppedAtTheStop(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	id := auditKResultID(t, first["receipt"], func(fields map[string]any) (map[string]any, error) { return workCall(t, f.b, "record", fields) })
	ingested, err := improvementCall(t, f.b, map[string]any{"operation": "ingest", "sources": []string{id}, "title": "The export keeps its filters", "lesson": "The CSV export keeps its filters.", "applies_to": "CSV export filters", "kind": "fact", "scene": "when exporting CSV"})
	if err != nil {
		t.Fatal(err)
	}
	pageID, _ := loopObject(t, loopObject(t, ingested)["page"])["id"].(string)
	again, err := workCall(t, f.b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + pageID})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, k := range asList(again["knowledge"]) {
		if m, _ := k.(map[string]any); m["id"] == pageID {
			ref, _ = m["ref"].(string)
		}
	}
	refused, _ := f.record(first, map[string]any{"result": map[string]any{"changed": "The export keeps its filters.", "evidence": []string{ref}}})
	if !strings.Contains(auditJSON(refused["errors"]), "or leave evidence out") {
		t.Fatalf("precondition: refused with the two ways: %.300s", auditJSON(refused["errors"]))
	}
	out, _ := f.record(first, map[string]any{"result": map[string]any{"changed": "The export keeps its filters."}})
	t.Logf("evidence left out: complete=%v errors=%.300s", out["complete"], auditJSON(out["errors"]))
	for i := 1; i <= 2; i++ {
		reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done; nothing was checked."}))
		t.Logf("Stop %d: blocked=%v reason=%.1600s", i, blocked, reason)
		if i == 1 && blocked {
			t.Errorf("an agent that left evidence out, as the refusal said, is stopped at the Stop: %.200s", reason)
		}
	}
}
