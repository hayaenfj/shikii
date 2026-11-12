package main

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Audit M of 0.31.0 (after the redesign into types): the ways a work's own report still reaches evidence.

// auditMRecordFn records with the receipt of one preparation.
func auditMRecordFn(t *testing.T, b notebook.Book, receipt any) func(map[string]any) (map[string]any, error) {
	return func(fields map[string]any) (map[string]any, error) {
		fields["receipt"] = receipt
		return workCall(t, b, "record", fields)
	}
}

// auditMSourceContent reads a saved source as `wiki query <id>` shows it to the agent.
func auditMSourceContent(t *testing.T, b notebook.Book, id string) notebook.Observation {
	t.Helper()
	v, err := b.ReadSources([]string{id})
	if err != nil {
		t.Fatalf("precondition: read the source %s: %v", id, err)
	}
	list, _ := v["observations"].([]notebook.Observation)
	if len(list) != 1 {
		t.Fatalf("precondition: the source %s is one observation: %v", id, v)
	}
	return list[0]
}

// INVARIANT 10 (type side): "an observation never takes a title of a work source" (CaptureObserved refuses it). checkWorkSource
// finds the capture memo by workHash(o), which is the JSON of workObservation; the field source (the kind) is unexported and not
// in the JSON. An observation the agent sends with the kind, title, content, reference and version of the receipt's earlier result
// (the agent reads the content with wiki query <result_source>; reference and version are the defaults) hashes to the memo of that
// result: it takes memo.Source, is checked with Validate instead of ValidateObserved, and comes back as the id of the report. As
// new1 it is cited as evidence, and selfEvidence looks at the name "new1" (no source of that name), so the report passes.
func TestAuditMAnObservationTakesTheMemoOfTheResult(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	record := auditMRecordFn(t, b, v["receipt"])
	report := auditKResultID(t, v["receipt"], func(f map[string]any) (map[string]any, error) { return workCall(t, b, "record", f) })
	saved := auditMSourceContent(t, b, report)
	second, err := record(map[string]any{
		"observations": []any{map[string]any{"kind": "agent", "title": notebook.WorkResult().Title(), "content": saved.Content}},
		"result":       map[string]any{"changed": "Validated the Quartz exports; checked.", "sufficient": true, "evidence": []string{"new1"}},
	})
	t.Logf("report %s; second record: err=%v complete=%v saved=%.500s errors=%.500s", report, err, second["complete"], auditJSON(second["saved"]), auditJSON(second["errors"]))
	if err != nil {
		return
	}
	for _, item := range asList(second["saved"]) {
		if m, _ := item.(map[string]any); m["kind"] == "observation" && m["id"] == report {
			t.Errorf("INVARIANT 10: an observation the agent sent under the title %q was not refused: it came back as the id of the work's own report %s", notebook.WorkResult().Title(), report)
		}
	}
	if second["complete"] == true {
		t.Errorf("INVARIANT 10: a result whose only evidence is the work's own earlier report (as new1) is complete and sufficient")
	}
}

// The refusal of selfEvidence says "cite what you checked (...), or leave evidence out". A result that cites a report is saved
// first (captureWorkCompletion saves it, then checks its evidence), so it stays a pending result; the agent that follows the
// refusal and sends the result again without the evidence gets a new result, and the reply still has the first one pending.
func TestAuditMFollowingTheRefusalLeavesAPendingResult(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	record := auditMRecordFn(t, b, v["receipt"])
	report := auditKResultID(t, v["receipt"], func(f map[string]any) (map[string]any, error) { return workCall(t, b, "record", f) })
	refused, err := record(map[string]any{"result": map[string]any{"changed": "Validated the Quartz exports again.", "evidence": []string{report}}})
	t.Logf("citing the report %s: err=%v complete=%v errors=%.400s", report, err, refused["complete"], auditJSON(refused["errors"]))
	if err != nil || refused["complete"] == true {
		t.Fatalf("precondition: the report is refused as evidence")
	}
	followed, err := record(map[string]any{"result": map[string]any{"changed": "Validated the Quartz exports again."}})
	reasoning := loopObject(t, followed["reasoning"])
	t.Logf("evidence left out, as the refusal says: err=%v complete=%v storage=%v reasoning.complete=%v pending_results=%v next=%.300s", err, followed["complete"], auditJSON(followed["storage"]), reasoning["complete"], auditJSON(reasoning["pending_results"]), reasoning["next"])
	if err != nil || followed["complete"] != true || reasoning["pending_results"] != nil || reasoning["complete"] != true {
		t.Errorf("the record sent as the refusal says (evidence left out) does not close the work: the refused result stays pending")
	}
}

// The same with the first way the refusal names: cite an observation of this call as new1.
func TestAuditMFollowingTheRefusalWithNew1LeavesAPendingResult(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	record := auditMRecordFn(t, b, v["receipt"])
	report := auditKResultID(t, v["receipt"], func(f map[string]any) (map[string]any, error) { return workCall(t, b, "record", f) })
	if refused, err := record(map[string]any{"result": map[string]any{"changed": "Validated the Quartz exports again.", "evidence": []string{report}}}); err != nil || refused["complete"] == true {
		t.Fatalf("precondition: the report is refused as evidence")
	}
	followed, err := record(map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Quartz export check", "content": "The synthetic export matched."}},
		"result": map[string]any{"changed": "Validated the Quartz exports again.", "evidence": []string{"new1"}}})
	reasoning := loopObject(t, followed["reasoning"])
	t.Logf("new1 cited, as the refusal says: err=%v complete=%v storage=%v pending_results=%v next=%.200s", err, followed["complete"], auditJSON(followed["storage"]), auditJSON(reasoning["pending_results"]), reasoning["next"])
	if err != nil || followed["complete"] != true || reasoning["pending_results"] != nil {
		t.Errorf("the record sent as the refusal says (an observation as new1) does not close the work: the refused result stays pending")
	}
	// what the reply then asks: repair the pending result with its ref
	pending := ""
	for id := range loopObject(t, reasoning["pending_results"]) {
		pending = id
	}
	if pending != "" {
		observed := ""
		for _, item := range asList(followed["saved"]) {
			if m, _ := item.(map[string]any); m["kind"] == "observation" {
				observed, _ = m["ref"].(string)
			}
		}
		repaired, err := record(map[string]any{"result": map[string]any{"ref": pending, "evidence": []string{observed}}})
		t.Logf("the repair the reply asks for (evidence %s, the observation of the call before): err=%v complete=%v errors=%.300s", observed, err, repaired["complete"], auditJSON(repaired["errors"]))
	}
}
