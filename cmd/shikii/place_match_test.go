package main

// Items from the second recheck of 0.8.3 deferred to 0.9 (new-B and test holes: direction of ties, lowercasing), checked through matching (matchPlacedDraft) alone.

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func placedRecs(now time.Time, page string, placed ...string) []store.Record {
	recs := []store.Record{}
	for i, p := range placed {
		recs = append(recs, store.Record{TraceID: "t" + string(rune('a'+i)), At: now.Add(time.Duration(i-len(placed)) * time.Minute), Inputs: map[string]string{"new_lesson": p, "lesson": page}})
	}
	return recs
}

// New-B: draft B, which only restated the page's lesson, scored higher than draft A, which really added something, and got the truth. Word sequences
// already in the merge target's current lesson are excluded from the comparison.
func TestPlaceMatchPrefersTheDraftThatAddedSomething(t *testing.T) {
	now := time.Now()
	page := "変更履歴は英語と日本語を揃える。"
	restated := "英語と日本語を揃える"              // B: only restates part of the current lesson (fully contained in the merged text)
	added := "見出しは版と題の形にし、未公開の見出しのまま出さない" // A: the added content (slightly rewritten when merged)
	recs := placedRecs(now, page, added, restated)
	d := notebook.Draft{ID: "p1", Title: "変更履歴", Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にし、未公開のまま出さない。"}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != added {
		t.Fatalf("goes to the added draft A: got %q", got)
	}
}

// Direction of ties: with equal coverage, the one placed more recently gets it.
func TestPlaceMatchTieGoesToTheNewerDraft(t *testing.T) {
	now := time.Now()
	page := "古い教訓"
	older, newer := "コミットの本文に検証を書く", "コミットの件名に対象を書く"
	recs := placedRecs(now, page, older, newer) // newer is later
	d := notebook.Draft{ID: "p1", Title: "コミット", Lesson: "コミットの本文に検証を書く。コミットの件名に対象を書く。"}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != newer {
		t.Fatalf("a tie goes to the newer: got %q", got)
	}
}

// Lowercasing: do not miss only because of English upper and lower case.
func TestPlaceMatchIgnoresLetterCase(t *testing.T) {
	now := time.Now()
	page := "Old lesson"
	placed := "Read The Status Page before changing a spec"
	recs := placedRecs(now, page, placed)
	d := notebook.Draft{ID: "p1", Title: "Status", Lesson: "read the status page before changing a spec, then decide."}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != placed {
		t.Fatalf("do not miss on a case difference: got %q", got)
	}
}
