package main

// Tests that reproduce the 0.8.3 recheck (new-1, remaining-1, remaining-2) (brought in from what the auditor wrote in a copy).

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Recheck new-1 (Japanese): place 2 items in the same place (A, then B), rewrite A into X and merge. X's body is long-ish (the body so far + A).
func TestPlacementTwoDraftsInOnePlaceLabelsTheMergedOne(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	b := notebook.Draft{Sources: []string{src}, Title: "変更履歴の項目", Lesson: "変更履歴は英語と日本語の両方に、版ごとに同じ項目を同じ順で書く", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	trA, trB := placedTrace(t, out, 0, pid), placedTrace(t, out, 1, pid)
	body := "## 揃え方\n変更履歴は英語と日本語の両方に、版ごとに同じ項目を書く。片方だけに足さない。順も同じにする。\n## 見出し\n見出しは「版 — 題」の形にし、未公開の見出しのまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは「版 — 題」にし、未公開のまま出さない。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[trA].Truth != "yes" {
		t.Errorf("the merged A does not get \"yes\" (A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
	}
}

// Recheck new-1 (English, placed in reverse order): English. Place B, then an unrelated A. Merge B into X with a long English body.
func TestPlacementEnglishLaterUnrelatedDraftDoesNotWin(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "Status page first", "Commit author identity")
	pid := ids["Status page first"]
	b := notebook.Draft{Sources: []string{src}, Title: "Read status", Lesson: "Read the status page before changing any decision spec.", Kind: "rule", Scene: "テストの時", AppliesTo: "operating"}
	outB, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{b}})
	if err != nil {
		t.Fatal(err)
	}
	a := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", Kind: "rule", Scene: "テストの時", AppliesTo: "releasing"}
	outA, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}})
	if err != nil {
		t.Fatal(err)
	}
	trB, trA := placedTrace(t, outB, 0, pid), placedTrace(t, outA, 0, pid)
	body := "A model returns a probability. From the truths people and results supply, the tool measures how the model's confidence relates to how often it is right, and draws a line above which it may act on its own. Until the line exists, nothing is automatic. Every decision falls into a band: act proceeds automatically, ask goes to a person, discard does nothing, abstain means an input or an endpoint is missing. Truths live apart from the model, so changing models does not start you over. Alternative wordings can be scored on the same truths and the better one kept. The page and the status command show whether it is working: whether delivered lessons helped, how many turns did the routine, how many turns guards stopped and how many were fixed, how many decisions wait for a person, and which lines have been drawn. Read the status page before changing any decision spec."
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "Status page first", Lesson: "Status page first; read it before changing a decision spec.", Kind: "rule", Scene: "テストの時", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[trB].Truth != "yes" || place[trA].Truth != "" {
		t.Errorf("it went to the unrelated A placed later instead of the merged B (A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
	}
}

// No problem in the recheck (replacing): a new page that rewrote the lesson, via replaces.
func TestPlacementReplacementWithRewrittenLesson(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}})
	if err != nil {
		t.Fatal(err)
	}
	tr := placedTrace(t, out, 0, pid)
	nd := notebook.Draft{Sources: []string{src}, Replaces: []string{pid}, Title: "変更履歴の見出しと言語", Lesson: "変更履歴は英語と日本語を揃え、見出しは「版 — 題」にして未公開のまま出さない。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: nd}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[tr].Truth != "yes" {
		t.Errorf("replacing does not give \"yes\"")
	}
}

// Recheck remaining-1: English. After placing an unrelated A (X is a candidate), save X with a long English body through an edit that was not placed (an edit through wiki ingest).
func TestPlacementEnglishEditOfACandidatePage(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "Status page first", "Commit author identity")
	pid := ids["Status page first"]
	a := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", Kind: "rule", Scene: "テストの時", AppliesTo: "releasing"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}}); err != nil {
		t.Fatal(err)
	}
	body := "A model returns a probability. From the truths people and results supply, the tool measures how the model's confidence relates to how often it is right, and draws a line above which it may act on its own. Until the line exists, nothing is automatic. Every decision falls into a band: act proceeds automatically, ask goes to a person, discard does nothing, abstain means an input or an endpoint is missing. Truths live apart from the model, so changing models does not start you over. Alternative wordings can be scored on the same truths and the better one kept. The page and the status command show whether it is working: whether delivered lessons helped, how many turns did the routine, how many turns guards stopped and how many were fixed, how many decisions wait for a person, and which lines have been drawn."
	edit := notebook.Draft{ID: pid, Sources: []string{src}, Title: "Status page first", Lesson: "Read the status page first when running the tool.", Kind: "rule", Scene: "テストの時", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: edit}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if len(place) != 0 {
		t.Errorf("an unrelated English edit gave a truth to a draft that was not saved: %d items", len(place))
	}
}

