package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Audit of 0.33.0 (U-8): 0.33.0 told a receipt changed while copying from one of an earlier request, but a record by the hook's
// ids whose turn_id was copied with a character changed was still refused with "a newer request has started ... prepare again".
// No newer request had started, and preparing again replaces the hook's preparation. A turn id is 16 random hex characters, so an
// id a few characters from the ongoing request's is that id changed while copying: the refusal says so and asks for the ids as the
// hook gave them. The id of an earlier request keeps the old refusal. An id far from every known one (a made-up id) cannot be told
// from one of a request the session record no longer keeps, and keeps it too.
func TestAHookTurnIDCopiedWithACharacterChangedIsNotCalledAnEarlierRequest(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain archive approval."})
	earlier := loadSession(root, "planning").TurnID
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain the current recovery conditions."})
	s := loadSession(root, "planning")
	if s.Planning == nil || s.Planning.Work == nil || !s.Planning.Work.HookPrepared {
		t.Fatal("precondition: the hook prepared the current request")
	}
	result := map[string]any{"changed": "Explained the recovery conditions."}
	record := func(turn string) (map[string]any, error) {
		return workCall(t, b, "record", map[string]any{"session_id": "planning", "turn_id": turn, "result": result})
	}
	const hex = "0123456789abcdef"
	changed := func(at ...int) string { // the ongoing turn id with the characters at these places changed
		c := []byte(s.TurnID)
		for _, i := range at {
			c[i] = hex[(strings.IndexByte(hex, c[i])+1)%16]
		}
		return string(c)
	}
	cases := map[string]string{"one character changed": changed(1), "two characters changed": changed(0, 9)}
	if swapped := []byte(s.TurnID); swapped[3] != swapped[4] {
		swapped[3], swapped[4] = swapped[4], swapped[3]
		cases["two characters swapped"] = string(swapped)
	}
	for name, turn := range cases {
		_, err := record(turn)
		if err == nil || strings.Contains(err.Error(), "a newer request has started") || !strings.Contains(err.Error(), "changed while copying") || !strings.Contains(err.Error(), "exactly as the hook notice gave them") {
			t.Errorf("a turn_id with %s is refused as a copy, not as an earlier request: %v", name, err)
		}
	}
	for name, turn := range map[string]string{"the earlier request's turn_id": earlier, "a made-up turn_id": "0123456789abcdef"} {
		if turn == s.TurnID {
			continue
		}
		if _, err := record(turn); err == nil || !strings.Contains(err.Error(), "a newer request has started") {
			t.Errorf("%s keeps the refusal of an earlier request: %v", name, err)
		}
	}
	// The bound: three changed characters are a copy, four are not; and a near id that is a request the session keeps is that
	// request's.
	if !copiedTurnID(changed(0, 5, 10), s.TurnID) || copiedTurnID(changed(0, 5, 10, 15), s.TurnID) || copiedTurnID(s.TurnID, s.TurnID) {
		t.Errorf("copiedTurnID takes up to three changed characters: %s", s.TurnID)
	}
	kept := &planningFrame{Recent: []planningTurn{{ID: changed(1)}}}
	if !earlierTurnID(kept, changed(1)) || earlierTurnID(kept, changed(2)) {
		t.Error("earlierTurnID names only the turns the session keeps")
	}
	// When a newer turn has started without a preparation of its own, a near copy of that turn's id is not the prepared request's:
	// sending it exactly would be refused as an earlier one, so the refusal must not say no newer request has started.
	newer := &hookSession{Scope: s.Scope, TurnID: s.TurnID, Planning: &planningFrame{TurnID: earlier}}
	if err := currentPlanningFrame(newer, changed(1), s.Scope); err == nil || strings.Contains(err.Error(), "changed while copying") {
		t.Errorf("a copy of a turn the hook did not prepare is refused as an earlier request: %v", err)
	}
	if r, err := record(s.TurnID); err != nil || r["complete"] != true {
		t.Fatalf("the ids as the hook gave them still record after the refusals: %v %v", r, err)
	}
}
