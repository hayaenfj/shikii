package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"strings"
	"testing"
)

// Audit J of 0.31.0 (fifth round): the checks of the shipped tests themselves.

// The check this round took out of TestAuditEAReportAmongThePendingObservationsIsNotListedAsUsable (audit_0_31_e_more_test.go):
// a report among the pending observations is not in the list of what evidence can cite. The list is printed when a receipt
// without hook ids cites current; a report of the earlier request waits there as evidenceN. With the w.Reports filter of the list
// taken out (work_record.go:184), every shipped test passes.
func TestAuditJTheUsableListLeavesThePendingReportOut(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	done, err := f.record(first, map[string]any{
		"result":    map[string]any{"changed": "Edited the export.", "checked": "Looked at it."},
		"reasoning": map[string]any{"phase": "finish", "focus": "Review the export", "assessment": "retain", "reason": "The export was edited.", "evidence": []any{"current"}, "next_action": "None.", "result": "Done."}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: %v %.400s", err, auditJSON(done))
	}
	standalone, err := workCall(t, f.b, "prepare", map[string]any{"query": "CSV export totals", "request": "Check the totals of the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	reports := []string{}
	for _, raw := range auditGSlice(standalone["observations"]) {
		o, _ := raw.(map[string]any)
		text := auditJSON(o["source"])
		if strings.Contains(text, notebook.WorkResult().Title()) || strings.Contains(text, notebook.WorkReview().Title()) {
			ref, _ := o["ref"].(string)
			reports = append(reports, ref)
		}
	}
	if len(reports) == 0 {
		t.Fatalf("precondition: a report of the earlier request is listed among the observations: %.300s", auditJSON(standalone["observations"]))
	}
	refused, err := f.record(standalone, map[string]any{"result": map[string]any{"changed": "Checked the totals.", "evidence": []any{"current"}}})
	raw := auditJSON(refused["errors"])
	t.Logf("reports listed as %v; the refusal of current: %.500s", reports, raw)
	if err != nil || !strings.Contains(raw, "usable here: ") {
		t.Fatalf("precondition: the refusal lists what evidence can cite: %v %s", err, raw)
	}
	list := raw[strings.Index(raw, "usable here: "):]
	for _, ref := range reports {
		if strings.Contains(list, ref+",") || strings.Contains(list, ref+" (") || strings.Contains(list, ": "+ref) || strings.Contains(list, " "+ref+" ") {
			t.Errorf("the refusal lists %s, a report of the earlier request, among what evidence can cite: %s", ref, list)
		}
	}
}
