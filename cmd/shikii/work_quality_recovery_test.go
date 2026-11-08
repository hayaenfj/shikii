package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkRecoveredResultKeepsQualityAgreement(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		byRef, readingQuality bool
	}{
		{"direct_conflict_control", false, false},
		{"recovered_conflict", true, false},
		{"recovered_agreement_control", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			yes := true
			autonomyRun(t, b, "Synthetic conflict generator.", "The generator condition was checked.", nil)
			autonomyRun(t, b, "Synthetic first independent support.", "The first independent source condition passed.", &yes)
			p, e := workCall(t, b, "prepare", map[string]any{"request": "Synthetic quality-conflict validation.", "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}})
			if e != nil {
				t.Fatal("setup", e)
			}
			for i := 0; i < 3; i++ {
				if _, e = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}}); e != nil {
					t.Fatal("setup", e)
				}
			}
			if qualityRecoveryState(t, b, p).Reasoning.Reading.Mixed {
				t.Fatal("setup: mixed reading")
			}
			partial, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Saved the original claimed passing result before evidence recovery.", "sufficient": true, "evidence": []string{"synthetic-missing-evidence"}}})
			if e != nil || partial["complete"] != false {
				t.Fatal("setup: missing result binding", e, partial)
			}
			if qualityRecoverySupports(t, b) != 1 {
				t.Fatal("setup: initial support count")
			}
			result := map[string]any{"changed": "Saved the original claimed passing result before evidence recovery.", "sufficient": true, "evidence": []string{"new1"}}
			if tc.byRef {
				result = map[string]any{"ref": loopObject(t, partial["storage"])["result_source"], "evidence": []string{"new1"}}
			}
			observed := "The current owner check found an omitted condition."
			if tc.readingQuality {
				observed = "The current owner check also passed."
			}
			readingEvidence := fmt.Sprintf(`{"type":"reading-quality/v1","check":"Retain the exact owner condition.","sufficient":%v,"observed":%q}`, tc.readingQuality, observed)
			r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(`{"type":"reading-quality/v1","check":"Retain the exact owner condition.","sufficient":true,"observed":"The earlier owner check passed."}`), trialObservation(readingEvidence)}, "result": result, "reading": map[string]any{"check": "Retain the exact owner condition.", "sufficient": tc.readingQuality, "evidence": []string{"new2"}}})
			count := qualityRecoverySupports(t, b)
			if !tc.byRef {
				if e == nil {
					t.Fatal("control: explicit conflicting quality was not rejected")
				}
				t.Logf("direct_conflict_rejected=true support=%d", count)
				return
			}
			if e != nil {
				if tc.readingQuality {
					t.Fatal("control: matching recovered quality was rejected", e)
				}
				t.Logf("recovered_conflict_rejected=true support=%d", count)
				return
			}
			w := qualityRecoveryState(t, b, p)
			material, e := b.ReadSources([]string{w.Result.Source})
			if e != nil {
				t.Fatal(e)
			}
			var original workCompletion
			if e = json.Unmarshal([]byte(material["observations"].([]notebook.Observation)[0].Content), &original); e != nil || original.Sufficient == nil || !*original.Sufficient {
				t.Fatal("original grade was rewritten", e)
			}
			negativeIndexed := false
			for _, reading := range w.Reasoning.Readings {
				negativeIndexed = negativeIndexed || reading.Indexed && !reading.Details.Sufficient
			}
			t.Logf("recovered_reading_quality=%v complete=%v procedure=%v support_before=1 support_after=%d negative_reading_indexed=%v finish_assessment=%s original_positive_retained=true", tc.readingQuality, r["complete"], r["procedure"], count, negativeIndexed, w.Reasoning.Finish.Assessment)
			if tc.readingQuality {
				if count != 2 || r["complete"] != true || loopObject(t, r["procedure"])["status"] != "adopted" {
					t.Fatal("control: agreeing recovered result did not adopt", r)
				}
				return
			}
			if count > 1 {
				t.Fatal("RSI-R5-B01: a recovered positive result adopted a candidate despite explicit opposite reading quality for the same check")
			}
		})
	}
}

