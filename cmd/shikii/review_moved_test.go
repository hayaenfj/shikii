package main

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.7: an AI score given before the project folder was moved still carries the old path. The examples a person corrected went back to
// the next scorer only when that path equalled today's, so after a move every earlier correction silently stopped coming back.
// The scores live in this project's own store, so the path written in them says nothing about which project they belong to.
func TestCorrectedScoreFromBeforeAFolderMoveStillReturns(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	_, trace := reviewSeed(t, st, scope, 1)
	sp, err := spec.Load(st.Dir + "/decisions/done-as-asked.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveReview(store.Review{Spec: sp.ID, TraceID: trace, Scope: "/the/folder/before/the/move", Truth: "yes", Reason: "\u5b8c\u4e86\u3057\u305f\u3068\u4eee\u63a1\u70b9"}); err != nil {
		t.Fatal(err)
	}
	// The same score sent again from the moved folder is the same score, not a second one
	if _, err := st.SaveReview(store.Review{Spec: sp.ID, TraceID: trace, Scope: scope, Truth: "yes", Reason: "\u5b8c\u4e86\u3057\u305f\u3068\u4eee\u63a1\u70b9"}); err != nil {
		t.Fatalf("resending the same score after the move: %v", err)
	}
	capture(t, func() error { return cmdLabel([]string{"--store", st.Dir, "--source", "audit", trace, "no"}) })
	view, err := reviewQuery(st, scope)
	if err != nil {
		t.Fatal(err)
	}
	if got := view["human_corrections"].([]map[string]any); len(got) != 1 {
		t.Fatalf("the person's correction of a score given before the move goes back to the next scorer: %v", got)
	}
}
