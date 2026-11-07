package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkReadingLearningInterruptedIndexRecoversSameExecution(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover interrupted reading evidence.", "brief")
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "reading" {
			return errors.New("synthetic lost reading acknowledgement")
		}
		return nil
	}
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The actual interrupted reading answer was checked.")}, "reading": map[string]any{"check": "Recover the requested source.", "sufficient": true, "evidence": []string{"new1"}}})
	workAfterWrite = old
	if err != nil || v["complete"] != false {
		t.Fatalf("setup must interrupt after the reading index was written: %v %v", v, err)
	}
	ref := loopObject(t, v["errors"].([]any)[0])["ref"]
	if ref == "reading" || ref == nil {
		t.Fatal("an interrupted reading needs a durable recovery reference")
	}
	if _, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); err != nil {
		t.Fatal(err)
	}
	v, err = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": map[string]any{"ref": ref}})
	if err != nil || v["complete"] != true {
		t.Fatalf("reference-only retry must recover the original reading: %v %v", v, err)
	}
}

func readingLearningBook(t *testing.T) (notebook.Book, notebook.Page) {
	t.Helper()
	b, _ := loopBook(t)
	p := reasoningPage(t, b, "Reading source recovery", "howto", strings.Repeat("Inspect the exact source revision before relying on a recovery procedure.\n\n", 80))
	return b, p
}