func qualityRecoveryState(t *testing.T, b notebook.Book, p map[string]any) *workState {
	t.Helper()
	token, e := workDecode(p["receipt"].(string))
	if e != nil {
		t.Fatal(e)
	}
	dir, e := openWorkSessions(b.Root)
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	w, _, e := loadWorkState(dir, b.Scope, token)
	if e != nil {
		t.Fatal(e)
	}
	return w
}

func qualityRecoverySupports(t *testing.T, b notebook.Book) int {
	t.Helper()
	paths, e := filepath.Glob(filepath.Join(b.Root, "records", "wiki-searches", "*", ".procedure-policy-v1.json"))
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, path := range paths {
		data, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var j struct {
			Lanes map[string]struct {
				Candidate *struct {
					Support []any `json:"support"`
				} `json:"candidate"`
				History []struct {
					Support []any `json:"support"`
				} `json:"history"`
			} `json:"lanes"`
		}
		if e = json.Unmarshal(data, &j); e != nil {
			t.Fatal(e)
		}
		for _, l := range j.Lanes {
			if l.Candidate != nil {
				n += len(l.Candidate.Support)
			}
			for _, h := range l.History {
				n += len(h.Support)
			}
		}
	}
	return n
}

func TestWorkTypedReadingEvidencePrecedesProcedureSupport(t *testing.T) {
	for _, byRef := range []bool{false, true} {
		for _, typedFalse := range []bool{false, true} {
			t.Run(fmt.Sprintf("result_ref_%v_typed_false_%v", byRef, typedFalse), func(t *testing.T) {
				b, _ := readingLearningBook(t)
				yes := true
				autonomyRun(t, b, "Synthetic evidence-order generator.", "The source generation condition was checked.", nil)
				autonomyRun(t, b, "Synthetic first evidence-order support.", "The distinct first source condition passed.", &yes)
				p, e := workCall(t, b, "prepare", map[string]any{"request": "Synthetic evidence-order held-out task.", "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}})
				if e != nil {
					t.Fatal("setup", e)
				}
				for i := 0; i < 3; i++ {
					if _, e = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}}); e != nil {
						t.Fatal("setup", e)
					}
				}
				if qualityRecoveryState(t, b, p).Reasoning.Reading.Mixed {
					t.Fatal("setup: mixed reading")
				}
				result := map[string]any{"changed": "Retained the synthetic declared passing result.", "sufficient": true, "evidence": []string{"new1"}}
				if byRef {
					partial, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Retained the synthetic declared passing result.", "sufficient": true, "evidence": []string{"synthetic-unresolved-source"}}})
					if e != nil || partial["complete"] != false {
						t.Fatal("setup: missing evidence binding", e, partial)
					}
					result = map[string]any{"ref": loopObject(t, partial["storage"])["result_source"], "evidence": []string{"new1"}}
				}
				if qualityRecoverySupports(t, b) != 1 {
					t.Fatal("setup: support count")
				}
				typedQuality := !typedFalse
				observed := "The current exact owner check passed."
				if typedFalse {
					observed = "The current exact owner check found the required condition missing."
				}
				typedEvidence := fmt.Sprintf(`{"type":"reading-quality/v1","check":"Retain the exact owner condition.","sufficient":%v,"observed":%q}`, typedQuality, observed)
				r, e := workCall(t, b, "record", map[string]any{
					"receipt": p["receipt"], "result": result,
					"observations": []any{trialObservation(`{"type":"reading-quality/v1","check":"Retain the exact owner condition.","sufficient":true,"observed":"The earlier exact owner check passed."}`), trialObservation(typedEvidence)},
					"reading":      map[string]any{"check": "Retain the exact owner condition.", "sufficient": true, "evidence": []string{"new2"}},
				})
				if e != nil {
					t.Fatal("record acceptance", e)
				}
				count := qualityRecoverySupports(t, b)
				readingRejected := false
				if raw, ok := r["errors"].([]any); ok {
					for _, err := range raw {
						readingRejected = readingRejected || loopObject(t, err)["kind"] == "reading"
					}
				}
				original := qualityRecoveryState(t, b, p).Result.Source
				material, e := b.ReadSources([]string{original})
				if e != nil || len(material["observations"].([]notebook.Observation)) != 1 {
					t.Fatal("original lost", e)
				}
				if typedFalse && !readingRejected {
					t.Fatal("setup: contradictory typed reading was not rejected")
				}
				// Inspect persistence and automatic completion after the rejected reading.
				next, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}})
				if e != nil {
					t.Fatal("prepare after reading rejection", e)
				}
				w := qualityRecoveryState(t, b, p)
				assessment := "none"
				if w.Reasoning.Finish != nil {
					assessment = w.Reasoning.Finish.Assessment
				}
				t.Logf("result_ref=%v declared_result=true declared_reading=true typed_reading=%v record_complete=%v reading_rejected=%v support_before=1 support_after=%d support_after_prepare=%d procedure=%v auto_recovery=%v finish=%s original_retained=%v", byRef, typedQuality, r["complete"], readingRejected, count, qualityRecoverySupports(t, b), r["procedure"], next["procedure_recovery"], assessment, w.Result.Source == original)
				if typedFalse && count > 1 {
					t.Fatal("RSI-R5-B01: positive support was committed before contradictory typed reading evidence was rejected")
				}
				if !typedFalse && (r["complete"] != true || count != 2 || loopObject(t, r["procedure"])["status"] != "adopted") {
					t.Fatal("control: consistent typed evidence did not adopt")
				}
			})
		}
	}
}