// No problem in the recheck (the same draft twice): place the same draft twice, rewrite and merge.
func TestPlacementSameDraftPlacedTwice(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	for i := 0; i < 2; i++ {
		if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}}); err != nil {
			t.Fatal(err)
		}
	}
	body := "## 見出し\n見出しを「版 — 題」にする。公開前の見出しのまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にする。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	yes := 0
	for _, l := range place {
		if l.Truth == "yes" {
			yes++
		}
	}
	if yes == 0 {
		t.Errorf("merging a draft placed twice does not give \"yes\"")
	}
}

// Recheck remaining-2: even if an old exact-match record can still be given a truth, a merge that keeps the lesson unchanged is hit.
func TestPlacementLabelableLeftoverExactDoesNotShadowMerge(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	// Leftover place records from when the page was created (the candidate page no longer exists, so they stay without a truth; real data has these on 15 pages)
	if err := st.AppendRecord(store.Record{TraceID: "leftover1", SpecID: "wiki-place", SpecHash: "x", Model: "m", InputsHash: "h1", At: time.Now().Add(-48 * time.Hour),
		Inputs: map[string]string{"new_lesson": "変更履歴は英語と日本語を揃える。", "lesson": "コミットの説明を丁寧に残す。"}}); err != nil {
		t.Fatal(err)
	}
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}})
	if err != nil {
		t.Fatal(err)
	}
	var tr string
	for _, c := range out.(placeResult).Placements[0].Candidates {
		if c.PageID == pid {
			tr = c.SameTopic.TraceID
		}
	}
	body := "## 見出し\n" + draft.Lesson
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃える", Lesson: "変更履歴は英語と日本語を揃える。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if l := place[tr]; l.Truth != "yes" {
		t.Fatalf("a merge that added to the body without changing the lesson does not get a truth: %+v (count given %d)", l, len(place))
	}
}

// Recheck new-1 (English, 2 items in one place): English. Place 2 items in one place (A: to be merged, B: an unrelated topic), and merge A into X with a long-ish English body.
func TestPlacementEnglishTwoDraftsInOnePlace(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "Status page first", "Commit author identity")
	pid := ids["Status page first"]
	a := notebook.Draft{Sources: []string{src}, Title: "Read status", Lesson: "Read the status page before changing any decision spec.", Kind: "rule", Scene: "テストの時", AppliesTo: "operating"}
	b := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", Kind: "rule", Scene: "テストの時", AppliesTo: "releasing"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	trA, trB := placedTrace(t, out, 0, pid), placedTrace(t, out, 1, pid)
	body := "The status page shows whether the tool is working: which lines have been drawn, how many decisions wait for a person, whether delivered lessons helped, and how many turns the guards stopped. Read it first when something looks wrong, and read it before changing any decision spec, since a change of wording starts the truths over for that spec. It also says when the answers could not be separated with the current wording and model, and when there is only one answer so far."
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "Status page first", Lesson: "Read the status page first, and before changing any decision spec.", Kind: "rule", Scene: "テストの時", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[trA].Truth != "yes" || place[trB].Truth != "" {
		t.Errorf("it goes to the merged A and not to the unrelated B (A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
	}
}

// placedTrace is the trace of the insertion-place decision about page pid for the draftIdx-th draft in the place result.
func placedTrace(t *testing.T, out any, draftIdx int, pid string) string {
	t.Helper()
	for _, pl := range out.(placeResult).Placements {
		if pl.Draft != draftIdx {
			continue
		}
		for _, c := range pl.Candidates {
			if c.PageID == pid {
				return c.SameTopic.TraceID
			}
		}
	}
	t.Fatalf("%s is not among the candidates", pid)
	return ""
}

// Recheck low: a draft placed more than 24 hours ago (placeMatchWindow) gets no truth, even by a rewritten merge.
func TestPlacementWindowSkipsDraftsPlacedLongAgo(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}}); err != nil {
		t.Fatal(err)
	}
	ageRecords(t, st, "wiki-place", a.Lesson, placeMatchWindow+time.Hour)
	body := "## 見出し\n見出しを「版 — 題」にする。公開前の見出しのまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にする。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &body}
	if cov := coverageOf(a.Lesson, merged); cov < placeMatchMin {
		t.Fatalf("the limit cannot be tested unless everything but the time would match: %.2f", cov)
	}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("do not give a truth by fuzzy matching to a draft placed the day before: %d items", len(place))
	}
}
