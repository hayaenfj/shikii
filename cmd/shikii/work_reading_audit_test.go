package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

const auditATopic = "Violet reading condition"
const auditACheck = "Recover the specified condition from the exact article."

func auditABook(t *testing.T) (notebook.Book, notebook.Page) {
	t.Helper()
	b, _ := loopBook(t)
	p := reasoningPage(t, b, auditATopic, "howto", strings.Repeat("Retain the requested Violet condition before accepting the result.\n\n", 80))
	return b, p
}

func auditACall(t *testing.T, b notebook.Book, operation string, input map[string]any) map[string]any {
	t.Helper()
	v, err := workCall(t, b, operation, input)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func auditAFeedback(t *testing.T, b notebook.Book, receipt any, a any, text string) {
	t.Helper()
	input := map[string]any{"receipt": receipt, "observations": []any{map[string]any{"kind": "test", "title": "Synthetic checked condition", "content": text}}, "reading": map[string]any{"check": auditACheck, "sufficient": true, "evidence": []string{"new1"}}}
	if a != nil {
		input["assignment"] = a
	}
	v := auditACall(t, b, "record", input)
	if v["complete"] != true {
		t.Fatalf("quality recording rejected: %v", v["errors"])
	}
}

func auditAAssignment(id, task, stage string) map[string]any {
	return map[string]any{"id": id, "group": task, "role": "specialist", "stage": stage, "purpose": task, "question": "Recover the specified Violet condition.", "conditions": "synthetic isolated input", "commit": strings.Repeat("a", 40), "code_state": "provisional"}
}

func TestReadingAuditAStageAfterEmptyInitialRead(t *testing.T) {
	b, _ := auditABook(t)
	for i, task := range []string{"Violet case one", "Violet case two"} {
		for _, depth := range []string{"focused", "brief"} {
			a := auditAAssignment(fmt.Sprintf("review-%d-%s", i, depth), task, "review")
			first := auditACall(t, b, "prepare", map[string]any{"assignment": a, "query": "UnmatchedZirconOnly", "reasoning": map[string]any{"depth": depth}})
			if len(first["knowledge"].([]any)) != 0 {
				t.Fatal("initial query unexpectedly returned an article")
			}
			next := auditACall(t, b, "prepare", map[string]any{"assignment": a, "receipt": first["receipt"], "query": auditATopic})
			reading := loopObject(t, next["reading"])
			if reading["stage"] != "review" {
				t.Fatal("the empty initial search lost its stage")
			}
			t.Logf("review=%d depth=%s retained_stage=%v", i, depth, reading["stage"])
			auditAFeedback(t, b, next["receipt"], a, fmt.Sprintf("Independent synthetic review %d at %s recovered the specified condition.", i, depth))
		}
	}
	ordinary := auditACall(t, b, "prepare", map[string]any{"request": "Unseen ordinary task", "query": auditATopic})
	ordinaryAttention := loopObject(t, ordinary["attention"])
	ordinaryPolicy := loopObject(t, ordinaryAttention["learning"])
	t.Logf("ordinary_depth=%s learning_status=%v learning_cases=%v", ordinaryAttention["depth"], ordinaryPolicy["status"], ordinaryPolicy["cases"])
	a := auditAAssignment("research-next", "Unseen research task", "research")
	first := auditACall(t, b, "prepare", map[string]any{"assignment": a, "query": "UnmatchedZirconOnly"})
	next := auditACall(t, b, "prepare", map[string]any{"assignment": a, "receipt": first["receipt"], "query": auditATopic})
	attention := loopObject(t, next["attention"])
	researchPolicy := loopObject(t, attention["learning"])
	t.Logf("research_extension_depth=%s learning_status=%v learning_cases=%v", attention["depth"], researchPolicy["status"], researchPolicy["cases"])
	if ordinaryAttention["depth"] != "focused" || attention["depth"] != "focused" {
		t.Fatal("review-stage support crossed into an ordinary task and research stage after an empty initial read")
	}
}

func TestReadingAuditAInitialPinnedReadCannotDisappearFromCost(t *testing.T) {
	b, page := auditABook(t)
	for i, task := range []string{"Violet cost case one", "Violet cost case two"} {
		base := auditACall(t, b, "prepare", map[string]any{"request": task, "query": auditATopic, "reasoning": map[string]any{"depth": "focused"}})
		baseBytes := workContextSize(base["knowledge"])
		auditAFeedback(t, b, base["receipt"], nil, fmt.Sprintf("Synthetic focused cost check %d recovered the condition.", i))
		full := auditACall(t, b, "prepare", map[string]any{"request": task, "query": "lesson:" + page.ID + "@" + page.Revision, "reasoning": map[string]any{"depth": "brief"}})
		fullBytes := workContextSize(full["knowledge"])
		initial := loopObject(t, full["reading"])
		if initial["bytes"] != float64(fullBytes) || initial["reads"] != float64(1) || initial["mixed"] != true {
			t.Fatal("initial full delivery cost or learning exclusion was lost")
		}
		brief := auditACall(t, b, "prepare", map[string]any{"receipt": full["receipt"], "query": auditATopic})
		briefBytes := workContextSize(brief["knowledge"])
		trace := loopObject(t, brief["reading"])
		t.Logf("case=%d baseline=%d actual_initial_full_plus_brief=%d recorded_bytes=%.0f recorded_reads=%.0f", i, baseBytes, fullBytes+briefBytes, trace["bytes"], trace["reads"])
		if fullBytes+briefBytes <= baseBytes {
			t.Fatal("control did not increase actual delivery")
		}
		if trace["bytes"] != float64(fullBytes+briefBytes) || trace["reads"] != float64(2) || trace["mixed"] != true {
			t.Fatal("the later search discarded the initial full read")
		}
		v := auditACall(t, b, "record", map[string]any{"receipt": brief["receipt"], "observations": []any{trialObservation("A quality result including full recovery.")}, "reading": map[string]any{"check": auditACheck, "sufficient": true, "evidence": []string{"new1"}}})
		if v["complete"] != false {
			t.Fatal("initial full recovery taught a smaller-read preference")
		}
	}
	next := auditACall(t, b, "prepare", map[string]any{"request": "Unseen Violet cost case", "query": auditATopic})
	a := loopObject(t, next["attention"])
	policy := loopObject(t, a["learning"])
	t.Logf("selected_depth=%s learning_status=%v learning_cases=%v", a["depth"], policy["status"], policy["cases"])
	if a["depth"] != "focused" {
		t.Fatal("an uncounted initial full read produced an adopted smaller-read default")
	}
}

func TestReadingAuditASameReportDifferentKindCannotAddSupport(t *testing.T) {
	b, _ := auditABook(t)
	for i, task := range []string{"Violet duplicate case one", "Violet duplicate case two"} {
		for _, depth := range []string{"focused", "brief"} {
			p := auditACall(t, b, "prepare", map[string]any{"request": task, "query": auditATopic, "reasoning": map[string]any{"depth": depth}})
			kind := []string{"test", "agent"}[i]
			v := auditACall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": kind, "title": "Same synthetic report", "content": "The exact same synthetic checked result is reused here."}}, "reading": map[string]any{"check": auditACheck, "sufficient": true, "evidence": []string{"new1"}}})
			if v["complete"] != true {
				t.Fatalf("reading rejected: %v", v["errors"])
			}
		}
	}
	next := auditACall(t, b, "prepare", map[string]any{"request": "Unseen duplicate-control task", "query": auditATopic})
	a := loopObject(t, next["attention"])
	policy := loopObject(t, a["learning"])
	t.Logf("identical_report_content selected_depth=%s learning_status=%v learning_cases=%v", a["depth"], policy["status"], policy["cases"])
	if a["depth"] != "focused" {
		t.Fatal("changing only the observation kind multiplied identical report content into an adopted default")
	}
}

func TestReadingAuditAControls(t *testing.T) {
	t.Run("initial_stage_preserved", func(t *testing.T) {
		b, _ := auditABook(t)
		a := auditAAssignment("review-direct", "Violet direct task", "review")
		p := auditACall(t, b, "prepare", map[string]any{"assignment": a, "query": auditATopic})
		r := loopObject(t, p["reading"])
		if r["stage"] != "review" {
			t.Fatal("initial nonempty read did not retain stage")
		}
		t.Log("direct initial read retains review stage")
	})
	t.Run("later_pinned_recovery_counted", func(t *testing.T) {
		b, page := auditABook(t)
		first := auditACall(t, b, "prepare", map[string]any{"request": "Violet later recovery", "query": auditATopic, "reasoning": map[string]any{"depth": "brief"}})
		full := auditACall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
		expected := workContextSize(first["knowledge"]) + workContextSize(full["knowledge"])
		r := loopObject(t, full["reading"])
		if r["bytes"] != float64(expected) || r["reads"] != float64(2) || r["recoveries"] != float64(1) {
			t.Fatal("later pinned recovery was not counted")
		}
		t.Logf("later recovery actual_and_recorded_bytes=%d reads=2 recoveries=1", expected)
	})
	t.Run("identical_kind_content_deduplicated", func(t *testing.T) {
		b, _ := auditABook(t)
		for _, task := range []string{"Violet duplicate control one", "Violet duplicate control two"} {
			for _, depth := range []string{"focused", "brief"} {
				p := auditACall(t, b, "prepare", map[string]any{"request": task, "query": auditATopic, "reasoning": map[string]any{"depth": depth}})
				auditAFeedback(t, b, p["receipt"], nil, "The exact same synthetic checked result is reused here.")
			}
		}
		next := auditACall(t, b, "prepare", map[string]any{"request": "Violet control next", "query": auditATopic})
		a := loopObject(t, next["attention"])
		policy := loopObject(t, a["learning"])
		if a["depth"] != "focused" || policy["cases"] != float64(1) {
			t.Fatal("identical kind/content was not deduplicated")
		}
		t.Log("same kind and content retain focused with cases=1")
	})
}