func TestWorkDifferentTurnQualityRemainsSeparate(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	reasoningPage(t, b, "Reading source recovery", "howto", strings.Repeat("Keep each original result with its own task and exact source conditions.\n\n", 80))
	session := "synthetic-separate-turn-quality"
	prepare := func() map[string]any {
		p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}})
		if e != nil {
			t.Fatal("setup", e)
		}
		return p
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the first synthetic condition."})
	old := prepare()
	r, e := workCall(t, b, "record", map[string]any{"receipt": old["receipt"], "result": map[string]any{"changed": "Retained the first synthetic passing result.", "sufficient": true, "evidence": []string{"synthetic-unresolved-old-proof"}}})
	if e != nil || r["complete"] != false {
		t.Fatal("setup: old pending result", e, r)
	}
	original := loopObject(t, r["storage"])["result_source"].(string)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the second and different synthetic condition."})
	p := prepare()
	repaired, e := workCall(t, b, "record", map[string]any{
		"receipt":      p["receipt"],
		"observations": []any{trialObservation(`{"type":"reading-quality/v1","check":"Retain the exact condition.","sufficient":true,"observed":"The first task's condition passed."}`), trialObservation(`{"type":"reading-quality/v1","check":"Retain the exact condition.","sufficient":false,"observed":"The second task omitted its different required condition."}`)},
		"result":       map[string]any{"ref": original, "evidence": []string{"new1"}},
		"reading":      map[string]any{"check": "Retain the exact condition.", "sufficient": false, "evidence": []string{"new2"}},
	})
	if e != nil || repaired["complete"] != true || loopObject(t, repaired["storage"])["result_source"] != original || loopObject(t, repaired["reasoning"])["complete"] == true {
		t.Fatal("independent old recovery was coupled to the new task", e, repaired)
	}
	w := qualityRecoveryState(t, b, p)
	pending := w.Reasoning.PendingResults[original]
	if pending == nil || !pending.Complete || pending.Turn == w.Reasoning.TurnID {
		t.Fatal("old recovery lost its turn identity")
	}
	negativeIndexed := false
	for _, reading := range w.Reasoning.Readings {
		negativeIndexed = negativeIndexed || reading.Indexed && !reading.Details.Sufficient
	}
	if !negativeIndexed {
		t.Fatal("current negative reading was not recorded independently")
	}
	material, e := b.ReadSources([]string{original})
	if e != nil {
		t.Fatal(e)
	}
	var saved workCompletion
	if e = json.Unmarshal([]byte(material["observations"].([]notebook.Observation)[0].Content), &saved); e != nil || saved.Sufficient == nil || !*saved.Sufficient {
		t.Fatal("old quality changed", e)
	}
	t.Log("old_turn_positive_repaired=true current_turn_negative_reading_indexed=true current_task_complete=false original_grade_unchanged=true")
}

