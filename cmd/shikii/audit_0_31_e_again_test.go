package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func auditEObs(title, content string) map[string]any {
	return map[string]any{"kind": "test", "title": title, "content": content}
}

func auditESavedRefs(saved map[string]any) string {
	items, _ := saved["saved"].([]any)
	out := []string{}
	for _, raw := range items {
		item := raw.(map[string]any)
		out = append(out, fmt.Sprint(item["kind"], ":", item["ref"]))
	}
	return strings.Join(out, " ")
}

// audit E: a request that interrupted another is prepared, records, and is prepared again, three times over. What each receipt
// is handed, how the observations are numbered, and what the result can cite at the end.
func TestAuditEPreparedAgainAfterAnInterruption(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{auditEObs("Totals", "The totals match.")}}); err != nil {
		t.Fatal(err)
	}
	// the person's message comes during the work
	ids := f.ask("Also check the totals of the CSV export.")
	second := f.prepare(ids, "CSV export totals")
	t.Logf("R2 #1 carried: %s", auditJSON(second["carried"]))
	saved, err := f.record(second, map[string]any{"observations": []any{auditEObs("Filters", "The filters are kept.")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("R2 #1 saved: %s", auditESavedRefs(saved))
	if got := auditESavedRefs(saved); got != "observation:observed2" {
		t.Errorf("the observation of the interrupting request follows the numbers it was handed: %s", got)
	}
	third := f.prepare(ids, "CSV export totals")
	carried, _ := third["carried"].(map[string]any)
	t.Logf("R2 #2 carried: %s", auditJSON(carried))
	if got := auditJSON(carried["sources"]); got != `["observed1","observed2"]` {
		t.Errorf("the second preparation of the interrupting request keeps observed1 (handed over) and observed2 (its own): %s", got)
	}
	saved, err = f.record(third, map[string]any{"observations": []any{auditEObs("Button", "The button reads Export.")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := auditESavedRefs(saved); got != "observation:observed3" {
		t.Errorf("the third observation is observed3: %s", got)
	}
	fourth := f.prepare(ids, "CSV export totals")
	carried, _ = fourth["carried"].(map[string]any)
	t.Logf("R2 #3 carried: %s", auditJSON(carried))
	if got := auditJSON(carried["sources"]); got != `["observed1","observed2","observed3"]` {
		t.Errorf("the third preparation keeps all three: %s", got)
	}
	// the earlier receipts are refused
	if _, err := f.record(third, map[string]any{"observations": []any{auditEObs("Late", "Late.")}}); err == nil {
		t.Errorf("the replaced receipt still records")
	}
	done, err := f.record(fourth, map[string]any{"result": map[string]any{"changed": "Checked the totals.", "checked": "Compared.", "evidence": []any{"observed1", "observed2", "observed3"}}})
	if err != nil || done["complete"] != true {
		t.Errorf("the last receipt cites all three observations: %v complete=%v errors=%s", err, done["complete"], auditJSON(done["errors"]))
	}
	articles := loadSession(f.root, "planning").Planning.Work.Articles
	seen := map[string]string{}
	for ref, a := range articles {
		key := a.ID + "@" + a.Revision
		if other, ok := seen[key]; ok {
			t.Errorf("the same page revision has two references: %s and %s (%s)", ref, other, key)
		}
		seen[key] = ref
	}
	t.Logf("articles of the last work: %d", len(articles))
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Errorf("the Stop after the recorded result passes: %s", reason)
	}
}

// audit E: the request is prepared again after its result and ending review were recorded. The reasoning state of the new
// receipt, and whether the Stop still passes without anything more.
func TestAuditEPreparedAgainAfterTheResult(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	done, err := f.record(first, map[string]any{"observations": []any{auditEObs("Totals", "The totals match.")}, "result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"new1"}}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: %v %.300s", err, auditJSON(done))
	}
	second := f.prepare(ids, "CSV export filters")
	t.Logf("carried: %s", auditJSON(second["carried"]))
	t.Logf("reasoning: %.400s", auditJSON(second["reasoning"]))
	w := loadSession(f.root, "planning").Planning.Work
	t.Logf("sources=%v reports=%d result=%v", w.Sources, len(w.Reports), w.Result != nil)
	for ref, id := range w.Sources {
		if strings.HasPrefix(ref, "observed") && w.Reports[id] {
			t.Errorf("a report of the earlier preparation is among the sources of the new one under %s", ref)
		}
	}
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Errorf("the result and its review were recorded before the request was prepared again, and the Stop is blocked: %.300s", reason)
	}
	// the same result sent again with the new receipt
	again, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"observed1"}}})
	t.Logf("the same result with the new receipt: err=%v complete=%v errors=%s saved=%s", err, again["complete"], auditJSON(again["errors"]), auditESavedRefs(again))
	if err != nil || again["complete"] != true {
		t.Errorf("the same result, citing the observation the first receipt saved, is refused with the new receipt")
	}
}

