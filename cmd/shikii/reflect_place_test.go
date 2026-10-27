package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// Puts pages for testing insertion points and returns title → id.
func seedLessons(t *testing.T, st store.Store, scope string, titles ...string) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for i, title := range titles {
		src := fmt.Sprintf("%016x", 100+i)
		if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "依頼 " + title, Summary: "返答", Reply: "人の返事 " + title}); err != nil {
			t.Fatal(err)
		}
		res, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(notebook.Draft{Sources: []string{src}, Title: title, Lesson: title + "。", Kind: "rule", Scene: "テストの時", AppliesTo: title + "を扱う時"})
		if err != nil {
			t.Fatal(err)
		}
		ids[title] = res.Page.ID
	}
	return ids
}

// After writing a new lesson, before saving, the insertion points (same-topic pages, highest p first) and the top suspected contradictions come back.
// The result of the save (added to or replaced) is attached automatically as the truth for those decisions. People do not answer.
func TestReflectPlaceRanksPagesAndSettlesFromSubmit(t *testing.T) {
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.HasPrefix(state, "existing lesson:") { // wiki-conflict
			if strings.Contains(state, "作らない") {
				return 0.85
			}
			return 0.2
		}
		switch { // wiki-place
		case strings.Contains(state, "existing page title: 変更履歴"):
			return 0.9
		case strings.Contains(state, "existing page title: コミット"):
			return 0.4
		}
		return 0.1
	})
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す", "ライセンスはApache-2.0またはMIT", "プロジェクトを明示的に有効化する")
	src := fmt.Sprintf("%016x", 200)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "変更履歴の日本語版は要る?", Summary: "返答", Reply: "日本語版は要りません"}); err != nil {
		t.Fatal(err)
	}
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の日本語版", Lesson: "変更履歴は英語だけで保ち、日本語版は作らない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を更新する時"}

	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{{Sources: []string{src}, Title: "x"}}}); err == nil {
		t.Fatal("a draft without a lesson cannot ask for an insertion point")
	}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}})
	if err != nil {
		t.Fatal(err)
	}
	res := out.(placeResult)
	if len(res.Placements) != 1 || len(res.Placements[0].Candidates) != 4 {
		t.Fatalf("compares against every live page: %+v", res)
	}
	c := res.Placements[0].Candidates
	if c[0].PageID != ids["変更履歴は英語と日本語を揃える"] || c[0].SameTopic.P != 0.9 || c[1].PageID != ids["コミットの説明を丁寧に残す"] || c[1].SameTopic.P != 0.4 || c[0].SameTopic.Band != "ask" {
		t.Fatalf("orders by same-topic p, highest first: %+v", c)
	}
	if c[0].Conflict == nil || c[0].Conflict.P != 0.85 || c[2].Conflict == nil || c[3].Conflict != nil {
		t.Fatalf("contradiction is asked only for the top %d: %+v", placeConflictTop, c)
	}
	if c[0].SameTopic.Calibrated || c[0].SameTopic.Band == "act" {
		t.Fatalf("no act while there is no line: %+v", c[0])
	}
	if recs, _ := st.Records("wiki-place", 0); len(recs) != 4 {
		t.Fatalf("the decisions are recorded: %d", len(recs))
	}

	// Added to the same page without replacing → that page is same topic "yes" (weight 1) and disagreement "no" (partial); other pages are same topic "no" (partial)
	draft.ID = c[0].PageID
	sub, err := runReflect(agent, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{draft}})
	if err != nil || !sub.(reflectResult).Complete {
		t.Fatalf("save: %v %+v", err, sub)
	}
	place, _ := st.LabeledTraces("wiki-place")
	conflict, _ := st.LabeledTraces("wiki-conflict")
	if l := place[c[0].SameTopic.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight || l.Source != "outcome" {
		t.Fatalf("the page added to is the same topic: %+v", l)
	}
	if l := place[c[1].SameTopic.TraceID]; l.Truth != "no" || l.Weight != placeOtherWeight {
		t.Fatalf("a page not chosen is another topic (partial): %+v", l)
	}
	if l := conflict[c[0].Conflict.TraceID]; l.Truth != "no" || l.Weight != placeOtherWeight {
		t.Fatalf("added without replacing means no disagreement (partial): %+v", l)
	}
	if _, ok := conflict[c[1].Conflict.TraceID]; ok {
		t.Fatal("for a page neither added to nor replaced, whether it disagrees is unknown")
	}

	// Replaced → the replaced page is same topic "yes" and disagreement "yes" (weight 1)
	draft2 := notebook.Draft{Sources: []string{src}, Title: "変更履歴は英語だけ", Lesson: "変更履歴は日本語版を作らず英語だけにする", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を更新する時"}
	out, err = runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft2}})
	if err != nil {
		t.Fatal(err)
	}
	top := out.(placeResult).Placements[0].Candidates[0]
	draft2.Replaces = []string{top.PageID}
	if sub, err := runReflect(agent, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{draft2}}); err != nil || !sub.(reflectResult).Complete {
		t.Fatalf("saving the replacement: %v %+v", err, sub)
	}
	place, _ = st.LabeledTraces("wiki-place")
	conflict, _ = st.LabeledTraces("wiki-conflict")
	if l := place[top.SameTopic.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight {
		t.Fatalf("the replaced page is the same topic: %+v", l)
	}
	if l := conflict[top.Conflict.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight {
		t.Fatalf("replaced means they disagree: %+v", l)
	}
	sp, err := spec.Load(filepath.Join(agent, "decisions", "wiki-place.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := humanTruthsFor(st, sp); n == 0 {
		t.Fatal("truths from results count as truths for drawing the line")
	}
}

// In a project with no pages, it only returns that this becomes a new page (the model is not asked).
func TestReflectPlaceWithoutPagesNeedsNoModel(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo) // no model
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	if err := st.AppendFeedback(scope, store.Feedback{ID: fmt.Sprintf("%016x", 300), Request: "依頼", Summary: "返答", Reply: "返事"}); err != nil {
		t.Fatal(err)
	}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{{Sources: []string{fmt.Sprintf("%016x", 300)}, Title: "t", Lesson: "l", AppliesTo: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p := out.(placeResult).Placements; len(p) != 1 || len(p[0].Candidates) != 0 || !strings.Contains(p[0].Note, "new page") {
		t.Fatalf("%+v", p)
	}
	// The same from the CLI (shikii reflect place, JSON on standard input)
	old := stdin
	defer func() { stdin = old }()
	stdin = strings.NewReader(fmt.Sprintf(`{"wiki":[{"sources":["%016x"],"title":"t","lesson":"l","kind":"rule","scene":"テストの時","applies_to":"a"}]}`, 300))
	cli := capture(t, func() error { return cmdReflect([]string{"place"}) })
	if !strings.Contains(cli, `"placements"`) || !strings.Contains(cli, "new page") {
		t.Fatalf("shikii reflect place must answer like the MCP operation: %s", cli)
	}
}

// 0.8.3: merging into an existing page often rewrites the lesson text, and it is often saved directly with wiki ingest.
// Even when the text differs from the draft at placing time (as long as the draft is in the body), it is matched as the same draft, and wiki ingest also attaches the truth from the result.
// Without this, truths were attached only when "a new page was made", every insertion-point truth became "no", and no line could be drawn (all 384 were no in this project).
// Saving an unrelated page does not attach truths to that draft's decisions.
func TestPlacementOutcomeFollowsRewrittenMergesThroughIngest(t *testing.T) {
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "existing page title: 変更履歴") {
			return 0.9
		}
		return 0.1
	})
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す", "ライセンスはApache-2.0またはMIT")
	src := fmt.Sprintf("%016x", 300)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "変更履歴の版の見出しは?", Summary: "返答", Reply: "見出しは版と題にして"}); err != nil {
		t.Fatal(err)
	}
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(placeResult).Placements[0].Candidates
	if c[0].PageID != ids["変更履歴は英語と日本語を揃える"] {
		t.Fatalf("the closest page comes first: %+v", c)
	}
	// Saving an unrelated page with wiki ingest does not attach truths to the earlier draft's decisions
	other := notebook.Draft{Sources: []string{src}, Title: "ライセンスの表記", Lesson: "配布物には 3 つのライセンスファイルと第三者の表示を入れる", Kind: "rule", Scene: "テストの時", AppliesTo: "配布物を組む時"}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: other}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("an unrelated save attaches no truth: %+v", place)
	}
	// A save that edits the same page with text loosely related to the placed draft (coverage 30-60%) does not attach a truth to that draft (audit 0.8.3: it passed even with a lower bound of 0.3)
	weakBody := "## 見出しの日付\n変更履歴の見出しには日付を書かない。版の形は version.go と揃える。"
	weak := notebook.Draft{ID: c[0].PageID, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃える", Lesson: "変更履歴は英語と日本語を揃える。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &weakBody}
	if cov := coverageOf(draft.Lesson, weak); cov < 0.3 || cov >= placeMatchMin {
		t.Fatalf("this negative example must have coverage between 0.3 and 0.6 to test the lower bound: %.2f", cov)
	}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: weak}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("a loosely related edit attaches no truth: %+v", place)
	}
	// Merge into the closest page with wiki ingest, rewriting the lesson text (the body also rephrases the draft text; coverage 0.6-0.95)
	body := "## 見出し\n見出しを「版 — 題」にする。公開前の見出しのまま出さない。"
	merged := notebook.Draft{ID: c[0].PageID, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする",
		Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にする。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時",
		Body: &body}
	if cov := coverageOf(draft.Lesson, merged); cov < placeMatchMin || cov >= 0.95 {
		t.Fatalf("a rewritten merge must have coverage between 0.6 and 0.95 to test the lower bound: %.2f", cov)
	}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if l := place[c[0].SameTopic.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight || l.Source != "outcome" {
		t.Fatalf("a page merged into with rewriting is same topic \"yes\": %+v", l)
	}
	if l := place[c[1].SameTopic.TraceID]; l.Truth != "no" || l.Weight != placeOtherWeight {
		t.Fatalf("a page not chosen is \"no\" (partial): %+v", l)
	}
}

