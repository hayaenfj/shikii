package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// audit C, finding A-2 (INVARIANTS 10): the request is prepared a second time (the Stop block says "prepare again ... when you no
// longer hold the receipt"). The new work state starts without the note of which sources are the work's own reports, while
// "result" still names the latest report.
func TestAuditCReportsAfterTheSameRequestIsPreparedAgain(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"observed1"}}})
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := a["storage"].(map[string]any)["result_source"].(string)
	if a["complete"] == true || idA == "" || !strings.Contains(auditJSON(a["errors"]), "is this result itself") {
		t.Fatalf("precondition: report A is saved and refused as its own evidence: %s", auditJSON(a))
	}
	b, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button.", "checked": "Looked at it.", "evidence": []any{"observed1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if b["complete"] == true || !strings.Contains(auditJSON(b["errors"]), "a report this work wrote about itself") {
		t.Fatalf("precondition: report B, citing report A, is refused: %s", auditJSON(b))
	}
	// control: with the same receipt, A repaired with B as its evidence is refused
	control, err := f.record(first, map[string]any{"result": map[string]any{"ref": idA, "evidence": []any{"result"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("control (same receipt): complete=%v %.160s", control["complete"], auditJSON(control["errors"]))
	if control["complete"] == true {
		t.Fatalf("control: with the first receipt the repair is refused: %s", auditJSON(control))
	}
	second := f.prepare(ids, "CSV export filters") // the same request, prepared again
	repaired, err := f.record(second, map[string]any{"result": map[string]any{"ref": idA, "evidence": []any{"result"}}})
	t.Logf("after preparing again: err=%v complete=%v saved=%s storage=%s", err, repaired["complete"], auditJSON(repaired["saved"]), auditJSON(repaired["storage"]))
	if err == nil && repaired["complete"] == true {
		t.Errorf("INVARIANT 10: after the same request was prepared again, report A is complete with report B (result) as its evidence: nothing was observed, and both are reports the work wrote about itself")
	}
}

// audit C, finding A-3 (INVARIANTS 10 and 9): saving a report fails once after the source was written (the failure point the
// persistence tests use), and the agent sends the report again in other words. The first report's capture is finished later by the
// ending, outside captureWorkCompletion, and is not noted as a report.
func TestAuditCReportWhoseCaptureWasFinishedLater(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
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
	t.Logf("A (its save is interrupted): err=%v complete=%v errors=%.160s", err, a["complete"], auditJSON(a["errors"]))
	if !failed || err != nil || a["complete"] == true {
		t.Fatalf("precondition: the first report's save fails once: %v %.300s", err, auditJSON(a))
	}
	b, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
	t.Logf("B (reworded): err=%v complete=%v saved=%.200s", err, b["complete"], auditJSON(b["saved"]))
	w := loadSession(f.root, "planning").Planning.Work
	t.Logf("sources=%v reports=%d", w.Sources, len(w.Reports))
	c, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button, checked.", "checked": "It works.", "sufficient": true, "evidence": []any{"observed1"}}})
	t.Logf("C cites observed1 (report A): err=%v complete=%v errors=%.200s", err, c["complete"], auditJSON(c["errors"]))
	if err == nil && c["complete"] == true {
		t.Errorf("INVARIANT 10: a result (sufficient: true) whose only evidence is observed1, the first report of the same work (nothing was observed), is complete")
	}
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	if carried, _ := second["carried"].(map[string]any); carried != nil && strings.Contains(auditJSON(carried["sources"]), "observed") {
		t.Errorf("INVARIANT 9: the first report of the interrupted request is handed over as a saved source: %s", auditJSON(carried["sources"]))
	}
}

// audit C, finding A-4 (INVARIANTS 10, last sentence): a receipt without hook ids has no current request. The refusals it meets
// on the way to a recorded result.
func TestAuditCStandaloneRefusalsDoNotOfferCurrent(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) string {
		t.Helper()
		fields["receipt"] = prepared["receipt"]
		saved, err := workCall(t, b, "record", fields)
		if err != nil {
			return err.Error()
		}
		return auditJSON(saved["errors"])
	}
	// the most common wrong call: what was checked, written as text in evidence
	first := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked at the page"}}})
	t.Logf("1) evidence as text: %.330s", first)
	if strings.Contains(first, "use current") || strings.Contains(first, "(current") || strings.Contains(first, "or current") {
		t.Errorf("INVARIANT 10: the refusal a receipt without hook ids gets offers current")
	}
	second := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"current"}}})
	t.Logf("2) following it with evidence [current]: %.200s", second)
	if !strings.Contains(second, `unknown source reference \"current\"`) {
		t.Errorf("control: current is refused for this receipt: %.300s", second)
	}
	third := record(map[string]any{"reasoning": map[string]any{"phase": "finish", "focus": "Review the result", "assessment": "retain", "reason": "Nothing changed.", "evidence": []any{"knowledge1"}, "next_action": "None.", "result": "Done."}})
	t.Logf("3) reasoning finish while a report waits: %.330s", third)
	if strings.Contains(third, "refs are current") {
		t.Errorf("INVARIANT 10: the refusal of the ending review tells a receipt without hook ids that refs are current, newN or knowledgeN")
	}
}

