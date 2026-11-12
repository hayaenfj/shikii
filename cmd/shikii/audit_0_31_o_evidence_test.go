package main

import (
	"regexp"
	"strings"
	"testing"
)

// Audit O of 0.31.0: the fix of M-U4/N-I1 runs selfEvidence before the result is saved. bindWorkEvidence (the resolution of the
// references) still runs after the save: a result whose evidence names a reference that does not resolve is saved, refused and
// left pending, and the agent that follows the refusal with a reworded result (the same work, other words) leaves the first one
// pending: the work does not close. GREEN control: the same flow with the citation of a report (refused before the save) closes.
func TestAuditOAnUnresolvedReferenceLeavesAPendingResult(t *testing.T) {
	run := func(t *testing.T, evidence, again string) (map[string]any, map[string]any) {
		b, _ := loopBook(t)
		v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
		if err != nil {
			t.Fatal(err)
		}
		record := auditMRecordFn(t, b, v["receipt"])
		report := auditKResultID(t, v["receipt"], func(f map[string]any) (map[string]any, error) { return workCall(t, b, "record", f) })
		if evidence == "report" {
			evidence = report
		}
		refused, err := record(map[string]any{"result": map[string]any{"changed": "Validated the Quartz exports again.", "evidence": []string{evidence}}})
		if err != nil || refused["complete"] == true {
			t.Fatalf("precondition: the evidence %s is refused: %v %.300s", evidence, err, auditJSON(refused))
		}
		// as the refusal says: a refusal after the save names the saved result to repair by its ref; one before the save asks for
		// the record again
		result := map[string]any{"changed": again, "evidence": []string{"new1"}}
		if m := regexp.MustCompile(`send result:\{ref:\\?"([0-9a-f]{16})`).FindStringSubmatch(auditJSON(refused["errors"])); m != nil {
			result = map[string]any{"ref": m[1], "evidence": []string{"new1"}}
		}
		followed, err := record(map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Quartz export check", "content": "The synthetic export matched."}},
			"result": result})
		if err != nil {
			t.Fatal(err)
		}
		return refused, followed
	}
	t.Run("control: a report cited (refused before the save)", func(t *testing.T) {
		_, followed := run(t, "report", "Validated the Quartz exports again, with the check.")
		if followed["complete"] != true {
			t.Fatalf("control: %.400s", auditJSON(followed))
		}
	})
	for name, again := range map[string]string{"the same words": "Validated the Quartz exports again.", "other words": "Validated the Quartz exports again, with the check."} {
		t.Run("a reference that does not resolve, sent again in "+name, func(t *testing.T) {
			refused, followed := run(t, "observed9", again)
			reasoning := loopObject(t, followed["reasoning"])
			t.Logf("refusal: %.400s", auditJSON(refused["errors"]))
			t.Logf("followed: complete=%v storage=%s pending_results=%s next=%.200v", followed["complete"], auditJSON(followed["storage"]), auditJSON(reasoning["pending_results"]), reasoning["next"])
			if !strings.Contains(auditJSON(refused["errors"]), "send result:{ref:") {
				t.Errorf("the refusal after the save does not say how the saved result is repaired: %.300s", auditJSON(refused["errors"]))
			}
			if followed["complete"] != true {
				t.Errorf("the record sent as the refusal says (the observation added, the saved result repaired by its ref) does not close the work")
			}
		})
	}
}

// A guard for the case of selfEvidence that refuses "result" in the record of a result before the result is saved (the fix of
// M-U4/N-I1): without it the first record citing "result" is saved, refused after the save as "this result itself", and left
// pending, so the record sent again as the refusal says (evidence left out) does not close the work. No shipped test cites "result"
// in a result (_audit_o/mutations.log, E7). GREEN on the candidate.
func TestAuditOAResultCitingResultIsRefusedBeforeTheSave(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	record := auditMRecordFn(t, b, v["receipt"])
	refused, err := record(map[string]any{"result": map[string]any{"changed": "Validated the Quartz exports.", "evidence": []string{"result"}}})
	if err != nil || refused["complete"] == true {
		t.Fatalf("precondition: a result citing result is refused: %v %.300s", err, auditJSON(refused))
	}
	t.Logf("refusal: %.300s", auditJSON(refused["errors"]))
	followed, err := record(map[string]any{"result": map[string]any{"changed": "Validated the Quartz exports."}})
	reasoning := loopObject(t, followed["reasoning"])
	if err != nil || followed["complete"] != true || reasoning["pending_results"] != nil {
		t.Errorf("the record sent again as the refusal says (evidence left out) does not close the work: complete=%v pending_results=%s", followed["complete"], auditJSON(reasoning["pending_results"]))
	}
}

// The new refusal of an outcome that cites a result with no evidence of its own (the fix of N-U2) says "record the result with the
// evidence it rests on first". Followed in the next call (an observation, the result citing it, and the same outcome citing result),
// the work closes. GREEN check of the wording.
func TestAuditOTheOutcomeRefusalIsFollowedAsItSays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Invoice rounding check", "lesson": "Run the invoice fixture.", "applies_to": "invoice rounding", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "invoice rounding", "request": "Run the invoice rounding check"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	outcome := map[string]any{"ref": ref, "conditions": "two lines", "action": "Ran the fixture.", "result": "Rounded per line.", "sources": []string{"result"}}
	got, _ := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Ran the invoice rounding check."}, "outcomes": []any{outcome}})
	raw := auditJSON(got["errors"])
	if !strings.Contains(raw, "record the result with the evidence it rests on first") {
		t.Fatalf("precondition: the outcome is refused with the new words: %.400s", raw)
	}
	again, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"],
		"observations": []any{map[string]any{"kind": "test", "title": "Invoice fixture", "content": "Two lines rounded per line."}},
		"result":       map[string]any{"changed": "Ran the invoice rounding check.", "checked": "Ran the fixture.", "evidence": []string{"new1"}},
		"outcomes":     []any{outcome}})
	t.Logf("followed: err=%v complete=%v errors=%.400s", err, again["complete"], auditJSON(again["errors"]))
	if err != nil || strings.Contains(auditJSON(again["errors"]), `"kind":"outcome"`) {
		t.Errorf("the outcome is still refused after the result was recorded with its evidence, as the refusal says")
	}
}
