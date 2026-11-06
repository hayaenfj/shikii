package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func trialPlan(effort string, evidence ...string) map[string]any {
	return map[string]any{"case": "csv-owner-v1", "question": "Does the export retain its owner condition?", "expected": "Only the requested owner's rows remain.", "method": "Run the same two-owner CSV fixture.", "conditions": "Synthetic CSV, two owners, UTF-8", "decision": "If another owner's row remains, repair the filter before reuse.", "effort": effort, "evidence": evidence}
}

func TestWorkTrialReplayAfterPrepareDoesNotReopenReview(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "Only owner A remained.", "evidence": []string{"new1"}}}, map[string]any{"observations": []any{trialObservation("The same synthetic owner fixture passed.")}})
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", ref), nil)
	before := loadSession(root, "reasoning").Reasoning.Generation
	p = reasoningHistoryPrepare(t, root, b)
	v := trialRecord(t, b, p["receipt"], map[string]any{"ref": ref}, nil)
	if loadSession(root, "reasoning").Reasoning.Generation != before || loopObject(t, v["reasoning"])["complete"] != true {
		t.Fatal("retrying the same saved execution after prepare must not invent new work evidence")
	}
}

func TestWorkTrialInterruptedCaptureRecoversOriginalResult(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	old := workAfterWrite
	defer func() { workAfterWrite = old }()
	calls := 0
	workAfterWrite = func(kind string) error {
		if kind == "observation" {
			calls++
			if calls == 2 {
				return errors.New("synthetic interrupted trial result")
			}
		}
		return nil
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The original execution contained an unexpected owner.")}, "trial": map[string]any{"ref": ref, "result": map[string]any{"assessment": "challenges", "observed": "Original failed owner condition.", "evidence": []string{"new1"}}}})
	workAfterWrite = old
	if err != nil || v["complete"] != false || calls != 2 {
		t.Fatalf("the execution capture must actually be interrupted: %v %v %d", v, err, calls)
	}
	p = reasoningHistoryPrepare(t, root, b)
	if v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": reasoningInput("finish", "current")}); err == nil && v["complete"] == true {
		t.Fatal("an interrupted execution cannot be skipped")
	}
	v = trialRecord(t, b, p["receipt"], map[string]any{"ref": ref}, nil)
	state := loadSession(root, "reasoning").Reasoning.Trials[ref]
	if state.ResultSource == "" || state.Result.Observed != "Original failed owner condition." || state.Reviewed {
		t.Fatal("reference-only recovery must retain the original observation and outstanding discrepancy")
	}
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", ref), nil)
}

func trialRecord(t *testing.T, b notebook.Book, receipt any, trial map[string]any, extra map[string]any) map[string]any {
	t.Helper()
	in := map[string]any{"receipt": receipt, "trial": trial}
	for key, value := range extra {
		in[key] = value
	}
	v, err := workCall(t, b, "record", in)
	if err != nil || v["complete"] != true {
		t.Fatalf("trial was not saved: %v %v", v, err)
	}
	return v
}

func trialSavedRef(t *testing.T, v map[string]any, kind string) string {
	t.Helper()
	for _, raw := range v["saved"].([]any) {
		item := loopObject(t, raw)
		if item["kind"] == kind {
			ref, _ := item["ref"].(string)
			if ref != "" {
				return ref
			}
		}
	}
	t.Fatalf("missing saved %s reference: %v", kind, v)
	return ""
}

func trialObservation(result string) map[string]any {
	return map[string]any{"kind": "test", "title": "Synthetic CSV execution", "content": result}
}

