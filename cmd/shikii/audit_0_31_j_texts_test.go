package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Audit J of 0.31.0 (fifth round): the refusals added in this round, followed as they are written.

// The refusal of an observation titled like one of shikii's reports says "give the observation a title of its own". Sent again
// that way, in the same record as a result that cites it, the record is complete.
func TestAuditJObservationTitleRefusalFollowed(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	refused, err := f.record(first, map[string]any{"observations": []any{map[string]any{"title": notebook.WorkResult.Title(), "content": "Ran make test: 412 passed."}},
		"result": map[string]any{"changed": "Edited the export.", "checked": "Ran make test.", "evidence": []any{"new1"}}})
	raw := auditJSON(refused["errors"])
	t.Logf("refused: err=%v complete=%v errors=%.500s", err, refused["complete"], raw)
	if err != nil || refused["complete"] == true || !strings.Contains(raw, "give the observation a title of its own") {
		t.Fatalf("precondition: the observation is refused for its title: %v %s", err, raw)
	}
	again, err := f.record(first, map[string]any{"observations": []any{map[string]any{"title": "make test after the export change", "content": "Ran make test: 412 passed."}},
		"result": map[string]any{"changed": "Edited the export.", "checked": "Ran make test.", "evidence": []any{"new1"}}})
	t.Logf("sent as told: err=%v complete=%v errors=%.400s", err, again["complete"], auditJSON(again["errors"]))
	if err != nil || again["complete"] != true {
		t.Errorf("the record sent as the refusal says is not complete: %v %.400s", err, auditJSON(again["errors"]))
	}
}

