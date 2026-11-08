package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func autonomyRun(t *testing.T, b notebook.Book, task, evidence string, sufficient *bool) (map[string]any, map[string]any) {
	t.Helper()
	p := readingLearningPrepare(t, b, task, "")
	selected := p
	for i := 0; i < 3; i++ {
		var err error
		var next map[string]any
		next, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
		if err != nil {
			t.Fatal(err)
		}
		if len(next["how_to_record"].(string)) > 200 {
			t.Fatal("receipt extensions must not repeat the full recording instructions")
		}
		if loopObject(t, next["attention"])["procedure"] != nil {
			selected = next
		}
	}
	result := map[string]any{"changed": "Checked the requested source condition.", "evidence": []string{"new1"}}
	if sufficient != nil {
		result["sufficient"] = *sufficient
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(evidence)}, "result": result})
	if err != nil || r["complete"] != true {
		t.Fatal("one ending report must drive the procedure loop", r, err)
	}
	return selected, r
}

func TestWorkAutonomyRecoversIndexWithoutResendingResult(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Check recovery after a lost acknowledgement.", "")
	for i := 0; i < 3; i++ {
		if _, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
			t.Fatal(e)
		}
	}
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "procedure" {
			return errors.New("synthetic lost procedure acknowledgement")
		}
		return nil
	}
	r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Saved the original checked result."}})
	workAfterWrite = old
	if e != nil || r["complete"] != false || loopObject(t, r["storage"])["status"] != "saved-pending" {
		t.Fatal("control: the report is saved before indexing acknowledgement", r, e)
	}
	id := loopObject(t, r["storage"])["result_source"]
	if _, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
		t.Fatal(e)
	}
	token, _ := workDecode(p["receipt"].(string))
	dir, e := openWorkSessions(b.Root)
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	w, _, e := loadWorkState(dir, b.Scope, token)
	if e != nil || w.Reasoning.Procedure.Indexed != id {
		t.Fatal("prepare must recover the original report automatically", e)
	}
	if w.Reasoning.Finish == nil {
		t.Fatal("automatic recovery must finish the saved report without another recording call")
	}
	if w.Reasoning.Procedure.View.Status != "proposed" {
		t.Fatal("a replay cannot generate another proposal")
	}
	t.Run("next human request", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		page := reasoningPage(t, book, "Reading source recovery", "howto", strings.Repeat("Preserve the exact source condition and recovery path.\n\n", 80))
		if _, e := book.RecordLookup("Reading source recovery", []notebook.Page{page}); e != nil {
			t.Fatal(e)
		}
		session := "procedure-recovery"
		runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the original source condition."})
		first, e := workCall(t, book, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "Reading source recovery"})
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 3; i++ {
			if _, e = workCall(t, book, "prepare", map[string]any{"receipt": first["receipt"], "query": "Reading source recovery"}); e != nil {
				t.Fatal(e)
			}
		}
		dirs, e := filepath.Glob(filepath.Join(root, "records", "wiki-searches", "*"))
		if e != nil || len(dirs) != 1 {
			t.Fatal("locate the isolated search journal", dirs, e)
		}
		checkpoint := filepath.Join(dirs[0], ".procedure-policy-v1.json")
		if e = os.Mkdir(checkpoint, 0700); e != nil {
			t.Fatal(e)
		}
		failed, e := workCall(t, book, "record", map[string]any{"receipt": first["receipt"], "result": map[string]any{"changed": "The original source result was saved."}})
		if e != nil || failed["complete"] != false {
			t.Fatal("control: indexing must fail before publication", failed, e)
		}
		if e = os.Remove(checkpoint); e != nil {
			t.Fatal(e)
		}
		runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Now check a different source condition."})
		next, e := workCall(t, book, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "Reading source recovery"})
		if e != nil || next["procedure_recovery"] != nil {
			t.Fatal("the new request must resume the old saved feedback", next["procedure_recovery"], e)
		}
		data, e := os.ReadFile(checkpoint)
		if e != nil || !strings.Contains(string(data), "proposed") {
			t.Fatal("recover the unindexed prior result without resending it", e)
		}
	})
}

