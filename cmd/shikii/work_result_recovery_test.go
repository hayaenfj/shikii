package main

import (
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"os"
	"testing"
	"time"
)

func TestWorkResultEvidenceRecoveryKeepsOriginalTurn(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "result-binding-recovery"
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the first synthetic condition."})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepare := func() map[string]any {
		p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "synthetic condition"})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	first := prepare()
	r, e := workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "result": map[string]any{"changed": "The original synthetic check is retained.", "sufficient": true, "evidence": []string{"missing-check"}}})
	if e != nil || r["complete"] != false {
		t.Fatal("the missing check must remain unresolved", r, e)
	}
	original := loopObject(t, r["storage"])["result_source"].(string)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check a different synthetic condition."})
	p := prepare()
	finish := reasoningInput("finish", "current")
	end, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": finish})
	if e != nil || end["complete"] != false {
		t.Fatal("a new turn cannot discard the unresolved original", end, e)
	}
	repaired, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The first original check is now available.")}, "result": map[string]any{"ref": original, "evidence": []string{"new1"}}})
	if e != nil || repaired["complete"] != true || loopObject(t, repaired["storage"])["result_source"] != original {
		t.Fatal("recover only the binding and retain the original body", repaired, e)
	}
	if loopObject(t, repaired["reasoning"])["complete"] == true {
		t.Fatal("repair of the old check cannot finish the new task")
	}
	// A renewed receipt still recognizes the saved recovery and its pinned proof.
	p = prepare()
	replay, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original}})
	if e != nil || replay["complete"] != true {
		t.Fatal("a confirmed recovery must survive renewed preparation without resending evidence", replay, e)
	}
	invalid, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original, "evidence": []string{"missing-recovery-evidence"}}})
	if e == nil && invalid["complete"] == true {
		t.Fatal("an unrelated alias cannot replace the pinned recovery proof")
	}
	current, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the second synthetic condition."}})
	if e != nil || current["complete"] != true || loopObject(t, current["reasoning"])["complete"] != true {
		t.Fatal("the actual new report may finish its own task", current, e)
	}
	material, e := b.ReadSources([]string{original})
	if e != nil {
		t.Fatal(e)
	}
	if material["observations"].([]notebook.Observation)[0].Content != `{"sufficient":true,"changed":"The original synthetic check is retained.","evidence":["missing-check"]}` {
		t.Fatal("recovery rewrote the original report")
	}
}

func TestWorkResultChangedProofKeepsDistinctSnapshots(t *testing.T) {
	b, _ := readingLearningBook(t)
	p := readingLearningPrepare(t, b, "Keep each actual proof bound to its own result.", "")
	for i := 0; i < 3; i++ {
		if _, e := workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "Reading source recovery"}); e != nil {
			t.Fatal(e)
		}
	}
	report := map[string]any{"changed": "The synthetic condition was checked.", "sufficient": true, "evidence": []string{"new1"}}
	call := func(text string) map[string]any {
		r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation(text)}, "result": report})
		if e != nil || r["complete"] != true {
			t.Fatal("the changed proof must create a valid immutable execution", r, e)
		}
		return r
	}
	first := call("The first actual synthetic proof.")
	second := call("The second actual synthetic proof.")
	again := call("The second actual synthetic proof.")
	id := func(r map[string]any) any { return loopObject(t, r["storage"])["result_source"] }
	if id(first) == id(second) || id(second) != id(again) {
		t.Fatal("changed proof needs a distinct snapshot and identical replay must reuse it")
	}
}

func TestWorkResultRecoveryHistoryPreservesPending(t *testing.T) {
	previous := &reasoningProgress{PendingResults: map[string]*workResultRecovery{}}
	for i := 0; i < 70; i++ {
		previous.PendingResults[fmt.Sprintf("%016x", i)] = &workResultRecovery{Complete: true, CompletedAt: time.Unix(int64(i), 0)}
	}
	previous.PendingResults["pending-a"] = &workResultRecovery{}
	previous.PendingResults["pending-b"] = &workResultRecovery{}
	current := nextTurnReasoning(previous, "next")
	if len(current.PendingResults) != 66 || current.PendingResults["pending-a"] == nil || current.PendingResults["pending-b"] == nil || current.PendingResults[fmt.Sprintf("%016x", 69)] == nil {
		t.Fatal("bound completed receipts while retaining unresolved originals and recent repairs")
	}
}