// An archive shikii keeps of a work's state ("Pending wiki work before replacing preparation") that waits among the pending
// observations is noted as a report of the work (w.Reports), and citing it is refused with the words of that note: "a report
// this work wrote about itself (an earlier result or an ending review)". The words written for archives in this round ("an
// archive of the work's state") are only in the other branch (workReportSource), which an archive listed this way never reaches.
func TestAuditJArchiveRefusalNamesWhatItIs(t *testing.T) {
	f := newAuditFlow(t)
	content, _ := json.Marshal(map[string]any{"work": map[string]any{"result": map[string]any{"changed": "Edited the export.", "sufficient": true}}})
	archive, err := f.b.CaptureWork(notebook.WorkPending, string(content), "work preparation replacement for turn 0123456789abcdef", "work-preparation", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	prepared := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	ref := ""
	for _, raw := range prepared["observations"].([]any) {
		o := raw.(map[string]any)
		if strings.Contains(auditJSON(o["source"]), archive.ID) {
			ref, _ = o["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("precondition: the archive is listed among the observations: %.300s", auditJSON(prepared["observations"]))
	}
	done, err := f.record(prepared, map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Nothing new.", "evidence": []any{ref}}})
	raw := auditJSON(done["errors"])
	t.Logf("the archive as %s: err=%v complete=%v errors=%.500s", ref, err, done["complete"], raw)
	if done["complete"] == true {
		t.Fatalf("a result citing the archive is complete")
	}
	if strings.Contains(raw, "an earlier result or an ending review") && !strings.Contains(raw, "archive") {
		t.Errorf("the refusal calls the archive %q an earlier result or an ending review: %.400s", "Pending wiki work before replacing preparation", raw)
	}
}

// What a bare delivery of a corrected lesson looks like in the JSON the agent reads: the correction keeps its source_id, and
// its quote and action come as empty strings.
func TestAuditJBareCorrectionAsTheAgentReadsIt(t *testing.T) {
	s := reflectSource{Memories: []store.MemoryObservation{{SourceID: "s1", Quote: store.MemoryQuote{Key: "lesson:1:1", ID: "1", Revision: "1", Correction: &store.Correction{SourceID: "0123456789abcdef", Quote: "Still in English.", Action: "Write it in Japanese."}}}}}
	raw, _ := json.Marshal(reflectBare(s).Memories[0])
	t.Logf("a bare delivery: %s", raw)
	if strings.Contains(string(raw), `"quote":""`) || strings.Contains(string(raw), `"action":""`) {
		t.Errorf("a bare delivery of a correction reads as a correction whose words are empty: %s", raw)
	}
}

// INVARIANT 10 lets evidence cite the records of what the agent did (a trial, a measured reading, a procedure it executed). A
// result that cites the source of a trial plan is taken. No shipped test holds this side of workSourceTitles: with every title
// in the table treated as a report (workReport without its citable check), the shipped tests all pass.
func TestAuditJATrialIsEvidenceOfAResult(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	id := ""
	for _, raw := range plan["saved"].([]any) {
		if item, ok := raw.(map[string]any); ok && item["kind"] == "trial" {
			id, _ = item["id"].(string)
		}
	}
	if id == "" {
		t.Fatalf("precondition: the trial plan is saved as a source: %.400s", auditJSON(plan))
	}
	got, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the totals.", "checked": "Planned and ran the trial.", "evidence": []string{id}}})
	raw := auditJSON(got["errors"]) + " " + errString(err)
	t.Logf("a result citing the trial plan %s: complete=%v %.300s", id, got["complete"], raw)
	if strings.Contains(raw, "wrote about") {
		t.Errorf("INVARIANT 10: a result citing the source of a trial plan is refused as a report: %.300s", raw)
	}
}

// INVARIANT 10: the refusal of citing a report (selfEvidence) offers current only to a receipt with hook ids. No shipped test
// holds it for a receipt without them: with "or current" added for every receipt (work_completion.go:58), they all pass.
func TestAuditJSelfEvidenceRefusalOnAStandaloneReceipt(t *testing.T) {
	f := newAuditFlow(t)
	prepared, err := workCall(t, f.b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := f.record(prepared, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Button", "content": "The button reads Export."}},
		"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"new1"}}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: %v %.300s", err, auditJSON(done))
	}
	refused, err := f.record(prepared, map[string]any{"result": map[string]any{"changed": "Renamed the button and checked it.", "evidence": []any{"result"}}})
	raw := auditJSON(refused["errors"]) + " " + errString(err)
	t.Logf("citing the result on a receipt without hook ids: %.400s", raw)
	if !strings.Contains(raw, "cite what you checked") {
		t.Fatalf("precondition: selfEvidence refuses it: %s", raw)
	}
	if strings.Contains(raw, "or current") {
		t.Errorf("INVARIANT 10: the refusal offers current to a receipt without hook ids: %.400s", raw)
	}
}

// The source that binds a result to its evidence ("Recovered work result evidence") is no evidence (INVARIANT 10). No shipped
// test holds it: with notebook.WorkRecovery.Title() marked citable in workSourceTitles, every shipped test passes (TestAnObservationCannot...
// takes its answer from the same table). Written the way TestAuditGAnArchiveOfTheWorkIsEvidence writes an archive.
func TestAuditJTheRecoverySourceIsNoEvidence(t *testing.T) {
	f := newAuditFlow(t)
	content, _ := json.Marshal(map[string]any{"result": map[string]any{"changed": "Edited the export."}, "evidence": []string{"0123456789abcdef"}})
	recovery, err := f.b.CaptureWork(notebook.WorkRecovery, string(content), "result-recovery:0123456789abcdef", "work-recovery", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	prepared := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	ref := ""
	for _, raw := range prepared["observations"].([]any) {
		o := raw.(map[string]any)
		if strings.Contains(auditJSON(o["source"]), recovery.ID) {
			ref, _ = o["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("precondition: the recovery source is listed among the observations: %.300s", auditJSON(prepared["observations"]))
	}
	done, err := f.record(prepared, map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Nothing new.", "evidence": []any{ref}}})
	t.Logf("%s as %s: err=%v complete=%v errors=%.300s", notebook.WorkRecovery.Title(), ref, err, done["complete"], auditJSON(done["errors"]))
	if err == nil && done["complete"] == true {
		t.Errorf("INVARIANT 10: a result that observed nothing is complete with %s (%q) as its only evidence", ref, notebook.WorkRecovery.Title())
	}
}