func TestWorkAutonomyGeneratesValidatesAdoptsAndRollsBack(t *testing.T) {
	b, _ := readingLearningBook(t)
	yes, no := true, false
	_, trained := autonomyRun(t, b, "Investigate source A.", "Source A's exact condition was checked.", nil)
	proposal := loopObject(t, trained["procedure"])
	ending := ""
	for _, raw := range trained["saved"].([]any) {
		v := loopObject(t, raw)
		if v["kind"] == "reasoning" {
			ending = v["id"].(string)
		}
	}
	material, e := b.ReadSources([]string{ending})
	if e != nil {
		t.Fatal(e)
	}
	var snapshot struct {
		Result    *workCompletionRef `json:"result"`
		Procedure string             `json:"procedure"`
	}
	if e = json.Unmarshal([]byte(material["observations"].([]notebook.Observation)[0].Content), &snapshot); e != nil || snapshot.Result == nil || len(snapshot.Result.Evidence) != 1 || snapshot.Procedure != proposal["source"] {
		t.Fatal("the immutable ending must retain evidence bindings and the procedure trace beyond session expiry", e)
	}
	if pending, e := b.PendingObservations(); e != nil || len(pending) != 1 {
		t.Fatal("used check evidence must not create another manual reflection", len(pending), e)
	}
	if proposal["status"] != "proposed" {
		t.Fatal("stalled work must generate a bounded proposal", proposal)
	}
	id := proposal["policy"]
	_, first := autonomyRun(t, b, "Investigate source B.", "Source B's separate condition was checked.", &yes)
	if v := loopObject(t, first["procedure"]); v["status"] != "validating" || v["cases"] != float64(1) {
		t.Fatal("one held-out task cannot adopt", v)
	}
	_, second := autonomyRun(t, b, "Investigate source C.", "Source C's other condition was checked.", &yes)
	if v := loopObject(t, second["procedure"]); v["status"] != "adopted" || v["policy"] != id || v["saved_bytes"].(float64) <= 0 {
		t.Fatal("separate checked tasks must adopt a cheaper procedure", v)
	}
	p, failed := autonomyRun(t, b, "Investigate source D.", "Source D's deciding condition was missed.", &no)
	if loopObject(t, loopObject(t, p["attention"])["procedure"])["status"] != "adopted" {
		t.Fatal("control: the adopted procedure must actually be selected")
	}
	if v := loopObject(t, failed["procedure"]); v["status"] != "rolled_back" || v["policy"] == id {
		t.Fatal("a quality failure must immediately restore its exact parent", v)
	}
	if loopObject(t, loopObject(t, failed["reasoning"])["latest"])["assessment"] != "unresolved" {
		t.Fatal("failed quality must remain unresolved in the ending review")
	}
	next := readingLearningPrepare(t, b, "Investigate source E.", "")
	if v, ok := loopObject(t, next["attention"])["procedure"].(map[string]any); ok && v["policy"] == id {
		t.Fatal("a rejected procedure cannot silently return", v)
	}
}

func TestWorkAutonomyStrengthensValidationAfterFailure(t *testing.T) {
	b, _ := readingLearningBook(t)
	yes, no := true, false
	_, initial := autonomyRun(t, b, "Generate the first method.", "Generating result.", nil)
	_, failed := autonomyRun(t, b, "Fail the first trial.", "A requested condition was missing.", &no)
	if loopObject(t, failed["procedure"])["status"] != "rejected" {
		t.Fatal("reject the failed trial")
	}
	_, next := autonomyRun(t, b, "Generate another method.", "Another independently investigated condition.", nil)
	v := loopObject(t, next["procedure"])
	if v["required"] != float64(3) || v["policy"] == loopObject(t, initial["procedure"])["policy"] {
		t.Fatal("change the failed recipe and strengthen its validation", v)
	}
	for i := 0; i < 3; i++ {
		_, r := autonomyRun(t, b, fmt.Sprintf("Verify the safer method on task %d.", i), fmt.Sprintf("Task %d's distinct source condition was checked.", i), &yes)
		status := loopObject(t, r["procedure"])["status"]
		if i < 2 && status == "adopted" || i == 2 && status != "adopted" {
			t.Fatal("the new method must pass the increased held-out requirement", i, r["procedure"])
		}
	}
}

func TestWorkAutonomySeparatesOriginUnknownAndRepeatedEvidence(t *testing.T) {
	b, _ := readingLearningBook(t)
	yes := true
	_, train := autonomyRun(t, b, "Source origin.", "The original training condition was checked.", nil)
	if train["procedure"] == nil {
		t.Fatal("generate the proposal first")
	}
	p, r := autonomyRun(t, b, "Source origin.", "The same task cannot validate its own proposal.", &yes)
	if v, ok := loopObject(t, p["attention"])["procedure"].(map[string]any); ok && v["status"] == "validating" {
		t.Fatal("the generating task is excluded", v)
	}
	if v := loopObject(t, r["procedure"]); v["cases"] != float64(0) {
		t.Fatal("origin cannot supply support", v)
	}
	for i := 0; i < 2; i++ {
		_, r = autonomyRun(t, b, fmt.Sprintf("Unknown source %d.", i), fmt.Sprintf("An ungraded report for source %d.", i), nil)
		if v := loopObject(t, r["procedure"]); v["status"] == "adopted" || v["cases"] != float64(0) {
			t.Fatal("delivery and completion cannot invent quality", v)
		}
	}
	for _, task := range []string{"Checked source one.", "Checked source two."} {
		_, r = autonomyRun(t, b, task, "The same evidence text cannot establish separate validation.", &yes)
		if v := loopObject(t, r["procedure"]); v["status"] == "adopted" {
			t.Fatal("renaming a task and source cannot multiply evidence", v)
		}
	}
}

