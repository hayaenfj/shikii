package main

// Tests that reproduce H1, M1, M2, M3 and L1 of audit 0.8.3 (brought in from what the auditor wrote in a copy).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func audit083Setup(t *testing.T) (string, string, store.Store, string) {
	model := fakeAgentModel(t, func(state string) float64 { return 0.3 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	src := fmt.Sprintf("%016x", 777)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	return agent, scope, st, src
}

// A: an English draft that was placed but not saved gets a truth when an unrelated page is saved
func TestPlacementIgnoresUnrelatedEnglishSaves(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	seedLessons(t, st, scope, "Keep both changelogs in step", "Commit author identity")
	draft := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", Kind: "rule", Scene: "テストの時", AppliesTo: "releasing"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}}); err != nil {
		t.Fatal(err)
	}
	body := "A model returns a probability. From the truths people and results supply, the tool measures how the model's confidence relates to how often it is right, and draws a line above which it may act on its own. Until the line exists, nothing is automatic. Every decision falls into a band: act proceeds automatically, ask goes to a person, discard does nothing, abstain means an input or an endpoint is missing. Truths live apart from the model, so changing models does not start you over. Alternative wordings can be scored on the same truths and the better one kept. The page and the status command show whether it is working: whether delivered lessons helped, how many turns did the routine, how many turns guards stopped and how many were fixed, how many decisions wait for a person, and which lines have been drawn."
	other := notebook.Draft{Sources: []string{src}, Title: "Status page", Lesson: "Read the status page first when running the tool.", Kind: "rule", Scene: "テストの時", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: other}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("saving an unrelated English page gave a truth to a draft that was not saved: %d items", len(place))
	}
}

// B: when merging by adding to the body while keeping an existing page's lesson, an old (truthless) place record with the same sentence as that lesson hits first by exact match, and the current draft gets no truth
func TestPlacementLeftoverExactRecordDoesNotShadowMerge(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	// Leftover place records from when the page was created (the candidate page no longer exists, so they stay without a truth; real data has these on 15 pages)
	if err := st.AppendRecord(store.Record{TraceID: "leftover1", SpecID: "wiki-place", SpecHash: "x", Model: "m", InputsHash: "h1", At: time.Now().Add(-48 * time.Hour),
		Inputs: map[string]string{"new_lesson": "変更履歴は英語と日本語を揃える。", "lesson": "今は無い記事の教訓"}}); err != nil {
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

// C: an old placed draft that was merged before without a truth (it sits in the body verbatim) hits first with a higher score than the current rewritten draft
func TestPlacementPrefersTheNewestDraftOfTheMergedPage(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	old := notebook.Draft{Sources: []string{src}, Title: "変更履歴の両方", Lesson: "変更履歴は版ごとに英語と日本語の両方へ同じ項目を書き、片方だけに足さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{old}}); err != nil {
		t.Fatal(err)
	}
	ageRecords(t, st, "wiki-place", old.Lesson, 48*time.Hour) // the old draft was placed the day before (recheck: of 2 placed at the same time, pick by score)
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
	body := "## 前に足したこと\n" + old.Lesson + "\n## 見出し\n見出しは「版 — 題」にし、未公開のまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは「版 — 題」にし、未公開のまま出さない。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if l := place[tr]; l.Truth != "yes" {
		t.Fatalf("the current draft got no truth and the old draft got one: %+v (count given %d)", l, len(place))
	}
}

// D: status counts machine truths (context) and re-labels of the same input and says "partway through automatic drawing", but automatic calibration counts only human truths (one per input) and does not run
func TestLineStateCountsLikeTheAutoCalibration(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := agent + "/decisions/changes-in-scope.yaml"
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN; i++ {
		truth := "no"
		if i%2 == 0 {
			truth = "yes"
		}
		// Only 5 kinds of input (the same input labeled many times). Half are machine truths (context)
		src := "outcome"
		inp := fmt.Sprintf("in%d", i%5)
		if i%3 == 0 {
			src = "context"
		}
		labels = append(labels, store.Label{TraceID: fmt.Sprintf("t%03d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: inp, Truth: truth, Weight: 1, Source: src, At: time.Now()})
	}
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRecord(store.Record{TraceID: "r1", SpecID: sp.ID, SpecHash: sp.Hash(), Model: "m1", InputsHash: "in0", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	called := false
	old := hookCalibrate
	hookCalibrate = func(agent, path, storeDir, model string) { called = true }
	defer func() { hookCalibrate = old }()
	hookMaybeCalibrate(agent, sp.ID)
	if strings.Contains(out, "the line is drawn automatically") && !called {
		t.Fatalf("status says it will draw automatically, but automatic calibration does not run (human truths per input are below min_n)")
	}
}

// E: automatic calibration was tried on m1, yet even m2's key, never tried, shows as "tried but could not separate"
func TestLineStateStuckOnlyForTheModelTried(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := agent + "/decisions/changes-in-scope.yaml"
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN+2; i++ {
		truth := []string{"yes", "no"}[i%2]
		labels = append(labels, store.Label{TraceID: fmt.Sprintf("t%03d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m2", InputsHash: fmt.Sprintf("in%d", i), Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
	}
	st.AppendLabels(labels)
	guardModel := "m1"
	guard := agent + "/calibrating-changes-in-scope"
	os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 %s %d %s\n", guardModel, sp.Eval.MinN, sp.Hash())), 0o600)
	old := time.Now().Add(-2 * hookCalibrateGap)
	os.Chtimes(guard, old, old)
	out := capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	if strings.Contains(out, "tried: the answers could not be separated") {
		t.Fatalf("a note from trying on m1 makes m2's key show as \"tried but could not separate\":\n%s", out)
	}
}

// Audit 0.8.3 H1 (merge side): editing a page that was not among the candidates at place time (a page created after placing) with an unrelated long English body gives no truth to that draft.
func TestPlacementIgnoresEditsOfPagesThatWereNotCandidates(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	seedLessons(t, st, scope, "Keep both changelogs in step", "Commit author identity")
	draft := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", Kind: "rule", Scene: "テストの時", AppliesTo: "releasing"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}}); err != nil {
		t.Fatal(err)
	}
	later := seedLessons(t, st, scope, "Status page first")["Status page first"] // a page created after placing (not among the candidates)
	body := "Read the status page first. Never push or create a release until the person has reviewed it and explicitly said so; the status page shows what is waiting."
	edit := notebook.Draft{ID: later, Sources: []string{src}, Title: "Status page first", Lesson: "Status page first.", Kind: "rule", Scene: "テストの時", AppliesTo: "Status page first を扱う時", Body: &body}
	if cov := coverageOf(draft.Lesson, edit); cov < placeMatchMin {
		t.Fatalf("this negative example cannot test the candidate condition unless coverage passes the lower bound: %.2f", cov)
	}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: edit}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("editing a page that was not a candidate gives no truth to that draft: %d items", len(place))
	}
}

// ageRecords moves the time of the placed draft's (new_lesson) records back by d (to make a draft placed long ago).
func ageRecords(t *testing.T, st store.Store, specID, newLesson string, d time.Duration) {
	t.Helper()
	path := filepath.Join(st.Dir, "records", specID+".jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var rec store.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(rec.Inputs["new_lesson"]) == strings.TrimSpace(newLesson) {
			rec.At = rec.At.Add(-d)
			nb, _ := json.Marshal(rec)
			line = string(nb)
		}
		out = append(out, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
