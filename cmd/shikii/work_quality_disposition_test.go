package main

import "testing"

func TestWorkUnusableQualityEvidenceDoesNotCreatePermanentPending(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown_quality", true: "reported_quality"}[explicit], func(t *testing.T) {
			b, _ := readingLearningBook(t)
			p := readingLearningPrepare(t, b, "Keep a synthetic report even when quality cannot be evaluated.", "")
			for i := 0; i < 3; i++ {
				if _, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
					t.Fatal(e)
				}
			}
			result := map[string]any{"changed": "The synthetic rows were checked.", "evidence": []string{"new1"}}
			if explicit {
				result["sufficient"] = true
			}
			r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(`[{"owner":"synthetic","value":1}]`)}, "result": result})
			if e != nil || r["complete"] != true || loopObject(t, r["storage"])["status"] != "saved" {
				t.Fatal("unevaluable quality must not become permanent storage recovery", r, e)
			}
			status := loopObject(t, r["procedure"])["status"]
			if status != "ungraded" && status != "ineligible" {
				t.Fatal("unverified quality must be explicit", r)
			}
			if explicit && loopObject(t, r["reasoning"])["latest"].(map[string]any)["assessment"] != "unresolved" {
				t.Fatal("unsupported reported quality is not approved", r)
			}
			next, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
			if e != nil || next["procedure_recovery"] != nil {
				t.Fatal("no invalid fixed sample may be retried forever", next, e)
			}
			checked, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("A later actual check found the missing owner condition.")}, "result": map[string]any{"changed": "Corrected the earlier quality claim.", "sufficient": false, "evidence": []string{"new1"}}})
			if e != nil || checked["complete"] != true {
				t.Fatal("a real correction must remain recordable without inventing a passing check", checked, e)
			}
		})
	}
}

func TestWorkNewReportAfterFinishCanResumeByRef(t *testing.T) {
	b, _ := loopBook(t)
	p, e := workCall(t, b, "prepare", map[string]any{"query": "synthetic", "request": "Keep a corrected report after an earlier finish."})
	if e != nil {
		t.Fatal(e)
	}
	first, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "The first synthetic condition was checked."}})
	if e != nil || first["complete"] != true {
		t.Fatal(first, e)
	}
	second, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "The second synthetic condition was checked.", "sufficient": false, "evidence": []string{"new1"}}, "observations": []any{trialObservation("The actual second check failed.")}, "memory": []any{map[string]any{"ref": "missing-memory", "result": "unknown"}}})
	if e != nil || second["complete"] != false {
		t.Fatal("the second raw report must survive the unrelated batch failure", second, e)
	}
	original := loopObject(t, second["storage"])["result_source"]
	recovered, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original}})
	if e != nil || recovered["complete"] != true || loopObject(t, recovered["reasoning"])["complete"] != true {
		t.Fatal("a new saved report may finish without repeating its body", recovered, e)
	}
}