func TestWorkAutonomyKeepsExplicitRecoveryAndChangedRevisions(t *testing.T) {
	b, page := readingLearningBook(t)
	yes := true
	autonomyRun(t, b, "Generate from the source.", "The generating source check.", nil)
	p := readingLearningPrepare(t, b, "Check a different source condition.", "")
	full, err := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
	if err != nil || loopObject(t, full["knowledge"].([]any)[0])["body_included"] != true {
		t.Fatal("a policy cannot suppress explicit source recovery", err)
	}
	regressionChangeArticle(t, b, page, "updated")
	_, r := autonomyRun(t, b, "Inspect the changed source.", "The changed source condition is independently checked.", &yes)
	if v, ok := r["procedure"].(map[string]any); ok && (v["status"] == "adopted" || v["cases"] != float64(0)) {
		t.Fatal("changed revisions cannot inherit support", v)
	}
	if strings.Contains(fmt.Sprint(r), "permission granted") {
		t.Fatal("learning is never permission")
	}
}

func TestWorkAutonomyRechecksValidationSourcesBeforeReuse(t *testing.T) {
	b, _ := readingLearningBook(t)
	yes := true
	autonomyRun(t, b, "Train the method.", "Training source.", nil)
	_, support := autonomyRun(t, b, "Check held-out ownership.", "Ownership was independently checked.", &yes)
	autonomyRun(t, b, "Check held-out totals.", "Totals were independently checked.", &yes)
	id := ""
	for _, raw := range support["saved"].([]any) {
		v := loopObject(t, raw)
		if v["kind"] == "observation" {
			id = v["id"].(string)
			break
		}
	}
	paths, e := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", id+".json"))
	if e != nil || len(paths) != 1 {
		t.Fatal("find the synthetic check source", paths, e)
	}
	if e = os.Remove(paths[0]); e != nil {
		t.Fatal(e)
	}
	p := readingLearningPrepare(t, b, "Inspect the source after evidence loss.", "")
	p, e = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"})
	if e != nil {
		t.Fatal(e)
	}
	v := loopObject(t, loopObject(t, p["attention"])["procedure"])
	if v["policy"] != "loop-v1" || v["status"] != "rolled_back" {
		t.Fatal("missing validation evidence must restore the parent", v)
	}
}

func TestWorkAutonomyRejectsMoreExpensiveChildWithoutLosingParent(t *testing.T) {
	b, _ := readingLearningBook(t)
	yes := true
	autonomyRun(t, b, "Generate a parent method.", "Parent training condition.", nil)
	autonomyRun(t, b, "Verify parent on ownership.", "Ownership is correctly checked.", &yes)
	_, adopted := autonomyRun(t, b, "Verify parent on totals.", "Totals are correctly checked.", &yes)
	parent := loopObject(t, adopted["procedure"])["policy"]
	_, generated := autonomyRun(t, b, "Find remaining parent overhead.", "The additional source condition was checked.", &yes)
	if loopObject(t, generated["procedure"])["status"] != "proposed" {
		t.Fatal("generate an improvement to the adopted method")
	}
	_, result := autonomyRun(t, b, "Check the child procedure.", "The child's checked answer retains its required condition.", &yes)
	v := loopObject(t, result["procedure"])
	if v["status"] != "rejected" || v["policy"] != parent {
		t.Fatal("quality alone cannot justify a child that costs more than its actual parent", v)
	}
}

func TestWorkAutonomyKeepsHumanStatementsForSemanticReview(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Retain the person's stated preference.", "")
	r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "human", "title": "Synthetic user preference", "content": "Keep the explicit owner confirmation."}}, "result": map[string]any{"changed": "Retained the preference for semantic review.", "evidence": []string{"new1"}}})
	if e != nil || r["complete"] != true {
		t.Fatal(r, e)
	}
	pending, e := b.PendingObservations()
	if e != nil {
		t.Fatal(e)
	}
	for _, o := range pending {
		if o.Kind == "human" && o.Title == "Synthetic user preference" {
			return
		}
	}
	t.Fatal("automatic execution-check cleanup must not consume a human preference")
}
