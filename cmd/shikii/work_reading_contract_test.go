package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestReadingStagePreservesAcceptedAssignmentRange(t *testing.T) {
	for _, n := range []int{100, 101, 200, 201} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			b, source := loopBook(t)
			page := loopPage(t, b, source, map[string]any{"title": "State boundary owner", "body": strings.Repeat("Keep the fixed original owner.\n\n", 80)})
			a := map[string]any{"id": "synthetic-stage-task", "group": "synthetic-group", "role": "worker", "stage": strings.Repeat("s", n), "purpose": "Check fixed source condition"}
			p, e := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "lesson:" + page.ID, "reasoning": map[string]any{"depth": "brief"}})
			if n > 200 {
				if e == nil {
					t.Fatal("an overlong stage must fail before preparation")
				}
				return
			}
			if e != nil {
				t.Fatal("valid assignment preparation failed", e)
			}
			v, e := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Actual source outcome", "content": "The actual source owner was checked."}}, "reading": map[string]any{"check": "Keep the fixed owner.", "sufficient": true, "evidence": []string{"new1"}}})
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("stage_bytes=%d prepare_accepted=true reading_complete=%v errors=%v", n, v["complete"], v["errors"])
			if n == 100 && v["complete"] != true {
				t.Fatal("100-byte control failed")
			}
			if n > 100 && v["complete"] != true {
				t.Fatal("boundary counterexample did not reproduce")
			}
			ordinary, e := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": p["receipt"], "event": map[string]any{"id": "ordinary", "state": "completed", "summary": "The ordinary synthetic report completed."}})
			if e != nil || ordinary["complete"] != true {
				t.Fatal("ordinary report control failed", e, ordinary)
			}
		})
	}
}

func TestReadingExplicitResultsControlAdoption(t *testing.T) {
	cases := []struct {
		name, a, b string
		adopt      bool
	}{
		{"numeric", `{"observed":{"checks":1}}`, `{"observed":{"checks":1.0}}`, false},
		{"provenance", `{"observed":"Checked the exact condition.","provenance":{"task":"a"}}`, `{"observed":"Checked the exact condition.","unrelated":{"task":"b"}}`, false},
		{"typed-number", `{"type":"reading-quality/v1","check":"The requested source condition is correctly recovered.","sufficient":true,"observed":"{\"checks\":1}"}`, `{"type":"reading-quality/v1","check":"The requested source condition is correctly recovered.","sufficient":true,"observed":"{\"checks\":1e0}"}`, false},
		{"different", `{"observed":{"checks":1}}`, `{"observed":{"checks":2}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, _ := readingLearningBook(t)
			for i, task := range []string{"Recover explicit source A.", "Recover explicit source B."} {
				content := c.a
				if i == 1 {
					content = c.b
				}
				for _, depth := range []string{"focused", "brief"} {
					p := readingLearningPrepare(t, b, task, depth)
					readingLearningFeedback(t, b, p, true, content)
				}
			}
			p := readingLearningPrepare(t, b, "Recover another explicit source.", "")
			a := loopObject(t, p["attention"])
			policy := loopObject(t, a["learning"])
			if (policy["status"] == "adopted") != c.adopt {
				t.Fatalf("quality identity produced an incorrect adoption: %v", policy)
			}
			if !c.adopt && (policy["cases"] != float64(1) || a["depth"] != "focused") {
				t.Fatal("the same explicit result multiplied support")
			}
		})
	}
}
