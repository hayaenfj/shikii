package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkProcedureRegressionB01OriginEvidence(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "unknown_generator"
		if explicit {
			name = "graded_generator_control"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			yes := true
			var quality *bool
			if explicit {
				quality = &yes
			}
			_, gen := autonomyRun(t, b, "Synthetic original recovery task.", "Synthetic result A: owner condition retained.", quality)
			if loopObject(t, gen["procedure"])["status"] != "proposed" {
				t.Fatal("setup: no proposal")
			}
			_, first := autonomyRun(t, b, "Synthetic renamed recovery task.", "Synthetic result A: owner condition retained.", &yes)
			_, last := autonomyRun(t, b, "Synthetic independent recovery task.", "Synthetic result C: total condition retained.", &yes)
			t.Logf("explicit_generator_quality=%v reused_origin_cases=%v final_status=%v final_cases=%v", explicit, loopObject(t, first["procedure"])["cases"], loopObject(t, last["procedure"])["status"], loopObject(t, last["procedure"])["cases"])
			if loopObject(t, last["procedure"])["status"] == "adopted" {
				t.Fatal("RSI-AUDIT-2026-B01: origin evidence counted toward held-out adoption")
			}
		})
	}
}

func TestWorkProcedureRegressionB02ChangedSearchBasis(t *testing.T) {
	for _, changed := range []bool{true, false} {
		name := "changed_basis"
		if !changed {
			name = "same_basis_control"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			other := reasoningPage(t, b, "Synthetic unrelated maintenance", "howto", strings.Repeat("Maintenance condition for the separate procedure.\n\n", 80))
			p := readingLearningPrepare(t, b, "Synthetic task spanning two separate topics.", "")
			query := "Reading source recovery"
			if changed {
				query = "Synthetic unrelated maintenance"
			}
			for i := 0; i < 3; i++ {
				next, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": query})
				if e != nil {
					t.Fatal("setup", e)
				}
				if changed {
					knowledge := next["knowledge"].([]any)
					if len(knowledge) != 1 || loopObject(t, knowledge[0])["id"] != other.ID {
						t.Fatal("setup: unrelated search did not select only its own article")
					}
				}
			}
			ended, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Saved the synthetic maintenance investigation."}})
			if e != nil || ended["complete"] != true {
				t.Fatal("setup", e, ended["complete"])
			}
			view := loopObject(t, ended["procedure"])
			t.Logf("changed_basis=%v generated_status=%v", changed, view["status"])
			if changed {
				raw, e := b.ReadSources([]string{view["source"].(string)})
				if e != nil {
					t.Fatal("setup", e)
				}
				var sample notebook.ProcedureSample
				if e = json.Unmarshal([]byte(raw["observations"].([]notebook.Observation)[0].Content), &sample); e != nil {
					t.Fatal("setup", e)
				}
				t.Logf("initial_basis=%s stored_stalls=%d unrelated_article=%s basis_article=%s", sample.Trace.Basis.Topic, sample.Trace.Stalls, other.ID, sample.Trace.Basis.Articles[0].ID)
				for _, item := range []struct{ task, evidence string }{{"Synthetic unrelated validator one.", "Synthetic maintenance B: first separate result."}, {"Synthetic unrelated validator two.", "Synthetic maintenance C: second separate result."}} {
					v := readingLearningPrepare(t, b, item.task, "")
					for i := 0; i < 4; i++ {
						if _, e = workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": query}); e != nil {
							t.Fatal("setup", e)
						}
					}
					result, e := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{trialObservation(item.evidence)}, "result": map[string]any{"changed": "Only unrelated maintenance was checked.", "evidence": []string{"new1"}, "sufficient": true}})
					if e != nil || result["complete"] != true {
						t.Fatal("setup", e, result["complete"])
					}
					view = loopObject(t, result["procedure"])
					t.Logf("unrelated_validation_status=%v cases=%v", view["status"], view["cases"])
				}
				if view["status"] == "adopted" {
					t.Fatal("RSI-AUDIT-2026-B02: stalls and validation from another search adopted a procedure for the initial basis")
				}
			}
			if !changed && view["status"] != "proposed" {
				t.Fatal("control: repeated original search should generate")
			}
		})
	}
}

func TestWorkProcedureRegressionB03FailureSurvivesPartialRecovery(t *testing.T) {
	for _, interrupt := range []bool{true, false} {
		name := "partial_recovery"
		if !interrupt {
			name = "uninterrupted_control"
		}
		t.Run(name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			yes := true
			autonomyRun(t, b, "Synthetic generator before failure.", "Synthetic initial check.", nil)
			autonomyRun(t, b, "Synthetic independent validation A.", "Synthetic ownership check A.", &yes)
			_, adopted := autonomyRun(t, b, "Synthetic independent validation B.", "Synthetic totals check B.", &yes)
			if loopObject(t, adopted["procedure"])["status"] != "adopted" {
				t.Fatal("setup: no adopted procedure")
			}
			p := readingLearningPrepare(t, b, "Synthetic counterexample with a partial save.", "")
			old := workAfterWrite
			t.Cleanup(func() { workAfterWrite = old })
			if interrupt {
				workAfterWrite = func(kind string) error {
					if kind == "reading" {
						return errors.New("synthetic reading acknowledgement loss")
					}
					return nil
				}
			}
			first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("Synthetic counterexample: requested owner condition was omitted.")}, "result": map[string]any{"changed": "Saved the observed missing owner condition.", "evidence": []string{"new1"}, "sufficient": false}, "reading": map[string]any{"check": "Recover the explicit owner condition.", "sufficient": false, "evidence": []string{"new1"}}})
			workAfterWrite = old
			if e != nil {
				t.Fatal("setup", e)
			}
			final := first
			if interrupt {
				if first["complete"] != false || loopObject(t, first["storage"])["status"] != "saved-pending" {
					t.Fatal("setup: partial original result not saved")
				}
				ref := ""
				for _, item := range first["errors"].([]any) {
					v := loopObject(t, item)
					if v["kind"] == "reading" {
						ref = v["ref"].(string)
					}
				}
				if ref == "" {
					t.Fatal("setup: missing reading recovery ref")
				}
				final, e = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": map[string]any{"ref": ref}})
				if e != nil || final["complete"] != true {
					t.Fatal("setup: reference-only recovery failed", e, final["complete"])
				}
				oldSource := loopObject(t, first["storage"])["result_source"].(string)
				newSource := loopObject(t, final["storage"])["result_source"].(string)
				raw, e := b.ReadSources([]string{oldSource})
				if e != nil {
					t.Fatal("setup", e)
				}
				var completion workCompletion
				if e = json.Unmarshal([]byte(raw["observations"].([]notebook.Observation)[0].Content), &completion); e != nil {
					t.Fatal("setup", e)
				}
				if oldSource != newSource || completion.Sufficient == nil || *completion.Sufficient {
					t.Fatal("setup: saved negative result changed")
				}
				t.Log("saved_negative_result_retained=true")
			}
			view := loopObject(t, final["procedure"])
			t.Logf("interrupt=%v complete=%v final_procedure=%v", interrupt, final["complete"], view["status"])
			if view["status"] != "rolled_back" {
				t.Fatal("RSI-AUDIT-2026-B03: a saved negative result was not applied after partial recovery")
			}
		})
	}
}
