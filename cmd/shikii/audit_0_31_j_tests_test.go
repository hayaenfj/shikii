package main

import (
	"strings"
	"testing"
)

// Audit J of 0.31.0 (fifth round): the checks of the shipped tests themselves.

// bodyReaders (audit_0_31_d_tools_test.go) now looks at every use of r.Body, but only in a function declared with a parameter of
// type *http.Request. A handler that is a function literal (the package has three: project_registry.go, serve_ui.go,
// serve_writer.go), and a handler that reads the body through another name of the request, are not looked at.
func TestAuditJBodyReadersMissHandlersItDoesNotLookAt(t *testing.T) {
	for name, source := range map[string]string{
		"a handler that is a function literal": "package main\nfunc (s *server) routes() http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tvar req struct{}\n\t\t_ = json.NewDecoder(r.Body).Decode(&req)\n\t})\n}\n",
		"the request under another name":       "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\tin := r\n\t_ = json.NewDecoder(in.Body).Decode(&req)\n}\n",
	} {
		if found := bodyReaders(t, map[string]string{"new_handler.go": source}); len(found) == 0 {
			t.Errorf("%s: a new handler that reads its JSON body with json.NewDecoder is not found", name)
		}
	}
	// negative: the form the test knows is found
	if found := bodyReaders(t, map[string]string{"new_handler.go": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n"}); len(found) != 1 {
		t.Errorf("negative: the form json.NewDecoder(r.Body) is found: %v", found)
	}
}

// agentSourceTitles (audit_0_31_g_titles_test.go) finds a source literal by its type name. A literal whose type is left out, as
// an element of a slice literal, has no type name and is passed over. The package writes one that way now:
// work_assignment_record.go builds []workObservation{{Kind: "agent", Title: assignmentClip(...)}}, a title that is not in
// workSourceTitles, and TestEveryTitleShikiiWritesIsClassified passes.
func TestAuditJTitleReaderMissesElidedLiterals(t *testing.T) {
	found := sourceWrites(t, map[string]string{"new.go": "package main\nvar _ = []workObservation{{Kind: \"agent\", Title: \"A new archive of the work\"}}\nvar _ = []notebook.Observation{{Kind: \"agent\", Title: \"Another archive\"}}\n"})
	if len(found) != 2 {
		t.Errorf("the titles of agent sources written as elements of a slice literal are not found: %v", found)
	}
	real := sourceWrites(t, map[string]string{"work_assignment_record.go": string(mustRead(t, "work_assignment_record.go"))})
	t.Logf("sources found in work_assignment_record.go: %v", real)
	if len(real) == 0 {
		t.Errorf("work_assignment_record.go writes a source with kind agent and the title assignmentClip(role stage: state), which the reader does not see")
	}
}

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
		if strings.Contains(text, workResultTitle) || strings.Contains(text, workReviewTitle) {
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
