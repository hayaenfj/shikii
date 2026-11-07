package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func auditBReadingFixture(t *testing.T) (string, string, notebook.Book, map[string]any) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	reasoningPage(t, b, "CSV audit reading", "howto", strings.Repeat("Keep the requested CSV owner condition in the recovered source.\n\n", 80))
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": planningTestRequest})
	p, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV audit reading", "reasoning": map[string]any{"depth": "brief"}})
	if err != nil || p["reading"] == nil {
		t.Fatal("synthetic reading preparation failed", err)
	}
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	return repo, root, b, p
}

func auditBReadingInput(sufficient bool) map[string]any {
	return map[string]any{"check": "Keep the requested CSV owner condition.", "sufficient": sufficient, "evidence": []string{"current"}}
}

func auditBAdoptBrief(t *testing.T, b notebook.Book) {
	t.Helper()
	for _, task := range []string{"Recover CSV case A.", "Recover CSV case B."} {
		for _, depth := range []string{"focused", "brief"} {
			p, err := workCall(t, b, "prepare", map[string]any{"request": task, "query": "CSV audit reading", "reasoning": map[string]any{"depth": depth}})
			if err != nil {
				t.Fatal("matched preparation failed", err)
			}
			reading := auditBReadingInput(true)
			reading["evidence"] = []string{"new1"}
			v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(task + " " + depth + " was checked under the fixed owner condition.")}, "reading": reading})
			if err != nil || v["complete"] != true {
				t.Fatal("matched report recording failed", err)
			}
		}
	}
}

func auditBCurrentPolicy(t *testing.T, b notebook.Book, r *reasoningProgress) *notebook.ReadingPolicy {
	t.Helper()
	p, err := b.ReadingPreference(r.Reading.Topic, r.Reading.Stage, r.Reading.Articles)
	if err != nil || p == nil {
		t.Fatal("reading policy unavailable", err)
	}
	return p
}

func TestReadingAuditBSavedCapacityRefusalRetainsPreviousState(t *testing.T) {
	_, root, b, p := auditBReadingFixture(t)
	auditBAdoptBrief(t, b)
	if auditBCurrentPolicy(t, b, loadSession(root, "reasoning").Reasoning).Status != "adopted" {
		t.Fatal("fixture did not adopt its matched candidate")
	}
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
	if err := updateSession(root, "reasoning", func(s *hookSession) {
		for len(s.Planning.Work.Saved) < workSavedLimit {
			key := fmt.Sprintf("synthetic-existing-%d", len(s.Planning.Work.Saved))
			s.Planning.Work.Saved[key] = workItem{Kind: "synthetic"}
		}
	}); err != nil {
		t.Fatal("synthetic saved-response limit unavailable", err)
	}
	before := loadSession(root, "reasoning")
	sourcesBefore, err := filepath.Glob(filepath.Join(root, "records", "wiki-sources", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": auditBReadingInput(false)})
	if err != nil || v["complete"] != false {
		t.Fatal("fixture did not reach saved-response refusal", err)
	}
	s := loadSession(root, "reasoning")
	gate := verifyStopReasoning(b.Root, s, s)
	policy := auditBCurrentPolicy(t, b, s.Reasoning)
	sourcesAfter, err := filepath.Glob(filepath.Join(root, "records", "wiki-sources", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("record_complete=%v pending=%d reasoning_complete=%v left=%d stop_allowed=%v policy=%s new_sources=%d", v["complete"], len(s.Reasoning.Readings), reasoningView(s.Reasoning).Complete, reasoningLeft(s.Reasoning), gate == nil, policy.Status, len(sourcesAfter)-len(sourcesBefore))
	if !reasoningView(s.Reasoning).Complete || len(s.Reasoning.Readings) != 0 || len(sourcesAfter) != len(sourcesBefore) || s.Reasoning.Generation != before.Reasoning.Generation {
		t.Error("pre-reservation refusal changed the previous state")
	}
}

func TestReadingAuditBPendingOmissionSurvivesNextHumanTurn(t *testing.T) {
	repo, root, b, p := auditBReadingFixture(t)
	auditBAdoptBrief(t, b)
	token, err := workDecode(p["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, name, err := loadWorkState(dir, b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	commit := workCommitter(func(action func() error) error {
		commits++
		if commits == 2 {
			return errors.New("synthetic index write failure")
		}
		return commitWorkTurn(dir, name, b.Scope, token, action)
	})
	sufficient := false
	_, err = recordWorkReading(b, w, workReadingFeedback{Check: "Keep the requested CSV owner condition.", Sufficient: &sufficient, Evidence: []string{"current"}}, func([]string) ([]string, error) { return []string{w.Reasoning.CurrentSource}, nil }, func() error { return saveWorkState(dir, name, b.Scope, token.Turn, w, false) }, commit)
	if err == nil || commits != 2 {
		t.Fatal("fixture did not reach index failure after source capture")
	}
	before := loadSession(root, "reasoning")
	var ref string
	var original *workReadingState
	for k, state := range before.Reasoning.Readings {
		ref, original = k, state
	}
	if ref == "" {
		t.Fatal("original reading reference missing")
	}
	if original.Source == "" || original.Indexed {
		t.Fatal("expected an accepted snapshot with a pending index")
	}
	if _, err := b.OutcomeSources([]string{original.Source}); err != nil {
		t.Fatal("snapshot was not persisted", err)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "Check another CSV ownership condition."})
	after := loadSession(root, "reasoning")
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": after.TurnID, "query": "CSV audit reading"})
	if err != nil {
		t.Fatal("next turn preparation failed", err)
	}
	retry, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "reading": map[string]any{"ref": ref}})
	if _, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": map[string]any{"ref": ref}}); err == nil {
		t.Fatal("the previous turn receipt became valid again")
	}
	policy := auditBCurrentPolicy(t, b, before.Reasoning)
	t.Logf("source_persisted=%v index_pending=%v previous_pending=%d next_pending=%d retry_complete=%v outer_error=%v policy=%s next_depth=%v", original.Source != "", !original.Indexed, len(before.Reasoning.Readings), len(after.Reasoning.Readings), retry["complete"], err != nil, policy.Status, loopObject(t, next["attention"])["depth"])
	restored := loadSession(root, "reasoning").Reasoning.Readings[ref]
	if len(after.Reasoning.Readings) != 1 || err != nil || retry["complete"] != true || policy.Status != "suspended" || restored == nil || !restored.Indexed || restored.Source != original.Source || workHash(restored.Details) != workHash(original.Details) {
		t.Error("accepted insufficient reading did not recover its original execution and suspend adoption across turns")
	}
}

