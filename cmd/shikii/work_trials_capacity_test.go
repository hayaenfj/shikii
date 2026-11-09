package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestWorkTrialResultCapacityRetainsRequiredIntent(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	v := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, v, "trial")
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
	interimReadableBoundary(t, root, b)
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"ref": ref, "result": map[string]any{"assessment": "challenges", "observed": "A contrary owner row was found after the prior ending review.", "evidence": []string{"current"}}}})
	if err == nil && failed["complete"] == true {
		t.Fatal("capacity fixture did not reject the result")
	}
	state := loadSession(root, "reasoning")
	t.Logf("record_error=%v partial=%v result_retained=%v reasoning_complete=%v remaining=%d", err, failed["complete"], state.Reasoning.Trials[ref].Result != nil, reasoningView(state.Reasoning).Complete, reasoningLeft(state.Reasoning))
	if state.Reasoning.Trials[ref].Result == nil || reasoningView(state.Reasoning).Complete {
		t.Fatal("capacity rejection lost the challenged result and retained the previous complete ending review")
	}
}

func TestWorkTrialCaptureRecoveryHasReservedCapacity(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "observation" {
			return errors.New("synthetic interruption after trial plan publication")
		}
		return nil
	}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"plan": trialPlan("small", "current")}})
	workAfterWrite = old
	if err != nil || failed["complete"] != false {
		t.Fatal("capture interruption unavailable", failed, err)
	}
	state := loadSession(root, "reasoning")
	var ref string
	for key := range state.Reasoning.Trials {
		ref = key
	}
	if ref == "" {
		t.Fatal("pending plan missing")
	}
	if err := updateSession(root, "reasoning", func(s *hookSession) {
		for len(s.Planning.Work.Saved) < workSavedLimit {
			key := fmt.Sprintf("synthetic-reserved-%d", len(s.Planning.Work.Saved))
			s.Planning.Work.Saved[key] = workItem{Kind: "synthetic"}
		}
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"ref": ref}})
	t.Logf("recovery_error=%v response=%v", err, recovered)
	if err != nil || recovered["complete"] != true {
		t.Fatal("already-published trial recovery has no reserved saved-item slot")
	}
}

func TestWorkComparisonInterruptedCapturePreservesOriginal(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := []string{}
	for i := range 2 {
		plan := trialPlan("small", "current")
		plan["effort"] = []string{"small", "large"}[i]
		saved := trialRecord(t, b, p["receipt"], map[string]any{"plan": plan}, nil)
		ref := trialSavedRef(t, saved, "trial")
		refs = append(refs, ref)
		trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "Only requested-owner rows remained.", "evidence": []string{"current"}, "measurements": map[string]any{"foreign_rows_count": i}}}, nil)
	}
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "observation" {
			return errors.New("synthetic interrupted comparison")
		}
		return nil
	}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"before": refs[0], "after": refs[1], "reason": "Keep the paired owner condition."}})
	workAfterWrite = old
	if err != nil || failed["complete"] != false {
		t.Fatal(failed, err)
	}
	ref := ""
	for key := range loadSession(root, "reasoning").Reasoning.Comparisons {
		ref = key
	}
	p = reasoningHistoryPrepare(t, root, b)
	got, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"ref": ref}})
	if err != nil || got["complete"] != true {
		t.Fatal(got, err)
	}
	saved := loadSession(root, "reasoning").Reasoning.Comparisons[ref]
	if saved.Source == "" || !strings.Contains(saved.Reason, "paired owner") || saved.Values["foreign_rows_count"].Delta != 1 {
		t.Fatal("comparison recovery changed the original paired result")
	}
}