func readingLearningPrepare(t *testing.T, b notebook.Book, task, depth string) map[string]any {
	t.Helper()
	in := map[string]any{"request": task, "query": "Reading source recovery"}
	if depth != "" {
		in["reasoning"] = map[string]any{"depth": depth}
	}
	v, err := workCall(t, b, "prepare", in)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func readingLearningFeedback(t *testing.T, b notebook.Book, p map[string]any, sufficient bool, evidence string) map[string]any {
	t.Helper()
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Checked reading answer", "content": evidence}}, "reading": map[string]any{"check": "The requested source condition is correctly recovered.", "sufficient": sufficient, "evidence": []string{"new1"}}})
	if err != nil || v["complete"] != true {
		t.Fatalf("reading feedback must be saved with measured delivery and evidence: %v %v", v, err)
	}
	return v
}

func readingLearningPair(t *testing.T, b notebook.Book, task string) {
	t.Helper()
	for _, depth := range []string{"focused", "brief"} {
		p := readingLearningPrepare(t, b, task, depth)
		readingLearningFeedback(t, b, p, true, fmt.Sprintf("Checked answer for %s using %s: the requested revision was retained.", task, depth))
	}
}

func TestWorkReadingLearningAdoptsAfterDistinctCheckedCases(t *testing.T) {
	b, _ := readingLearningBook(t)
	readingLearningPair(t, b, "Recover the first source condition.")
	p := readingLearningPrepare(t, b, "Recover the second source condition.", "")
	a := loopObject(t, p["attention"])
	learning := loopObject(t, a["learning"])
	if a["depth"] != "focused" || learning["status"] != "candidate" || learning["next_depth"] != "brief" {
		t.Fatalf("one case may suggest a verification, never adopt it: %v", a)
	}
	readingLearningPair(t, b, "Recover the second source condition.")
	p = readingLearningPrepare(t, b, "Recover an unseen third source condition.", "")
	a = loopObject(t, p["attention"])
	learning = loopObject(t, a["learning"])
	if a["depth"] != "brief" || learning["status"] != "adopted" || learning["cases"] != float64(2) {
		t.Fatalf("two separately checked cases must change the next default: %v", a)
	}
	if loopObject(t, p["reasoning"])["started"] != true {
		t.Fatal("learning must not invent a reasoning start")
	}
}

func TestWorkReadingLearningRetractsOnObservedInsufficiency(t *testing.T) {
	b, _ := readingLearningBook(t)
	readingLearningPair(t, b, "Recover source A.")
	readingLearningPair(t, b, "Recover source B.")
	p := readingLearningPrepare(t, b, "Recover source C.", "")
	readingLearningFeedback(t, b, p, false, "The supplied excerpts omitted the deciding condition for source C.")
	next := readingLearningPrepare(t, b, "Recover source D.", "")
	a := loopObject(t, next["attention"])
	if a["depth"] != "focused" || loopObject(t, a["learning"])["status"] != "suspended" {
		t.Fatalf("a new counterexample must retract an adopted default: %v", a)
	}
	if loopObject(t, a["learning"])["next_depth"] == "brief" {
		t.Fatal("the next experiment must learn from the failed candidate")
	}
}

func TestWorkReadingLearningRetainsCounterexampleAfterLaterSuccess(t *testing.T) {
	b, _ := readingLearningBook(t)
	readingLearningPair(t, b, "Recover source A.")
	readingLearningPair(t, b, "Recover source B.")
	p := readingLearningPrepare(t, b, "Recover source C.", "brief")
	readingLearningFeedback(t, b, p, false, "Source C had a missing condition.")
	p = readingLearningPrepare(t, b, "Recover source C.", "brief")
	readingLearningFeedback(t, b, p, true, "A later successful reading of source C cannot erase the previous omission.")
	next := readingLearningPrepare(t, b, "Recover source D.", "")
	if loopObject(t, next["attention"])["depth"] != "focused" {
		t.Fatal("a later success must preserve the earlier counterexample under the same scope")
	}
}

func TestWorkReadingLearningReplacesFailedCandidateWithVerifiedAlternative(t *testing.T) {
	b, _ := readingLearningBook(t)
	readingLearningPair(t, b, "Recover source A.")
	readingLearningPair(t, b, "Recover source B.")
	p := readingLearningPrepare(t, b, "Recover source C.", "brief")
	readingLearningFeedback(t, b, p, false, "The brief read missed source C.")
	for _, task := range []string{"Recover a difficult source D.", "Recover a difficult source E."} {
		p = readingLearningPrepare(t, b, task, "focused")
		readingLearningFeedback(t, b, p, false, task+" remained incomplete under focused.")
		p = readingLearningPrepare(t, b, task, "deep")
		readingLearningFeedback(t, b, p, true, task+" was recovered under deep.")
	}
	next := readingLearningPrepare(t, b, "Recover another difficult source.", "")
	if loopObject(t, next["attention"])["depth"] != "deep" || loopObject(t, loopObject(t, next["attention"])["learning"])["status"] != "adopted" {
		t.Fatal("verified deeper recovery must replace a suspended smaller-read candidate")
	}
}

func TestWorkReadingLearningDoesNotCountRenamedEvidenceTwice(t *testing.T) {
	b, _ := readingLearningBook(t)
	for _, task := range []string{"Recover source A.", "Recover source B."} {
		for _, depth := range []string{"focused", "brief"} {
			p := readingLearningPrepare(t, b, task, depth)
			readingLearningFeedback(t, b, p, true, "The same checked result must not become another independent case.")
		}
	}
	p := readingLearningPrepare(t, b, "Recover source C.", "")
	a := loopObject(t, p["attention"])
	if a["depth"] != "focused" || loopObject(t, a["learning"])["cases"] != float64(1) {
		t.Fatalf("duplicate content with new source IDs must not multiply support: %v", a)
	}
}

func TestWorkReadingLearningMeasuresRecoveryAndKeepsReceiptHistory(t *testing.T) {
	b, page := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover a condition omitted by the excerpt.", "brief")
	initial, ok := loopObject(t, p["reading"])["bytes"].(float64)
	if !ok {
		t.Fatal("prepare must return measured reading delivery")
	}
	x, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
	if err != nil {
		t.Fatal(err)
	}
	r := loopObject(t, x["reading"])
	if r["bytes"].(float64) <= initial || r["recoveries"] != float64(1) || r["reads"] != float64(2) {
		t.Fatalf("full recovery must count toward actual delivered cost: %v", r)
	}
	readingLearningFeedback(t, b, p, true, "After the additional full read, the condition was recovered.")
	readingLearningFeedback(t, b, p, true, "After the additional full read, the condition was recovered.")
	if v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reading": map[string]any{"check": "The requested source condition is correctly recovered.", "sufficient": true, "evidence": []string{"missing"}}}); err == nil && v["complete"] == true {
		t.Fatal("unbound reading-quality evidence must be refused")
	}
}