// audit C, finding U-4 (GREEN in the candidate; fails with work.go's "h.Observed > w.ObservedBase" taken out, which no test of the
// candidate that I ran catches): the same request prepared again does not give out an observedN name a second time.
func TestAuditCObservedNumbersAfterTheSameRequestIsPreparedAgain(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	second := f.prepare(ids, "CSV export filters")
	saved, err := f.record(second, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Filters", "content": "The filters are kept."}}})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := saved["saved"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["ref"] != "observed2" {
		t.Errorf("the second preparation of the same request names its first observation observed2 (observed1 was given out by the first): %s", auditJSON(saved["saved"]))
	}
}

// audit C, finding U-4 (GREEN in the candidate; fails with work_result_recovery.go's markReport taken out): the source that binds a
// repaired report to its evidence is a report of the work about itself too, and a later result cannot cite it.
func TestAuditCTheRepairBindingIsNotEvidence(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) map[string]any {
		t.Helper()
		fields["receipt"] = prepared["receipt"]
		saved, err := workCall(t, b, "record", fields)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	first := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked at it"}}})
	id, _ := first["storage"].(map[string]any)["result_source"].(string)
	if id == "" || first["complete"] == true {
		t.Fatalf("precondition: the report is saved and waits for its evidence: %s", auditJSON(first))
	}
	repaired := record(map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}}, "result": map[string]any{"ref": id, "evidence": []any{"new1"}}})
	binding, _ := repaired["storage"].(map[string]any)["binding_source"].(string)
	if binding == "" {
		t.Fatalf("precondition: the repair saved its binding: %s", auditJSON(repaired))
	}
	later := record(map[string]any{"result": map[string]any{"changed": "Renamed the button and the menu entry.", "checked": "See the repair.", "sufficient": true, "evidence": []any{binding}}})
	if later["complete"] == true || !strings.Contains(auditJSON(later["errors"]), "a report this work wrote about itself") {
		t.Errorf("a result whose only evidence is the binding of an earlier repair (a report of the work about itself) is not refused: %s", auditJSON(later))
	}
}