func TestWorkPendingResultSurvivesReceiptExpiryAndPruning(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "expiry-recovery"
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Retain an unresolved check beyond receipt expiry."})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "The original synthetic check is saved.", "sufficient": true, "evidence": []string{"missing-check"}}})
	if e != nil || r["complete"] != false {
		t.Fatal(r, e)
	}
	token, _ := workDecode(p["receipt"].(string))
	state := loadSession(b.Root, token.Session)
	state.Planning.Work.At = time.Now().Add(-hookSessionTTL - time.Hour)
	if e = saveSession(b.Root, token.Session, state); e != nil {
		t.Fatal(e)
	}
	path := sessionPath(b.Root, token.Session)
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if e = os.Chtimes(path, old, old); e != nil {
		t.Fatal(e)
	}
	pruneSessions(b.Root)
	if _, e = os.Stat(path); e != nil {
		t.Fatal("session cleanup discarded an accepted unresolved result", e)
	}
	if _, e = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "An expired receipt cannot write."}}); e == nil {
		t.Fatal("retaining pending work must not revive the expired receipt")
	}
	next, e := workCall(t, b, "prepare", map[string]any{"session_id": token.Session, "turn_id": token.Turn, "query": "synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	original := loopObject(t, r["storage"])["result_source"]
	repaired, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "observations": []any{trialObservation("The original expired-context check is now available.")}, "result": map[string]any{"ref": original, "evidence": []string{"new1"}}})
	if e != nil || repaired["complete"] != true {
		t.Fatal("a fresh receipt must recover the retained original without repeating its body", repaired, e)
	}
}

func TestSessionPruningCannotDeleteLockedCurrentState(t *testing.T) {
	root := t.TempDir()
	id := "synthetic-prune-lock"
	if e := saveSession(root, id, &hookSession{TurnID: "0000000000000001"}); e != nil {
		t.Fatal(e)
	}
	path := sessionPath(root, id)
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if e := os.Chtimes(path, old, old); e != nil {
		t.Fatal(e)
	}
	unlock, ok := lockSessionWithin(root, id, 0)
	if !ok {
		t.Fatal("lock fixture")
	}
	pruneSessions(root)
	_, e := os.Stat(path)
	unlock()
	if e != nil {
		t.Fatal("cleanup removed state owned by another writer", e)
	}
	pruneSessions(root)
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("inactive resolved state may still be cleaned", e)
	}
}

func TestWorkSavedReadyResultFinishesByRefAfterPreparation(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "ready-result-recovery"
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the saved synthetic result."})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepare := func() map[string]any {
		p, e := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "synthetic"})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	p := prepare()
	r, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The original synthetic check passed.")}, "result": map[string]any{"changed": "The checked result is retained.", "sufficient": true, "evidence": []string{"new1"}}, "reasoning": reasoningInput("update", "new1")})
	if e != nil || r["complete"] != true || loopObject(t, r["reasoning"])["complete"] == true {
		t.Fatal("save a ready report before its explicit ending", r, e)
	}
	original := loopObject(t, r["storage"])["result_source"]
	p = prepare()
	ended, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original}})
	if e != nil || ended["complete"] != true || loopObject(t, ended["reasoning"])["complete"] != true || loopObject(t, ended["storage"])["result_source"] != original || auditR2Count(t, b, "Work result and remaining conditions") != 1 {
		t.Fatal("finish only the saved ref without duplicating body or evidence", ended, e)
	}
	if _, e = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("A new synthetic condition needs a new report.")}}); e != nil {
		t.Fatal(e)
	}
	stale, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original}})
	if e != nil || stale["complete"] != false || loopObject(t, stale["reasoning"])["complete"] == true {
		t.Fatal("a ref-only replay cannot finish changed work", stale, e)
	}
}

func TestSessionPruningRetainsUnverifiablePendingEntries(t *testing.T) {
	for _, kind := range []string{"reading", "trial", "comparison", "result", "procedure"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			id := "synthetic-unverifiable-" + kind
			r := &reasoningProgress{}
			switch kind {
			case "reading":
				r.Readings = map[string]*workReadingState{"unknown": nil}
			case "trial":
				r.Trials = map[string]*workTrialState{"unknown": nil}
			case "comparison":
				r.Comparisons = map[string]*workComparisonView{"unknown": nil}
			case "result":
				r.PendingResults = map[string]*workResultRecovery{"unknown": nil}
			case "procedure":
				r.PendingProcedures = []*workProcedureProgress{nil}
			}
			if e := saveSession(root, id, &hookSession{TurnID: "0000000000000001", Reasoning: r}); e != nil {
				t.Fatal(e)
			}
			path := sessionPath(root, id)
			old := time.Now().Add(-hookSessionTTL - time.Hour)
			if e := os.Chtimes(path, old, old); e != nil {
				t.Fatal(e)
			}
			pruneSessions(root)
			if _, e := os.Stat(path); e != nil {
				t.Fatal("unverifiable pending state must be retained without disrupting other work", e)
			}
		})
	}
}
