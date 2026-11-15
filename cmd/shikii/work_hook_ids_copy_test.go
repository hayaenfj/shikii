package main

import (
	"strings"
	"testing"
	"time"

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

// Confirmation audit of 0.34.0 (U-C, U-F): a turn_id a few characters from the ongoing request's is refused by the request it names.
// One the session keeps as an earlier request (in Recent or Earlier) is that request's, not a copy, so it keeps the refusal of an
// earlier request (U-C: no test failed when that condition was taken out, as random ids are never that close). A copy of an ongoing
// request that is closed or past the session limit gets that request's refusal at once (U-F: it first said "send it exactly", and the
// exact id was then refused as closed, one more round trip).
func TestATurnIDNearTheOngoingOneIsRefusedByTheRequestItNames(t *testing.T) {
	const current, near, scope = "0123456789abcdef", "0123456789abcdee", "/p"
	session := func(change func(*hookSession)) *hookSession {
		s := &hookSession{Scope: scope, TurnID: current, Planning: &planningFrame{TurnID: current, At: time.Now()}}
		change(s)
		return s
	}
	refusal := func(s *hookSession, turn string) string {
		if err := currentPlanningFrame(s, turn, scope); err != nil {
			return err.Error()
		}
		return ""
	}
	if got := refusal(session(func(*hookSession) {}), near); !strings.Contains(got, "changed while copying") {
		t.Fatalf("precondition: a near id of an open request is a copy: %q", got)
	}
	for name, keep := range map[string]func(*hookSession){
		"Recent":  func(s *hookSession) { s.Planning.Recent = []planningTurn{{ID: near}} },
		"Earlier": func(s *hookSession) { s.Planning.Earlier = []planningTurn{{ID: near}} },
	} {
		if got := refusal(session(keep), near); !strings.Contains(got, "a newer request has started") || strings.Contains(got, "changed while copying") {
			t.Errorf("an earlier request the session keeps in %s is refused as a copy: %q", name, got)
		}
	}
	for name, c := range map[string]struct {
		change func(*hookSession)
		says   string
	}{
		"closed":  {func(s *hookSession) { s.Judged, s.Planning.Result = true, &planningReceipt{} }, "was closed at the end of its turn"},
		"expired": {func(s *hookSession) { s.Planning.At = time.Now().Add(-hookSessionTTL - time.Hour) }, "older than the session limit"},
	} {
		exact, copied := refusal(session(c.change), current), refusal(session(c.change), near)
		if !strings.Contains(exact, c.says) {
			t.Fatalf("precondition: the exact id of a %s request is refused with %q: %q", name, c.says, exact)
		}
		if copied != exact {
			t.Errorf("a copy of the id of the %s request is not refused as that request at once: %q (the exact id: %q)", name, copied, exact)
		}
	}
}