func TestWorkTrialCapacityPrecedesSourcePublication(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	if err := updateSession(root, "reasoning", func(s *hookSession) {
		for len(s.Planning.Work.Saved) < workSavedLimit {
			key := fmt.Sprintf("synthetic-reserved-%d", len(s.Planning.Work.Saved))
			s.Planning.Work.Saved[key] = workItem{Kind: "synthetic"}
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"plan": trialPlan("small", "current")}})
	if err == nil && failed["complete"] == true {
		t.Fatal("saved limit was not reached")
	}
	state := loadSession(root, "reasoning")
	for _, trial := range state.Reasoning.Trials {
		t.Logf("published_trial_source=%v", trial.Source != "")
	}
	if len(state.Reasoning.Trials) != 0 {
		t.Fatal("the saved-item refusal created a new trial and immutable source before checking capacity")
	}
}

func trialMatchedResults(t *testing.T, b notebook.Book, receipt any) []string {
	t.Helper()
	refs := []string{}
	for i := range 2 {
		plan := trialPlan("small", "current")
		plan["effort"] = []string{"small", "large"}[i]
		v := trialRecord(t, b, receipt, map[string]any{"plan": plan}, nil)
		ref := trialSavedRef(t, v, "trial")
		trialRecord(t, b, receipt, map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "Synthetic fixed owner result.", "evidence": []string{"current"}, "measurements": map[string]any{"foreign_rows_count": i}}}, nil)
		refs = append(refs, ref)
	}
	return refs
}

func TestWorkComparisonCapacityRetainsRequiredIntent(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := trialMatchedResults(t, b, p["receipt"])
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
	interimReadableBoundary(t, root, b)
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"before": refs[0], "after": refs[1], "reason": "This comparison must survive a session capacity refusal."}})
	if err == nil && failed["complete"] == true {
		t.Fatal("capacity refusal unavailable")
	}
	s := loadSession(root, "reasoning")
	t.Logf("comparisons=%d ending_complete=%v remaining=%d", len(s.Reasoning.Comparisons), reasoningView(s.Reasoning).Complete, reasoningLeft(s.Reasoning))
	if len(s.Reasoning.Comparisons) != 1 || reasoningView(s.Reasoning).Complete {
		t.Fatal("session capacity lost the comparison and retained the old ending review")
	}
}

func TestWorkComparisonCapacityPrecedesSourcePublication(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := trialMatchedResults(t, b, p["receipt"])
	if err := updateSession(root, "reasoning", func(s *hookSession) {
		for len(s.Planning.Work.Saved) < workSavedLimit {
			key := fmt.Sprintf("synthetic-reserved-%d", len(s.Planning.Work.Saved))
			s.Planning.Work.Saved[key] = workItem{Kind: "synthetic"}
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"before": refs[0], "after": refs[1], "reason": "No new comparison can be published when all recording slots are full."}})
	if err == nil && failed["complete"] == true {
		t.Fatal("saved limit unavailable")
	}
	s := loadSession(root, "reasoning")
	for _, c := range s.Reasoning.Comparisons {
		t.Logf("comparison_source_published=%v", c.Source != "")
	}
	if len(s.Reasoning.Comparisons) > 0 {
		t.Fatal("capacity refused after publishing the comparison")
	}
}

func TestWorkHistoricalComparisonAcrossTurnAndProject(t *testing.T) {
	repo, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := trialMatchedResults(t, b, p["receipt"])
	s := loadSession(root, "reasoning")
	a, z := s.Reasoning.Trials[refs[0]].ResultSource, s.Reasoning.Trials[refs[1]].ResultSource
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "Compare the earlier fixed owner executions."})
	p = reasoningHistoryPrepare(t, root, b)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "comparison": map[string]any{"before": a, "after": z, "reason": "Read the two earlier execution source IDs in the same project."}})
	if err != nil || v["complete"] != true {
		t.Fatal("same-project historical comparison failed", v, err)
	}
	other, _ := loopBook(t)
	if _, err := readWorkTrialExecution(other, a); err == nil {
		t.Fatal("another project accepted the execution source")
	}
}