// The procedure record of a result is written from that result. Citing it, by its name or by its id, is the work citing itself
// (audit C of 0.31.0, an unconfirmed doubt: the source was not noted as a report).
func TestAuditCTheProcedureRecordIsNotEvidence(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Check recovery after a lost acknowledgement.", "")
	for i := 0; i < 3; i++ {
		if _, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The requested source condition was checked.")},
		"result": map[string]any{"changed": "Checked the requested source condition.", "evidence": []string{"new1"}}})
	if err != nil || saved["complete"] != true {
		t.Fatalf("precondition: the first result is recorded: %v %.400s", err, auditJSON(saved))
	}
	procedure, _ := saved["procedure"].(map[string]any)
	id, _ := procedure["source"].(string)
	if id == "" {
		t.Fatalf("precondition: the record reports the source of its procedure record: %.400s", auditJSON(saved["procedure"]))
	}
	for _, ref := range []string{"procedure", id} {
		later, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the source condition and its exception (" + ref + ").", "evidence": []string{ref}}})
		if err != nil {
			t.Fatal(err)
		}
		if raw := auditJSON(later["errors"]); later["complete"] == true || !strings.Contains(raw, "wrote about itself") {
			t.Errorf("a later result with nothing observed cites the procedure record as %q and is not refused: complete=%v errors=%s", ref, later["complete"], raw)
		}
	}
}

// The same request prepared again (the agent lost its receipt): the new receipt cites what the earlier one observed, and the
// reply says so (audit D of 0.31.0, finding U-9: a message during the work took the references over, a lost receipt did not).
func TestAuditCThePreparationAgainKeepsTheEarlierObservations(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}}); err != nil {
		t.Fatal(err)
	}
	second := f.prepare(ids, "CSV export filters")
	carried, _ := second["carried"].(map[string]any)
	if text, _ := carried["next_action"].(string); !strings.HasPrefix(text, "This request was prepared before.") || !strings.Contains(auditJSON(carried["sources"]), `"observed1"`) {
		t.Fatalf("the second preparation of the same request says what it keeps of the first: %s", auditJSON(second["carried"]))
	}
	done, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Checked the totals.", "checked": "Compared the totals.", "evidence": []any{"observed1"}}})
	if err != nil || done["complete"] != true {
		t.Errorf("the second receipt cites observed1, the observation the first receipt saved: %v complete=%v errors=%s", err, done["complete"], auditJSON(done["errors"]))
	}
	// negative: a request that was not interrupted and not prepared again takes nothing over
	if stopped, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop passes: %s", stopped)
	}
	if next := f.prepare(f.ask("Now rename the export button, a new task."), "export button"); next["carried"] != nil {
		t.Errorf("a new request after the Stop let the turn end takes the references over: %s", auditJSON(next["carried"]))
	}
}

// The same request prepared again keeps the note of every report of the earlier preparation, also of a result a later result
// replaced: it is neither the saved result nor one that waits for evidence.
func TestAuditCAReplacedReportAfterThePreparationAgain(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	r1, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}},
		"result": map[string]any{"changed": "Checked the totals.", "checked": "Compared the totals.", "evidence": []any{"new1"}}})
	if err != nil || r1["complete"] != true {
		t.Fatalf("precondition: the first result is recorded: %v %.300s", err, auditJSON(r1))
	}
	id1, _ := r1["storage"].(map[string]any)["result_source"].(string)
	r2, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Checked the totals and the filters.", "checked": "Compared both.", "evidence": []any{"observed1"}}})
	if err != nil || r2["complete"] != true || id1 == "" || r2["storage"].(map[string]any)["result_source"] == id1 {
		t.Fatalf("precondition: a second result replaces the first: %v %.300s", err, auditJSON(r2))
	}
	second := f.prepare(ids, "CSV export filters")
	later, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Checked the totals, the filters and the button.", "evidence": []any{id1}}})
	if err != nil {
		t.Fatal(err)
	}
	if raw := auditJSON(later["errors"]); later["complete"] == true || !strings.Contains(raw, "a report this work wrote about itself") {
		t.Errorf("after the same request was prepared again, a result with nothing observed cites the first, replaced report (%s) and is not refused: complete=%v errors=%s", id1, later["complete"], raw)
	}
}