func TestWorkUnavailableReadingCheckPreservesNegativeAndExcludesSupport(t *testing.T) {
	for _, grade := range []bool{true, false} {
		t.Run(fmt.Sprint(grade), func(t *testing.T) {
			b, _ := readingLearningBook(t)
			yes := true
			autonomyRun(t, b, "Synthetic auxiliary-check generator.", "The original generator condition was checked.", nil)
			autonomyRun(t, b, "Synthetic prior auxiliary-check support.", "The first different condition passed.", &yes)
			p := readingLearningPrepare(t, b, "Check a new condition with an unavailable auxiliary proof.", "focused")
			for i := 0; i < 3; i++ {
				if _, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery", "reasoning": map[string]any{"depth": "focused"}}); e != nil {
					t.Fatal(e)
				}
			}
			before := qualityRecoverySupports(t, b)
			report, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The new owner condition has an actual checked result.")}, "result": map[string]any{"changed": "Saved the new owner check.", "sufficient": grade, "evidence": []string{"new1"}}, "reading": map[string]any{"check": "Preserve the current owner condition.", "sufficient": grade, "evidence": []string{"missing-auxiliary-check"}}})
			if e != nil || report["complete"] != false {
				t.Fatal("the unavailable reading remains explicit", report, e)
			}
			view := loopObject(t, report["procedure"])
			if grade {
				if qualityRecoverySupports(t, b) != before || view["status"] != "ineligible" {
					t.Fatal("unverified auxiliary quality cannot add positive support", report)
				}
			} else if view["status"] != "rejected" && view["status"] != "rolled_back" {
				t.Fatal("the actual negative cannot be lost with an auxiliary failure", report)
			}
			if _, e = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
				t.Fatal(e)
			}
			if qualityRecoverySupports(t, b) > before {
				t.Fatal("preparation cannot adopt an unverified positive")
			}
			w := qualityRecoveryState(t, b, p)
			if pendingProcedureCount(w.Reasoning) != 0 {
				t.Fatal("unsupported quality cannot leave an invalid retry loop")
			}
		})
	}
}

func TestWorkCheckedReadingDoesNotLeaveGeneratedReflection(t *testing.T) {
	for _, shared := range []bool{true, false} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			b, _ := readingLearningBook(t)
			baseline, e := b.PendingObservations()
			if e != nil {
				t.Fatal(e)
			}
			old := map[string]bool{}
			for _, o := range baseline {
				old[o.ID] = true
			}
			p := readingLearningPrepare(t, b, "Keep one checked result and its reading feedback.", "focused")
			observations := []any{trialObservation("The exact synthetic owner condition passed.")}
			readingRef := "new1"
			if !shared {
				observations = append(observations, trialObservation("The separate reading check retained the owner condition."))
				readingRef = "new2"
			}
			observations = append(observations, map[string]any{"kind": "human", "title": "Synthetic retained preference", "content": "Keep this original human preference available for a separate review."})
			r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": observations, "result": map[string]any{"changed": "Checked the exact owner condition.", "sufficient": true, "evidence": []string{"new1"}}, "reading": map[string]any{"check": "Retain the exact owner condition.", "sufficient": true, "evidence": []string{readingRef}}})
			if e != nil || r["complete"] != true {
				t.Fatal("the ordinary ending is one recording", r, e)
			}
			pending, e := b.PendingObservations()
			if e != nil {
				t.Fatal(e)
			}
			if len(pending) != len(baseline)+1 {
				t.Fatal("checked diagnostics must not add reflection work", pending)
			}
			for _, o := range pending {
				if !old[o.ID] && (o.Kind != "human" || o.Title != "Synthetic retained preference") {
					t.Fatal("only original human intent and unrelated sources remain", o)
				}
			}
			w := qualityRecoveryState(t, b, p)
			for _, reading := range w.Reasoning.Readings {
				material, e := b.ReadSources([]string{reading.Source})
				if e != nil || len(material["observations"].([]notebook.Observation)) != 1 {
					t.Fatal("processing must retain the original diagnostic", e)
				}
			}
		})
	}
}