func TestWorkTrialSessionCapacityEndToEnd(t *testing.T) {
	for _, kind := range []string{"plan", "result", "comparison"} {
		for _, archiveFails := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/archive-fails=%v", kind, archiveFails), func(t *testing.T) {
				_, root, b, p := reasoningFixture(t)
				saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
				input := map[string]any{"receipt": p["receipt"]}
				ref := ""
				if kind == "plan" {
					plan := trialPlan("small", "current")
					plan["decision"] = "The original plan must be recoverable after capacity refusal."
					input["trial"] = map[string]any{"plan": plan}
				} else if kind == "result" {
					v := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
					ref = trialSavedRef(t, v, "trial")
					input["trial"] = map[string]any{"ref": ref, "result": map[string]any{"assessment": "challenges", "observed": "The original contrary owner row must be recovered.", "evidence": []string{"current"}}}
				} else {
					refs := trialMatchedResults(t, b, p["receipt"])
					input["comparison"] = map[string]any{"before": refs[0], "after": refs[1], "reason": "The original paired comparison must be recovered."}
				}
				saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
				interimReadableBoundary(t, root, b)
				queue := filepath.Join(root, "records", "wiki-sources", store.StoreName(root, b.Scope), ".captured.json")
				original, err := os.ReadFile(queue)
				if err != nil {
					t.Fatal(err)
				}
				if archiveFails {
					if err = os.WriteFile(queue, []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				failed, err := workCall(t, b, "record", input)
				if archiveFails {
					if err := os.WriteFile(queue, original, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err == nil && failed["complete"] == true {
					t.Fatal("capacity refusal was not reached")
				}
				s := loadSession(root, "reasoning")
				if s.Planning.Work != nil || reasoningView(s.Reasoning).Complete {
					t.Fatal("capacity refusal left an old complete receipt")
				}
				if kind == "comparison" {
					for key := range s.Reasoning.Comparisons {
						ref = key
					}
				} else if ref == "" {
					for key := range s.Reasoning.Trials {
						ref = key
					}
				}
				if ref == "" {
					t.Fatal("required canonical reference was lost")
				}
				p = reasoningHistoryPrepare(t, root, b)
				retry := map[string]any{"receipt": p["receipt"]}
				if kind == "comparison" {
					retry["comparison"] = map[string]any{"ref": ref}
				} else {
					retry["trial"] = map[string]any{"ref": ref}
				}
				got, err := workCall(t, b, "record", retry)
				if err != nil || got["complete"] != true {
					t.Fatal("same-reference recovery failed", got, err)
				}
				s = loadSession(root, "reasoning")
				if kind == "comparison" {
					v := s.Reasoning.Comparisons[ref]
					if v.Source == "" || !strings.Contains(v.Reason, "original paired") || v.Values["foreign_rows_count"].Delta != 1 {
						t.Fatal("comparison intent changed")
					}
				} else {
					v := s.Reasoning.Trials[ref]
					if v.Source == "" {
						t.Fatal("plan was not captured")
					}
					if kind == "plan" && !strings.Contains(v.Plan.Decision, "original plan") {
						t.Fatal("plan intent changed")
					}
					if kind == "result" && (v.ResultSource == "" || v.Result == nil || !strings.Contains(v.Result.Observed, "original contrary") || v.Result.Assessment != "challenges") {
						t.Fatal("result intent changed")
					}
				}
				end := reasoningInput("finish", ref)
				end["result"] = "The original required intent and its sources were recovered and reviewed."
				got = saveReasoning(t, b, p["receipt"], end, nil)
				if loopObject(t, got["reasoning"])["complete"] != true || reasoningLeft(loadSession(root, "reasoning").Reasoning) != 0 {
					t.Fatal("recovered work could not complete")
				}
				t.Logf("%s capacity -> prepare -> same ref recovery -> supported finish complete; archive-fails=%v", kind, archiveFails)
			})
		}
	}
}