// A session record written before reports were noted (0.30.8) has none. When its request is prepared again, the saved result and
// the reports that wait for their evidence are noted from the record itself.
func TestAuditCReportsOfARecordWrittenBeforeTheyWereNoted(t *testing.T) {
	f := newAuditFlow(t)
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	a, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"observed1"}}})
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := a["storage"].(map[string]any)["result_source"].(string)
	if _, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button.", "checked": "Looked at it.", "evidence": []any{"observed1"}}}); err != nil || idA == "" {
		t.Fatalf("precondition: two reports wait for their evidence: %v", err)
	}
	if err := updateSession(f.root, "planning", func(s *hookSession) { s.Planning.Work.Reports = nil }); err != nil {
		t.Fatal(err)
	}
	second := f.prepare(ids, "CSV export filters")
	if carried, _ := second["carried"].(map[string]any); carried != nil && strings.Contains(auditJSON(carried["sources"]), "observed") {
		t.Errorf("a record without the note of its reports hands them over as observations: %s", auditJSON(carried["sources"]))
	}
	repaired, err := f.record(second, map[string]any{"result": map[string]any{"ref": idA, "evidence": []any{"result"}}})
	if err == nil && repaired["complete"] == true {
		t.Errorf("a record without the note of its reports was prepared again, and report A is complete with report B (result) as its evidence: %.300s", auditJSON(repaired["saved"]))
	}
}

// audit C, finding A-4 (INVARIANTS 10, last sentence): a receipt without hook ids is not offered current in how_to_record (B-I1).
// The same preparation with a reply that had to be shortened.
func TestAuditCStandaloneHowToRecordWhenTheReplyIsShortened(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", 12)
		applies := fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 12)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("CSV export filters %d", i), Lesson: "Keep the filters when exporting CSV.", AppliesTo: applies, Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ name, input string }{
		{"a reply that fits (max_bytes 393216)", `{"query":"CSV export filters","request":"Check the CSV export","max_bytes":393216}`},
		{"the CLI's own default (no max_bytes)", `{"query":"CSV export filters","request":"Check the CSV export"}`},
		{"a reply that is shortened (max_bytes 8192)", `{"query":"CSV export filters","request":"Check the CSV export","max_bytes":8192}`},
	} {
		var out string
		withStdin(t, c.input, func() { out = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
		var reply map[string]any
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			t.Fatal(err)
		}
		how, _ := reply["how_to_record"].(string)
		t.Logf("%-45s %6d bytes, limited=%v, how_to_record offers current: %v", c.name, len(out), reply["output"] != nil, strings.Contains(how, "(current,"))
		if strings.Contains(how, "(current,") {
			t.Errorf("INVARIANT 10: %s: a receipt without hook ids is told to cite current (evidence takes references (current, ...)), which its record refuses", c.name)
		}
	}
}

// A trial is evidence a result can cite, so a report is no evidence of a trial either (audit E of 0.31.0, an unconfirmed doubt:
// the way round through a trial was open).
func TestAuditEATrialCannotCiteAReport(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	saved, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The totals match.")},
		"result": map[string]any{"changed": "Checked the totals.", "checked": "Compared the totals.", "evidence": []string{"new1"}}})
	if err != nil || saved["storage"].(map[string]any)["result_source"] == "" {
		t.Fatalf("precondition: the result is saved: %v %.300s", err, auditJSON(saved))
	}
	planned, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"plan": trialPlan("small", "result")}})
	if raw := auditJSON(planned) + fmt.Sprint(err); !strings.Contains(raw, "wrote about itself") {
		t.Errorf("a trial plan cites the result of this work as its evidence and is not refused: %.400s", raw)
	}
	// negative: the observation the work saved is evidence of a trial
	kept, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"plan": trialPlan("small", "observed1")}})
	if raw := auditJSON(kept) + fmt.Sprint(err); strings.Contains(raw, "wrote about itself") {
		t.Errorf("a trial plan citing a saved observation is refused as if it cited a report: %.400s", raw)
	}
}
