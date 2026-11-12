package main

import (
	"testing"
)

// audit E, tests for lines of the fix that no shipped test of the sets I ran guards (each is GREEN on the candidate and RED with
// the named line taken out).

// work.go, "for id, pending := range w.Reasoning.PendingResults { markReport ... }": a report that waits for its evidence when the
// person's next message comes is listed for the next request as evidenceN (it waits for reflection). The next request's result
// cannot rest on it.
func TestAuditEGapAPendingReportOfTheInterruptedRequestIsNotEvidence(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked"}}})
	idA, _ := a["storage"].(map[string]any)["result_source"].(string)
	if err != nil || a["complete"] == true || idA == "" {
		t.Fatalf("precondition: report A waits for its evidence: %v %.300s", err, auditJSON(a))
	}
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	w := loadSession(f.root, "planning").Planning.Work
	names := []string{}
	for name, id := range w.Sources {
		if id == idA {
			names = append(names, name)
		}
	}
	t.Logf("the interrupting request holds report A as %v; reports noted: %v", names, w.Reports)
	if len(names) == 0 {
		t.Skip("report A is not among the sources of the next request")
	}
	for _, ref := range append(names, idA) {
		c, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Checked the totals (" + ref + ").", "sufficient": true, "evidence": []any{ref}}})
		if err == nil && c["complete"] == true {
			t.Errorf("the next request's result (sufficient: true, nothing observed) is complete with %s, the earlier request's report that still waits for its own evidence", ref)
		}
	}
}

// work.go, "markReport(w, w.Result.Source)" at prepare: a session record written before reports were noted (0.30.8) holds a
// result that is complete. Prepared again, a new result cannot rest on that result, under any name the new receipt has for it.
func TestAuditEGapTheSavedResultOfAnOlderRecordIsNotEvidence(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	done, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
	idR, _ := done["storage"].(map[string]any)["result_source"].(string)
	if err != nil || done["complete"] != true || idR == "" {
		t.Fatalf("precondition: a result without evidence is recorded: %v %.300s", err, auditJSON(done))
	}
	if err := updateSession(f.root, "planning", func(s *hookSession) { s.Planning.Work.Reports = nil }); err != nil {
		t.Fatal(err)
	}
	second := f.prepare(ids, "CSV export filters")
	w := loadSession(f.root, "planning").Planning.Work
	refs := []string{idR}
	for name, id := range w.Sources {
		if id == idR && name != "result" {
			refs = append(refs, name)
		}
	}
	t.Logf("the new receipt holds the saved result as %v; reports noted: %v", refs, w.Reports)
	for _, ref := range refs {
		later, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Renamed the button and checked it (" + ref + ").", "sufficient": true, "evidence": []any{ref}}})
		if err != nil {
			t.Fatal(err)
		}
		if later["complete"] == true {
			t.Errorf("an older record prepared again: a new result (sufficient: true, nothing observed) cites the saved result as %s and is complete", ref)
		}
	}
}

// audit E: the same older record (no note of its reports): what else of it the new receipt can cite. Its ending review is handed
// over as observedN and is noted nowhere.
func TestAuditEOlderRecordEndingReviewAfterThePreparationAgain(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	done, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
	idR, _ := done["storage"].(map[string]any)["result_source"].(string)
	if err != nil || done["complete"] != true || idR == "" {
		t.Fatalf("precondition: %v %.300s", err, auditJSON(done))
	}
	if err := updateSession(f.root, "planning", func(s *hookSession) {
		s.Planning.Work.Reports = nil
		for key, capture := range s.Planning.Work.Captures {
			capture.Report = false
			s.Planning.Work.Captures[key] = capture
		}
	}); err != nil {
		t.Fatal(err)
	}
	second := f.prepare(ids, "CSV export filters")
	w := loadSession(f.root, "planning").Planning.Work
	t.Logf("sources=%v reports=%v carried=%s", w.Sources, w.Reports, auditJSON(second["carried"]))
	for name, id := range w.Sources {
		if id == idR || name == "result" {
			continue
		}
		later, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Renamed the button and checked it (" + name + ").", "sufficient": true, "evidence": []any{name}}})
		t.Logf("a new result citing %s: err=%v complete=%v errors=%.160s", name, err, later["complete"], auditJSON(later["errors"]))
		if err == nil && later["complete"] == true {
			t.Errorf("an older record prepared again: a new result (sufficient: true, nothing observed) is complete with %s, the ending review of the earlier result, as its only evidence", name)
		}
	}
}
