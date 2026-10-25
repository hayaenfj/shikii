package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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