// coverageOf is the share of 2-character sequences of the placed draft text that are in the draft being saved (checks that the test samples can test the lower bound).
func coverageOf(placed string, d notebook.Draft) float64 {
	text := d.Title + "\n" + d.Lesson
	if d.Body != nil {
		text += "\n" + *d.Body
	}
	have, want := bigrams(text), bigrams(placed)
	hit := 0
	for b := range want {
		if have[b] {
			hit++
		}
	}
	return float64(hit) / float64(len(want))
}

// 0.9.0: the insertion-point reply returns the existing scenes, most used first (so a new draft's scene uses the same wording when one is close).
// It is returned also when the draft becomes a new page (no pages). Candidate pages also carry their kind and scene (the values carried over when merging).
func TestPlaceReturnsExistingScenes(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .3 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	src := fmt.Sprintf("%016x", 400)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: agent, Scope: scope}
	for i, sc := range []string{"コミットする時", "コミットする時", "公開する時"} {
		if _, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("記事 %d", i), Lesson: fmt.Sprintf("教訓 %d。", i), AppliesTo: "a", Kind: "rule", Scene: sc}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{{Sources: []string{src}, Title: "新しい", Lesson: "新しい教訓", AppliesTo: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	res := out.(placeResult)
	if len(res.Scenes) != 2 || res.Scenes[0].Scene != "コミットする時" || res.Scenes[0].Count != 2 || res.Scenes[1].Scene != "公開する時" {
		t.Fatalf("returns the existing scenes, most used first: %+v", res.Scenes)
	}
	if c := res.Placements[0].Candidates[0]; c.Kind != "rule" || c.Scene == "" {
		t.Fatalf("candidates also carry the kind and scene: %+v", c)
	}
}

// audit 0.9.0 M13: the scenes in the insertion-point reply are the most used, up to placeScenesMax (the reply is not inflated with every scene).
func TestLiveScenesAreCappedAndOrdered(t *testing.T) {
	var live []notebook.Page
	for i := 0; i < placeScenesMax+5; i++ {
		for j := 0; j <= i%3; j++ {
			live = append(live, notebook.Page{ID: fmt.Sprintf("%016x", i*10+j), Scene: fmt.Sprintf("場面 %02d の時", i)})
		}
	}
	got := liveScenes(live)
	if len(got) != placeScenesMax || got[0].Count != 3 || got[len(got)-1].Count > got[0].Count {
		t.Fatalf("most used first, up to %d: got %d, first %+v", placeScenesMax, len(got), got[0])
	}
}

// Tests that reproduce the 0.8.3 recheck (new-1, remaining-1, remaining-2) (brought in from what the auditor wrote in a copy).

// Recheck new-1: place 2 items in one place (A: to be merged, B: another draft), rewrite A into X and merge it with a long-ish body.
// The truth goes to the merged A, in Japanese and in English; in English B is an unrelated topic and gets none.
func TestPlacementTwoDraftsInOnePlaceLabelsTheMergedOne(t *testing.T) {
	type draft struct{ title, lesson, appliesTo string }
	for _, tc := range []struct {
		name               string
		page, other        string
		a, b               draft
		merged             draft
		body               string
		unrelatedBGetsNone bool
	}{
		{
			name: "Japanese", page: "変更履歴は英語と日本語を揃える", other: "コミットの説明を丁寧に残す",
			a:      draft{"変更履歴の見出し", "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", "変更履歴を書く時"},
			b:      draft{"変更履歴の項目", "変更履歴は英語と日本語の両方に、版ごとに同じ項目を同じ順で書く", "変更履歴を書く時"},
			merged: draft{"変更履歴は英語と日本語を揃え、見出しは版と題にする", "変更履歴は英語と日本語を揃える。見出しは「版 — 題」にし、未公開のまま出さない。", "変更履歴を書く時"},
			body:   "## 揃え方\n変更履歴は英語と日本語の両方に、版ごとに同じ項目を書く。片方だけに足さない。順も同じにする。\n## 見出し\n見出しは「版 — 題」の形にし、未公開の見出しのまま出さない。",
		},
		{
			name: "English", page: "Status page first", other: "Commit author identity",
			a:                  draft{"Read status", "Read the status page before changing any decision spec.", "operating"},
			b:                  draft{"Release needs consent", "Never push or create a release until the person has reviewed it and explicitly said so.", "releasing"},
			merged:             draft{"Status page first", "Read the status page first, and before changing any decision spec.", "operating"},
			body:               "The status page shows whether the tool is working: which lines have been drawn, how many decisions wait for a person, whether delivered lessons helped, and how many turns the guards stopped. Read it first when something looks wrong, and read it before changing any decision spec, since a change of wording starts the truths over for that spec. It also says when the answers could not be separated with the current wording and model, and when there is only one answer so far.",
			unrelatedBGetsNone: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, scope, st, src := audit083Setup(t)
			pid := seedLessons(t, st, scope, tc.page, tc.other)[tc.page]
			mk := func(d draft) notebook.Draft {
				return notebook.Draft{Sources: []string{src}, Title: d.title, Lesson: d.lesson, Kind: "rule", Scene: "テストの時", AppliesTo: d.appliesTo}
			}
			out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{mk(tc.a), mk(tc.b)}})
			if err != nil {
				t.Fatal(err)
			}
			trA, trB := placedTrace(t, out, 0, pid), placedTrace(t, out, 1, pid)
			merged := mk(tc.merged)
			merged.ID, merged.Body = pid, &tc.body
			if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
				t.Fatal(err)
			}
			place, _ := st.LabeledTraces("wiki-place")
			if place[trA].Truth != "yes" || tc.unrelatedBGetsNone && place[trB].Truth != "" {
				t.Errorf("it goes to the merged A (and not to an unrelated B) (A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
			}
		})
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