// audit E: a report whose save failed once, then the request is prepared again, then the report is finished by a later call.
func TestAuditEReportPendingAcrossThePreparationAgain(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "observation" && !failed {
			failed = true
			return errors.New("synthetic: the write was interrupted")
		}
		return nil
	}
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
	workAfterWrite = old
	if !failed || err != nil || a["complete"] == true {
		t.Fatalf("precondition: the first report's save fails once: %v %.300s", err, auditJSON(a))
	}
	second := f.prepare(ids, "CSV export filters")
	t.Logf("carried: %s", auditJSON(second["carried"]))
	b, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
	t.Logf("B: err=%v complete=%v saved=%s errors=%s", err, b["complete"], auditESavedRefs(b), auditJSON(b["errors"]))
	w := loadSession(f.root, "planning").Planning.Work
	t.Logf("sources=%v reports=%v", w.Sources, w.Reports)
	for ref, id := range w.Sources {
		if !strings.HasPrefix(ref, "observed") {
			continue
		}
		c, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Renamed the export button, checked (" + ref + ").", "checked": "It works.", "sufficient": true, "evidence": []any{ref}}})
		t.Logf("C cites %s (%s): err=%v complete=%v errors=%.160s", ref, id, err, c["complete"], auditJSON(c["errors"]))
		if err == nil && c["complete"] == true {
			t.Errorf("INVARIANT 10: nothing was observed, and a result whose only evidence is %s (a report of this work) is complete", ref)
		}
	}
	next := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	if carried, _ := next["carried"].(map[string]any); carried != nil && strings.Contains(auditJSON(carried["sources"]), "observed") {
		t.Errorf("INVARIANT 9: a report of the interrupted request is handed over: %s", auditJSON(carried["sources"]))
	}
}

// audit E: the save of the first report fails once after its source was written (the failure point of the persistence tests), and
// the request is prepared again. The written source waits for reflection, so the new preparation lists it as evidenceN; the note
// that it is a report is only made when the capture is finished, which happens after the next result bound its evidence.
func TestAuditEUnfinishedReportCitedAsEvidenceN(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "observation" && !failed {
			failed = true
			return errors.New("synthetic: the write was interrupted")
		}
		return nil
	}
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
	workAfterWrite = old
	if !failed || err != nil || a["complete"] == true {
		t.Fatalf("precondition: the first report's save fails once: %v %.300s", err, auditJSON(a))
	}
	second := f.prepare(ids, "CSV export filters")
	w := loadSession(f.root, "planning").Planning.Work
	t.Logf("after preparing again: sources=%v reports=%v observations of the reply=%.300s", w.Sources, w.Reports, auditJSON(second["observations"]))
	ref := ""
	for name := range w.Sources {
		if strings.HasPrefix(name, "evidence") {
			ref = name
		}
	}
	if ref == "" {
		t.Fatal("the unfinished report is not listed as evidenceN")
	}
	c, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Renamed the export button, checked.", "checked": "It works.", "sufficient": true, "evidence": []any{ref}}})
	t.Logf("C cites %s: err=%v complete=%v errors=%.200s storage=%s", ref, err, c["complete"], auditJSON(c["errors"]), auditJSON(c["storage"]))
	w = loadSession(f.root, "planning").Planning.Work
	t.Logf("afterwards: sources=%v reports=%v", w.Sources, w.Reports)
	if err == nil && c["complete"] == true {
		t.Errorf("INVARIANT 10: nothing was observed; the result (sufficient: true) is complete with %s as its only evidence, and %s is the first report of this same work (its save failed once and the request was prepared again)", ref, ref)
	}
}

// control: without the failure, the reports of the first preparation are refused under every name the second one has for them.
func TestAuditEReportsOfTheFirstPreparationUnderEveryName(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked"}}})
	if err != nil || a["complete"] == true {
		t.Fatalf("precondition: report A waits for evidence: %v %.300s", err, auditJSON(a))
	}
	second := f.prepare(ids, "CSV export filters")
	w := loadSession(f.root, "planning").Planning.Work
	t.Logf("sources=%v reports=%v observations=%.200s", w.Sources, w.Reports, auditJSON(second["observations"]))
	for name, id := range w.Sources {
		for _, ref := range []string{name, id} {
			c, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Renamed the export button (" + ref + ").", "sufficient": true, "evidence": []any{ref}}})
			if err == nil && c["complete"] == true {
				t.Errorf("a result citing %s (%s) is complete", ref, name)
			}
		}
	}
}

// audit E: the result of a request is recorded together with the agent's own ending review (reasoning finish), so the work does
// not write its internal review and the two reports stay in the queue that waits for reflection. The next request lists that queue
// as evidenceN. What the next request's result can rest on, after an interruption and after a Stop that let the turn end.
func TestAuditEReportsOfTheEarlierRequestAsEvidenceN(t *testing.T) {
	for _, ended := range []bool{false, true} {
		name := "the person's message came during the work"
		if ended {
			name = "the Stop let the turn end"
		}
		t.Run(name, func(t *testing.T) {
			f := newAuditFlow(t)
			first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
			done, err := f.record(first, map[string]any{
				"result":    map[string]any{"changed": "Edited the export.", "checked": "Looked at it."},
				"reasoning": map[string]any{"phase": "finish", "focus": "Review the export", "assessment": "retain", "reason": "The export was edited.", "evidence": []any{"current"}, "next_action": "None.", "result": "Done."}})
			if err != nil || done["complete"] != true {
				t.Fatalf("precondition: the result and the agent's ending review are recorded: %v %.400s", err, auditJSON(done))
			}
			reports := loadSession(f.root, "planning").Planning.Work.Reports
			if ended {
				if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
					t.Fatalf("precondition: the Stop passes: %s", reason)
				}
			}
			second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
			w := loadSession(f.root, "planning").Planning.Work
			t.Logf("reports of the first request: %v; the next request holds: %v (its own notes of reports: %v); carried=%.60s", reports, w.Sources, w.Reports, auditJSON(second["carried"]))
			for ref, id := range w.Sources {
				if !reports[id] {
					continue
				}
				c, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Checked the totals (" + ref + ").", "checked": "See the earlier report.", "sufficient": true, "evidence": []any{ref}}})
				t.Logf("the next request's result cites %s: err=%v complete=%v errors=%.140s", ref, err, c["complete"], auditJSON(c["errors"]))
				if err == nil && c["complete"] == true {
					t.Errorf("%s: nothing was observed, and the next request's result (sufficient: true) is complete with %s as its only evidence: a report the earlier request wrote about itself", name, ref)
				}
			}
		})
	}
}
