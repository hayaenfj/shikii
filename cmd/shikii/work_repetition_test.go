package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkRepetitionReusesDeliveredBodyAndKeepsRecovery(t *testing.T) {
	b, page := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover the source's deciding condition.", "")
	first := loopObject(t, p["knowledge"].([]any)[0])
	if first["body_included"] != true {
		t.Fatal("control: the complete body must have been delivered")
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
	if err != nil {
		t.Fatal(err)
	}
	entry := loopObject(t, v["knowledge"].([]any)[0])
	if entry["reused"] != true || entry["body_included"] != false || entry["article"] != nil || entry["ref"] != first["ref"] {
		t.Fatalf("reuse a complete unchanged body under the same receipt: %v", entry["reused"])
	}
	advice := loopObject(t, loopObject(t, v["attention"])["loop"])
	if advice["action"] != "reuse_previous" || advice["reused_body_bytes"].(float64) <= 0 {
		t.Fatal("return measured reuse and its next action", advice)
	}
	if loopObject(t, v["reading"])["mixed"] != true {
		t.Fatal("a different delivery procedure must not contaminate depth learning")
	}
	full, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
	if err != nil || loopObject(t, full["knowledge"].([]any)[0])["body_included"] != true {
		t.Fatal("an explicit recovery must always return the complete body", err)
	}
	data, _ := json.Marshal(full)
	if !strings.Contains(string(data), "Inspect the exact source revision") {
		t.Fatal("recovery lost source text")
	}
}

func TestWorkRepetitionSeparatesChangedDepthRevisionAndReceipt(t *testing.T) {
	b, page := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover a source condition.", "")
	explicit, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "deep"}})
	if err != nil || loopObject(t, explicit["knowledge"].([]any)[0])["body_included"] != true {
		t.Fatal("an explicit depth must retain requested evidence", err)
	}
	regressionChangeArticle(t, b, page, "updated")
	changed, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
	if err != nil {
		t.Fatal(err)
	}
	e := loopObject(t, changed["knowledge"].([]any)[0])
	if e["reused"] == true || e["revision"] == page.Revision || e["body_included"] != true {
		t.Fatal("a changed revision must be delivered", e)
	}
	fresh := readingLearningPrepare(t, b, "Recover a source condition.", "")
	if loopObject(t, fresh["knowledge"].([]any)[0])["reused"] == true {
		t.Fatal("another receipt must not inherit delivery claims")
	}
}

func TestWorkRepetitionDoesNotTreatExcerptsAsDeliveredBody(t *testing.T) {
	b, page := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover a source condition.", "brief")
	if loopObject(t, p["knowledge"].([]any)[0])["body_included"] != false {
		t.Fatal("control: excerpts only")
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
	if err != nil || loopObject(t, v["knowledge"].([]any)[0])["body_included"] != true {
		t.Fatal("unseen source text must remain recoverable", err)
	}
}

func TestWorkRepetitionSuggestsChangeAfterRepeatedSearchWithoutNewEvidence(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover a source condition.", "")
	var v map[string]any
	for i := 0; i < 2; i++ {
		var err error
		v, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
		if err != nil {
			t.Fatal(err)
		}
	}
	advice := loopObject(t, loopObject(t, v["attention"])["loop"])
	if advice["action"] != "change_approach" || advice["same_result_reads"] != float64(2) {
		t.Fatal("repeated unchanged searches need a different next action", advice)
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("A newly checked source condition changes this investigation.")}}); err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
	if err != nil || loopObject(t, loopObject(t, v["attention"])["loop"])["same_result_reads"] != float64(0) {
		t.Fatal("new evidence must reset the no-progress suggestion", err)
	}
}

func TestWorkRepetitionReportsRepeatedTrialResults(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := []string{}
	for _, input := range []string{"Original bounded check.", "Repeated bounded check."} {
		plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "new1")}, map[string]any{"observations": []any{trialObservation(input)}})
		ref := trialSavedRef(t, plan, "trial")
		refs = append(refs, ref)
		trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "inconclusive", "observed": "The same unresolved source condition remains.", "evidence": []string{"new1"}, "measurements": map[string]any{"unresolved_conditions": 1}}}, map[string]any{"observations": []any{trialObservation("The check did not resolve the condition.")}})
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "CSV"})
	if err != nil {
		t.Fatal(err)
	}
	advice := loopObject(t, loopObject(t, v["attention"])["loop"])
	if advice["action"] != "change_approach" || len(advice["repeated_trials"].([]any)) != 2 {
		t.Fatal("retain the exact reported trials and suggest another method", advice)
	}
	u := reasoningInput("update", refs...)
	u["assessment"], u["next_action"] = "revise", "Use a different distinguishing method."
	saveReasoning(t, b, p["receipt"], u, nil)
	v, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "CSV"})
	if err != nil || loopObject(t, loopObject(t, v["attention"])["loop"])["action"] == "change_approach" {
		t.Fatal("reviewing the exact results must settle the suggestion", err)
	}
}

func TestWorkRepetitionKeepsChangedPremiseReadableOnEveryRetry(t *testing.T) {
	b, page := readingLearningBook(t)
	path := filepath.Join(b.Scope, "context.txt")
	if err := os.WriteFile(path, []byte("CONTEXT_A"), 0600); err != nil {
		t.Fatal(err)
	}
	body, watch := page.Body, []string{"context.txt"}
	saved, err := b.Ingest(notebook.Draft{ID: page.ID, Title: page.Title, Lesson: page.Lesson, Body: &body, Kind: page.Kind, Scene: page.Scene, AppliesTo: page.AppliesTo, Sources: []string{page.Sources[0].ID}, Watch: &watch})
	if err != nil || saved.Page == nil {
		t.Fatal("watch setup", err)
	}
	p := readingLearningPrepare(t, b, "Recover the watched source condition.", "")
	if err := os.WriteFile(path, []byte("CONTEXT_B"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
		if err != nil {
			t.Fatal(err)
		}
		e := loopObject(t, v["knowledge"].([]any)[0])
		if e["reused"] == true || e["body_included"] != true || len(e["recheck"].([]any)) != 1 {
			t.Fatal("a changed premise must remain readable on every retry", i, e["reused"])
		}
	}
}

func TestWorkRepetitionKeepsSmallBodiesWhenReuseWouldCostMore(t *testing.T) {
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Small source", "body": "One checked condition.", "kind": "howto"})
	p, err := workCall(t, b, "prepare", map[string]any{"request": "Check a small source.", "query": "Small source"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Small source"})
	if err != nil || loopObject(t, v["knowledge"].([]any)[0])["reused"] == true {
		t.Fatal("reuse metadata must not replace a cheaper complete body", err)
	}
}