func TestWorkReadingLearningChargesFullRecoveryBeforeChoosing(t *testing.T) {
	b, page := readingLearningBook(t)
	for _, task := range []string{"Recover a whole source A.", "Recover a whole source B."} {
		p := readingLearningPrepare(t, b, task, "focused")
		readingLearningFeedback(t, b, p, true, task+" focused answer was checked.")
		p = readingLearningPrepare(t, b, task, "brief")
		if _, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision}); err != nil {
			t.Fatal(err)
		}
		readingLearningFeedback(t, b, p, true, task+" brief answer needed the full source.")
	}
	p := readingLearningPrepare(t, b, "Recover whole source C.", "")
	if loopObject(t, p["attention"])["depth"] != "focused" {
		t.Fatal("small excerpts followed by a costly full read must not win on initial bytes alone")
	}
}

func TestWorkReadingLearningRejectsMixedQueriesAndMissingQuality(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Recover source A.", "brief")
	if _, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "source revision"}); err != nil {
		t.Fatal(err)
	}
	for _, reading := range []any{map[string]any{"check": "Check the source.", "evidence": []string{"new1"}}, map[string]any{"check": "Check the source.", "sufficient": true, "evidence": []string{"new1"}}} {
		v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("A checked source condition.")}, "reading": reading})
		if err == nil && v["complete"] == true {
			t.Fatal("missing quality or mixed searches must not teach a reading default")
		}
	}
}

func TestWorkReadingLearningKeepsExplicitDepthAndInvalidatesChangedRevision(t *testing.T) {
	b, page := readingLearningBook(t)
	readingLearningPair(t, b, "Recover source A.")
	readingLearningPair(t, b, "Recover source B.")
	p := readingLearningPrepare(t, b, "Inspect deeply despite the learned default.", "deep")
	if loopObject(t, p["attention"])["depth"] != "deep" {
		t.Fatal("the agent's explicit depth must remain authoritative")
	}
	_, err := b.Patch(notebook.Patch{ID: page.ID, Revision: page.Revision, Sources: []string{page.Sources[0].ID}, ChangeReason: "The recovery condition changed.", Edits: []notebook.TextEdit{{Field: "lesson", Before: page.Lesson, After: "Inspect the updated source condition."}}})
	if err != nil {
		t.Fatal(err)
	}
	p = readingLearningPrepare(t, b, "Recover a condition under the changed revision.", "")
	if loopObject(t, p["attention"])["depth"] != "focused" {
		t.Fatal("an old article revision must not select a learned default")
	}
}

func TestWorkReadingRejectsInconsistentQualityBeforeAcceptance(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Check a malformed quality report.", "brief")
	content := `{"type":"reading-quality/v1","check":"A different quality check.","sufficient":true,"observed":"Checked the condition."}`
	v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(content)}, "reading": map[string]any{"check": "Keep the source condition.", "sufficient": true, "evidence": []string{"new1"}}})
	if err != nil || v["complete"] != false {
		t.Fatal("expected a rejected inconsistent quality report", err)
	}
	var state struct {
		Reasoning struct {
			Readings []json.RawMessage `json:"reading_executions"`
		}
	}
	data, _ := json.Marshal(v)
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Reasoning.Readings) != 0 {
		t.Fatal("a rejected quality result created an unrecoverable pending execution")
	}
}
