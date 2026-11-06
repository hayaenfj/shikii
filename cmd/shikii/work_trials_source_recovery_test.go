package main

import (
	"errors"
	"testing"
)

func TestWorkTrialPublishedResultBindingRecoversWithoutRecapture(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	observed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("Only owner A remained in the synthetic export.")}})
	if err != nil || observed["complete"] != true {
		t.Fatal(observed, err)
	}
	var evidence string
	for _, raw := range observed["saved"].([]any) {
		item := loopObject(t, raw)
		if item["kind"] == "observation" {
			evidence = item["ref"].(string)
		}
	}
	token, _ := workDecode(p["receipt"].(string))
	dir, err := openWorkSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, name, err := loadWorkState(dir, b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	save := func() error {
		if !failed && w.Reasoning.Trials[ref].ResultSource != "" {
			failed = true
			return errors.New("synthetic failure while saving the confirmed result source binding")
		}
		return saveWorkState(dir, name, b.Scope, token.Turn, w, false)
	}
	commit := workCommitter(func(action func() error) error { return commitWorkTurn(dir, name, b.Scope, token, action) })
	resolve := func(refs []string) ([]string, error) { return []string{w.Sources[evidence]}, nil }
	_, err = recordWorkTrial(b, w, workTrialRequest{Ref: ref, Result: &workTrialResult{Assessment: "supports", Observed: "Only owner A remained.", Evidence: []string{evidence}}}, resolve, save, commit)
	if err == nil || !failed {
		t.Fatal("the binding checkpoint was not interrupted", err)
	}
	original := w.Reasoning.Trials[ref].ResultSource
	onDisk := loadSession(root, "reasoning").Reasoning
	if original == "" || onDisk.Trials[ref].ResultSource != "" || len(onDisk.PendingCaptures) != 0 {
		t.Fatal("the confirmed-capture/unconfirmed-binding boundary was not reached")
	}
	prepared := reasoningHistoryPrepare(t, root, b)
	trialRecord(t, b, prepared["receipt"], map[string]any{"ref": ref}, nil)
	recovered := loadSession(root, "reasoning").Reasoning.Trials[ref].ResultSource
	if recovered != original {
		t.Fatal("the original confirmed trial execution was recaptured under a different source identity")
	}
}