func TestWorkTrialExpectationResultRequiresJudgment(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	result := trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "challenges", "observed": "A row owned by the other owner remained.", "evidence": []string{"new1"}, "measurements": map[string]any{"missed_conditions": 1}}}, map[string]any{"observations": []any{trialObservation("Requested owner A; observed owners A and B.")}})
	if v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": reasoningInput("finish", "current")}); err == nil && v["complete"] == true {
		t.Fatal("an unreviewed discrepancy must not be hidden by an unrelated ending review")
	}
	update := reasoningInput("update", ref)
	update["assessment"], update["next_action"] = "revise", "Repair the owner condition before reusing the export."
	saveReasoning(t, b, p["receipt"], update, nil)
	end := reasoningInput("finish", ref)
	end["assessment"], end["result"] = "unresolved", "The observed failure is retained with its original conditions; no repair is claimed."
	finished := saveReasoning(t, b, p["receipt"], end, nil)
	if loopObject(t, finished["reasoning"])["complete"] != true {
		t.Fatal("a cited discrepancy must be settled as a review, even when its substantive result remains unresolved")
	}
	data, _ := json.Marshal(loadSession(root, "reasoning").Reasoning)
	if !strings.Contains(string(data), "other owner") || !strings.Contains(string(data), "missed_conditions") {
		t.Fatalf("the plan and observation must survive with the turn: %s", data)
	}
	if trialSavedRef(t, result, "trial_result") != ref {
		t.Fatal("the actual result must remain linked to the same immutable plan")
	}
}

func TestWorkTrialSurvivesPreparationAndSelectsSmallCheck(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	large := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("large", "current")}, nil)
	largeRef := trialSavedRef(t, large, "trial")
	small := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	smallRef := trialSavedRef(t, small, "trial")
	next := loopObject(t, loopObject(t, small["reasoning"])["next_trial"])
	if next["ref"] != smallRef {
		t.Fatalf("prefer the smaller unresolved distinguishing check: %v", next)
	}
	selectLarge := reasoningInput("update", "current")
	selectLarge["next_trial"] = largeRef
	saveReasoning(t, b, p["receipt"], selectLarge, nil)
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV"})
	if err != nil || loopObject(t, loopObject(t, prepared["reasoning"])["next_trial"])["ref"] != largeRef {
		t.Fatalf("the selected check must survive a new preparation: %v %v", prepared, err)
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"ref": smallRef}}); err == nil {
		t.Fatal("an old receipt cannot act on a restored trial")
	}
	selectLarge["next_trial"] = "trial:unavailable"
	if v, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "reasoning": selectLarge}); err == nil && v["complete"] == true {
		t.Fatal("a proposed next check must be one of the current unresolved trials")
	}
}

func TestWorkTrialComparisonPreservesMatchedCases(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := []string{}
	for i, rows := range []string{"owner,value\nA,one\nB,two\n", "owner,value\nA,one\n"} {
		plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "new1")}, map[string]any{"observations": []any{trialObservation([]string{"Before: original filter", "After: repaired filter"}[i])}})
		ref := trialSavedRef(t, plan, "trial")
		refs = append(refs, ref)
		missed := strings.Count(rows, "B,")
		trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": []string{"challenges", "supports"}[i], "observed": "The owner fixture was executed.", "evidence": []string{"new1"}, "measurements": map[string]any{"missed_conditions": missed}}}, map[string]any{"observations": []any{trialObservation(rows)}})
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"before": refs[0], "after": refs[1], "reason": "Compare the same owner fixture under the two recorded procedures."}})
	if err != nil || v["complete"] != true {
		t.Fatalf("matched execution comparison was not saved: %v %v", v, err)
	}
	data, _ := json.Marshal(v)
	if !strings.Contains(string(data), `"delta":-1`) || !strings.Contains(string(data), "missed_conditions") {
		t.Fatalf("the comparison must expose the paired values without inventing a success label: %s", data)
	}
	for _, pair := range [][]string{{refs[0], refs[0]}, {refs[0], "missing"}} {
		if v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"before": pair[0], "after": pair[1], "reason": "Check the same input."}}); err == nil && v["complete"] == true {
			t.Fatalf("invalid comparison pair was accepted: %v", pair)
		}
	}
}
