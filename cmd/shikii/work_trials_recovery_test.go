package main

import (
	"errors"
	"testing"
)

func TestWorkReasoningHistoryRestoresTrialReview(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "challenges", "observed": "The synthetic owner condition failed.", "evidence": []string{"new1"}}}, map[string]any{"observations": []any{trialObservation("Owner B remained in the synthetic result.")}})
	update := reasoningInput("update", ref)
	old := workAfterWrite
	defer func() { workAfterWrite = old }()
	workAfterWrite = func(kind string) error {
		if kind == "reasoning-history" {
			return errors.New("synthetic interruption after immutable history")
		}
		return nil
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": update})
	workAfterWrite = old
	if err != nil || v["complete"] != false {
		t.Fatalf("failed to reach the history interruption: %v %v", v, err)
	}
	v, err = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": update})
	if err != nil || v["complete"] != true {
		t.Fatal(v, err)
	}
	if !loadSession(root, "reasoning").Reasoning.Trials[ref].Reviewed {
		t.Fatal("successful update retry restored the judgment but lost its trial review")
	}
}

func TestWorkReasoningHistoryRestoresReadingAndSelection(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("large", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	update := reasoningInput("update", "current")
	update["depth"], update["next_trial"] = "brief", ref
	old := workAfterWrite
	defer func() { workAfterWrite = old }()
	workAfterWrite = func(kind string) error {
		if kind == "reasoning-history" {
			return errors.New("synthetic interruption after immutable history")
		}
		return nil
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": update})
	workAfterWrite = old
	if err != nil || v["complete"] != false {
		t.Fatalf("failed to reach the history interruption: %v %v", v, err)
	}
	v, err = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": update})
	if err != nil || v["complete"] != true {
		t.Fatal(v, err)
	}
	r := loadSession(root, "reasoning").Reasoning
	if r.AttentionDepth != "brief" || r.SelectedTrial != ref {
		t.Fatalf("successful update retry lost reading/selection state: depth=%q selected=%q", r.AttentionDepth, r.SelectedTrial)
	}
}
