package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Every fixture uses t.TempDir and the repository's localhost-only fake model.
// No receipt, nonce, actual conversation or live project data is logged.
func TestWorkRecoveryRegressionA01OldFeedbackCannotConfirmNewReport(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "same_report_control"
		if changed {
			name = "changed_report_counterexample"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			p := readingLearningPrepare(t, b, "Inspect a synthetic recovery condition.", "")
			for i := 0; i < 3; i++ {
				if _, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
					t.Fatal(e)
				}
			}
			searches, e := filepath.Glob(filepath.Join(b.Root, "records", "wiki-searches", "*"))
			if e != nil || len(searches) != 1 {
				t.Fatal("fixture needs one isolated search directory")
			}
			checkpoint := filepath.Join(searches[0], ".procedure-policy-v1.json")
			if e = os.Mkdir(checkpoint, 0700); e != nil {
				t.Fatal(e)
			}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "SYNTHETIC-OLD-REPORT"}})
			if e != nil || first["complete"] != false || first["storage"] == nil {
				t.Fatal("fixture must save the first report and interrupt procedure indexing")
			}
			text := "SYNTHETIC-OLD-REPORT"
			if changed {
				text = "SYNTHETIC-NEW-REPORT"
			}
			next, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": text}})
			if e != nil {
				t.Fatal(e)
			}
			storage := loopObject(t, next["storage"])
			v, e := b.ReadSources([]string{storage["result_source"].(string)})
			if e != nil {
				t.Fatal(e)
			}
			original := v["observations"].([]notebook.Observation)[0]
			hasCurrent := strings.Contains(original.Content, text)
			all, e := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", "*.json"))
			if e != nil {
				t.Fatal(e)
			}
			newCaptured := false
			for _, path := range all {
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				newCaptured = newCaptured || strings.Contains(string(data), "SYNTHETIC-NEW-REPORT")
			}
			t.Logf("changed=%v storage=%s source_matches_submitted=%v new_report_captured=%v says_do_not_resend=%v", changed, storage["status"], hasCurrent, newCaptured, strings.Contains(next["next_action"].(string), "do not resend"))
			if strings.HasPrefix(storage["status"].(string), "saved") && !hasCurrent {
				t.Error("the submitted new report is absent, but storage and next_action describe the old report as this saved report")
			}
			if e = os.Remove(checkpoint); e != nil {
				t.Fatal(e)
			}
			recovered, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": text}})
			if e != nil || recovered["complete"] != true {
				t.Fatal("removing the synthetic filesystem obstruction and resending must save the intended report")
			}
			v, e = b.ReadSources([]string{loopObject(t, recovered["storage"])["result_source"].(string)})
			if e != nil || !strings.Contains(v["observations"].([]notebook.Observation)[0].Content, text) {
				t.Fatal("the recovery control must name the newly submitted report")
			}
			t.Log("recovery_control=complete source_matches_submitted=true")
		})
	}
}

func TestWorkRecoveryRegressionPendingNoteAcrossHumanTurn(t *testing.T) {
	for _, newTurn := range []bool{false, true} {
		name := "same_turn_control"
		if newTurn {
			name = "next_turn_counterexample"
		}
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			session := "synthetic-pending-note"
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the first synthetic owner condition."})
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "owner"})
			if e != nil {
				t.Fatal(e)
			}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"title": "Synthetic pending owner rule", "lesson": "Retain the synthetic ownership condition.", "applies_to": "synthetic routing", "kind": "rule", "scene": "synthetic routing", "sources": []string{"missing-source"}}}})
			if e != nil || first["complete"] != false {
				t.Fatal("fixture must retain a failed note")
			}
			before := loadSession(root, session)
			ref := ""
			for key := range before.Reasoning.RequiredNotes {
				ref = key
			}
			if ref == "" {
				t.Fatal("fixture needs one durable required-note reference")
			}
			if newTurn {
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Now check a second synthetic owner condition."})
			}
			after := loadSession(root, session)
			_, retained := after.Reasoning.RequiredNotes[ref]
			p2, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": after.TurnID, "query": "owner"})
			if e != nil {
				t.Fatal(e)
			}
			retry, e := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "notes": []any{map[string]any{"ref": ref, "target": "new", "title": "Synthetic pending owner rule", "lesson": "Retain the synthetic ownership condition.", "applies_to": "synthetic routing", "kind": "rule", "scene": "synthetic routing", "sources": []string{"current"}}}})
			if e != nil && !newTurn {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(retry["errors"])
			unknown := strings.Contains(string(raw), "unknown pending or required note reference") || e != nil && strings.Contains(e.Error(), "unknown pending or required note reference")
			t.Logf("new_turn=%v previous_required_notes=%d retained=%v explicit_ref_recovery_complete=%v unknown_ref_error=%v", newTurn, len(before.Reasoning.RequiredNotes), retained, retry["complete"], unknown)
			if !retained {
				t.Error("a new human request discards the failed note obligation and makes its returned recovery reference unusable")
			}
			if !newTurn && retry["complete"] != true {
				t.Fatal("the same-turn corrected-source repair must succeed")
			}
		})
	}
}

func TestWorkRecoveryUnboundReportCannotFinish(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "synthetic-binding"
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the synthetic owner boundary."})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "owner"})
	if e != nil {
		t.Fatal(e)
	}
	failed, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "SYNTHETIC-UNBOUND-REPORT", "sufficient": true, "evidence": []string{"missing-source"}}})
	if e != nil || failed["complete"] != false {
		t.Fatal("fixture must reject unbound evidence")
	}
	finish := map[string]any{"phase": "finish", "focus": "Synthetic owner check", "assessment": "retain", "reason": "Check the current synthetic request.", "evidence": []string{"current"}, "next_action": "Retain the report.", "result": "The explicit ending is saved."}
	end, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": finish})
	if e != nil {
		t.Fatal(e)
	}
	s := loadSession(root, session)
	stop := runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "\u5408\u6210\u3057\u305f\u6240\u6709\u8005\u306e\u78ba\u8a8d\u3092\u4fdd\u5b58\u3057\u307e\u3057\u305f\u3002"})
	if end["complete"] == true || loopObject(t, end["reasoning"])["complete"] == true {
		t.Error("a saved report with unbound evidence must remain incomplete")
	}
	t.Logf("result_ready=%v record_complete=%v reasoning_complete=%v stop_block=%v stop_halt=%v", s.Reasoning.Result.Ready, end["complete"], loopObject(t, end["reasoning"])["complete"], strings.Contains(stop, `"decision":"block"`), strings.Contains(stop, `"continue":false`))
}
