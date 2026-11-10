package main

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// A report recorded again after later evidence is accepted as the review of the current work (its review generation moves), even when
// its text did not change. Its internal ending review must then cover the current generation too; otherwise the reply says saved and
// complete while the turn can never finish, and the next_action it gives (result:{ref}) only repeats the same reply.
func TestWorkSameResultAfterLaterEvidenceFinishesTheCurrentGeneration(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "same-result"
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Check the synthetic export and report the result."})
	p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "synthetic export"})
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"changed": "The synthetic export was checked."}
	first, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": result})
	if err != nil || first["complete"] != true || loopObject(t, first["reasoning"])["complete"] != true {
		t.Fatal("the first report must finish the turn", first, err)
	}
	s := loadSession(root, session)
	if err := verifyStopReasoning(root, s, s); err != nil {
		t.Fatal("the first report must let the turn stop", err)
	}
	later, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Later synthetic check", "content": "A later synthetic check also passed."}}})
	if err != nil || len(later["saved"].([]any)) != 1 || loopObject(t, later["reasoning"])["complete"] != false {
		t.Fatal("later evidence must be saved and reopen the ending review", later, err)
	}
	check := func(step string, reply map[string]any) {
		t.Helper()
		s := loadSession(root, session)
		stop := verifyStopReasoning(root, s, s)
		reasoning := loopObject(t, reply["reasoning"])
		t.Logf("%s: complete=%v storage=%v errors=%v reasoning.complete=%v generation=%d finish.generation=%d result.review_generation=%d stop=%v next=%v", step, reply["complete"], reply["storage"], reply["errors"], reasoning["complete"], s.Reasoning.Generation, s.Reasoning.Finish.Generation, s.Reasoning.Result.ReviewGeneration, stop, reasoning["next"])
		if reply["complete"] != true || loopObject(t, reply["storage"])["status"] != "saved" {
			t.Fatalf("%s: the report must be accepted for the current work: %v", step, reply)
		}
		if reasoning["complete"] != true || stop != nil {
			t.Errorf("%s: the reply says saved and complete, but the ending review still belongs to the earlier generation: reasoning.complete=%v stop=%v", step, reasoning["complete"], stop)
		}
	}
	again, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": result})
	if err != nil {
		t.Fatal(err)
	}
	check("same report again", again)
	// What the reply's next_action tells the agent to do when the review is not complete.
	byRef, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": loopObject(t, again["storage"])["result_source"]}})
	if err != nil {
		t.Fatal(err)
	}
	check("result ref after it", byRef)
}
