package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