func TestReadingAuditBReadingCaptureRespectsSavedCapacity(t *testing.T) {
	_, root, b, p := auditBReadingFixture(t)
	if err := updateSession(root, "reasoning", func(s *hookSession) {
		for len(s.Planning.Work.Saved) < workSavedLimit-1 {
			key := fmt.Sprintf("synthetic-existing-%d", len(s.Planning.Work.Saved))
			s.Planning.Work.Saved[key] = workItem{Kind: "synthetic"}
		}
	}); err != nil {
		t.Fatal(err)
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": auditBReadingInput(true)})
	if err != nil || v["complete"] != true {
		t.Fatal("recording in the last response slot failed", err)
	}
	s := loadSession(root, "reasoning")
	t.Logf("saved_responses=%d limit=%d readings=%d", len(s.Planning.Work.Saved), workSavedLimit, len(s.Reasoning.Readings))
	if len(s.Planning.Work.Saved) > workSavedLimit {
		t.Error("snapshot and index exceeded the single reserved response slot")
	}
}

func TestReadingAuditBInterruptedCaptureSurvivesReplacementPreparation(t *testing.T) {
	for _, newTurn := range []bool{false, true} {
		t.Run(fmt.Sprint(newTurn), func(t *testing.T) {
			repo, root, b, p := auditBReadingFixture(t)
			old := workAfterWrite
			t.Cleanup(func() { workAfterWrite = old })
			workAfterWrite = func(kind string) error {
				if kind == "observation" {
					return errors.New("synthetic capture response interruption")
				}
				return nil
			}
			v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": auditBReadingInput(false)})
			workAfterWrite = old
			if err != nil || v["complete"] != false {
				t.Fatal("fixture did not reach the capture interruption", err)
			}
			before := loadSession(root, "reasoning")
			var ref string
			var details notebook.ReadingDetails
			for k, state := range before.Reasoning.Readings {
				ref, details = k, state.Details
			}
			if ref == "" {
				t.Fatal("original execution missing")
			}
			var memo workCapture
			for _, pending := range before.Reasoning.TrialCaptures {
				memo = pending
			}
			if memo.At.IsZero() {
				t.Fatal("original capture time missing")
			}
			if newTurn {
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "Check another CSV condition after capture interruption."})
			}
			next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV audit reading"})
			if err != nil {
				t.Fatal("replacement preparation failed", err)
			}
			v, err = workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "reading": map[string]any{"ref": ref}})
			if err != nil || v["complete"] != true {
				t.Fatal("original reference recovery failed", err)
			}
			after := loadSession(root, "reasoning").Reasoning.Readings[ref]
			if !after.Indexed || after.Source == "" || workHash(after.Details) != workHash(details) {
				t.Fatal("recovery changed the original execution")
			}
			material, err := b.ReadSources([]string{after.Source})
			if err != nil {
				t.Fatal(err)
			}
			observed := material["observations"].([]notebook.Observation)
			if len(observed) != 1 || !observed[0].ObservedAt.Equal(memo.At) {
				t.Fatal("capture retry changed the original timestamp")
			}
			t.Logf("recovered=%v insufficiency_retained=%v timestamp_retained=%v", after.Indexed, !after.Details.Sufficient, observed[0].ObservedAt.Equal(memo.At))
		})
	}
}