// B and recheck remaining-2: when merging by adding to the body while keeping an existing page's lesson, an old (truthless) place record with the same
// sentence as that lesson must not hit first by exact match and leave the current draft without a truth, whether the old record's candidate page is gone
// (it can no longer get a truth) or still exists (it could still get one).
func TestPlacementLeftoverExactRecordDoesNotShadowMerge(t *testing.T) {
	for _, tc := range []struct {
		name, candidate string // the leftover record's candidate page lesson
	}{
		{"candidate page gone", "今は無い記事の教訓"},
		{"candidate page still there", "コミットの説明を丁寧に残す。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent, scope, st, src := audit083Setup(t)
			ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
			pid := ids["変更履歴は英語と日本語を揃える"]
			// Leftover place records from when the page was created (they stay without a truth; real data has these on 15 pages)
			if err := st.AppendRecord(store.Record{TraceID: "leftover1", SpecID: "wiki-place", SpecHash: "x", Model: "m", InputsHash: "h1", At: time.Now().Add(-48 * time.Hour),
				Inputs: map[string]string{"new_lesson": "変更履歴は英語と日本語を揃える。", "lesson": tc.candidate}}); err != nil {
				t.Fatal(err)
			}
			draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時"}
			out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}})
			if err != nil {
				t.Fatal(err)
			}
			tr := placedTrace(t, out, 0, pid)
			body := "## 見出し\n" + draft.Lesson
			merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃える", Lesson: "変更履歴は英語と日本語を揃える。", Kind: "rule", Scene: "テストの時", AppliesTo: "変更履歴を書く時", Body: &body}
			if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
				t.Fatal(err)
			}
			place, _ := st.LabeledTraces("wiki-place")
			if l := place[tr]; l.Truth != "yes" {
				t.Fatalf("a merge that added to the body without changing the lesson does not get a truth: %+v (count given %d)", l, len(place))
			}
		})
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

// C1: when the placed draft restates the merge target's current lesson (a person said the same rule again), merging stopped giving a truth (0.8.3 gave one).
func TestAudit090PlaceMatchRestatedDraftStillLabeled(t *testing.T) {
	now := time.Now()
	page := "変更履歴は英語と日本語を揃える。"
	restated := "英語と日本語を揃える"
	recs := placedRecs(now, page, restated)
	body := "人がもう一度言った(2026-10-20)。"
	d := notebook.Draft{ID: "p1", Title: "変更履歴", Lesson: "変更履歴は英語と日本語を必ず揃える。", Body: &body}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != restated {
		t.Fatalf("merging a restating draft gives no truth: got %q", got)
	}
}

// Items from the second recheck of 0.8.3 deferred to 0.9 (new-B and test holes: direction of ties, lowercasing), checked through matching (matchPlacedDraft) alone.

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
