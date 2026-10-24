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

// 差し込み先の試験用の記事を置き、題 → id を返す。
func seedLessons(t *testing.T, st store.Store, scope string, titles ...string) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for i, title := range titles {
		src := fmt.Sprintf("%016x", 100+i)
		if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "依頼 " + title, Summary: "返答", Reply: "人の返事 " + title}); err != nil {
			t.Fatal(err)
		}
		res, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(notebook.Draft{Sources: []string{src}, Title: title, Lesson: title + "。", AppliesTo: title + "を扱う時"})
		if err != nil {
			t.Fatal(err)
		}
		ids[title] = res.Page.ID
	}
	return ids
}

// 新しい教訓を書いたら、保存の前に差し込み先(同じ話題の記事を p の高い順)と、上位の矛盾の疑いが返る。
// 保存の結果(足した・置き換えた)が、その判定の正解として自動で付く。人は答えない。
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
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の日本語版", Lesson: "変更履歴は英語だけで保ち、日本語版は作らない", AppliesTo: "変更履歴を更新する時"}

	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{{Sources: []string{src}, Title: "x"}}}); err == nil {
		t.Fatal("lesson の無い草稿には差し込み先を聞けない")
	}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}})
	if err != nil {
		t.Fatal(err)
	}
	res := out.(placeResult)
	if len(res.Placements) != 1 || len(res.Placements[0].Candidates) != 4 {
		t.Fatalf("生きている記事を全部突き合わせる: %+v", res)
	}
	c := res.Placements[0].Candidates
	if c[0].PageID != ids["変更履歴は英語と日本語を揃える"] || c[0].SameTopic.P != 0.9 || c[1].PageID != ids["コミットの説明を丁寧に残す"] || c[1].SameTopic.P != 0.4 || c[0].SameTopic.Band != "ask" {
		t.Fatalf("同じ話題の p が高い順に並べる: %+v", c)
	}
	if c[0].Conflict == nil || c[0].Conflict.P != 0.85 || c[2].Conflict == nil || c[3].Conflict != nil {
		t.Fatalf("矛盾は上位 %d 件にだけ聞く: %+v", placeConflictTop, c)
	}
	if c[0].SameTopic.Calibrated || c[0].SameTopic.Band == "act" {
		t.Fatalf("線が無い間は act を出さない: %+v", c[0])
	}
	if recs, _ := st.Records("wiki-place", 0); len(recs) != 4 {
		t.Fatalf("判定を記録する: %d", len(recs))
	}

	// 置き換えずに同じ記事へ足した → その記事は同じ話題「はい」(重み 1)・食い違い「いいえ」(部分)、他の記事は同じ話題「いいえ」(部分)
	draft.ID = c[0].PageID
	sub, err := runReflect(agent, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{draft}})
	if err != nil || !sub.(reflectResult).Complete {
		t.Fatalf("保存: %v %+v", err, sub)
	}
	place, _ := st.LabeledTraces("wiki-place")
	conflict, _ := st.LabeledTraces("wiki-conflict")
	if l := place[c[0].SameTopic.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight || l.Source != "outcome" {
		t.Fatalf("足した記事は同じ話題: %+v", l)
	}
	if l := place[c[1].SameTopic.TraceID]; l.Truth != "no" || l.Weight != placeOtherWeight {
		t.Fatalf("選ばなかった記事は別の話題(部分): %+v", l)
	}
	if l := conflict[c[0].Conflict.TraceID]; l.Truth != "no" || l.Weight != placeOtherWeight {
		t.Fatalf("置き換えずに足したなら食い違わない(部分): %+v", l)
	}
	if _, ok := conflict[c[1].Conflict.TraceID]; ok {
		t.Fatal("足しも置き換えもしなかった記事と食い違うかは分からない")
	}

	// 置き換えた → 置き換えた記事は同じ話題「はい」・食い違い「はい」(重み 1)
	draft2 := notebook.Draft{Sources: []string{src}, Title: "変更履歴は英語だけ", Lesson: "変更履歴は日本語版を作らず英語だけにする", AppliesTo: "変更履歴を更新する時"}
	out, err = runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft2}})
	if err != nil {
		t.Fatal(err)
	}
	top := out.(placeResult).Placements[0].Candidates[0]
	draft2.Replaces = []string{top.PageID}
	if sub, err := runReflect(agent, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{draft2}}); err != nil || !sub.(reflectResult).Complete {
		t.Fatalf("置き換えの保存: %v %+v", err, sub)
	}
	place, _ = st.LabeledTraces("wiki-place")
	conflict, _ = st.LabeledTraces("wiki-conflict")
	if l := place[top.SameTopic.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight {
		t.Fatalf("置き換えた記事は同じ話題: %+v", l)
	}
	if l := conflict[top.Conflict.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight {
		t.Fatalf("置き換えたなら食い違う: %+v", l)
	}
	sp, err := spec.Load(filepath.Join(agent, "decisions", "wiki-place.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := humanTruthsFor(st, sp); n == 0 {
		t.Fatal("結果の正解は線を引く正解に数える")
	}
}

// 記事が無いプロジェクトでは、新しい記事になることだけを返す(モデルには聞かない)。
func TestReflectPlaceWithoutPagesNeedsNoModel(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo) // モデル無し
	scope := feedbackScope(repo)
	st := store.Store{Dir: agent}
	if err := st.AppendFeedback(scope, store.Feedback{ID: fmt.Sprintf("%016x", 300), Request: "依頼", Summary: "返答", Reply: "返事"}); err != nil {
		t.Fatal(err)
	}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{{Sources: []string{fmt.Sprintf("%016x", 300)}, Title: "t", Lesson: "l", AppliesTo: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p := out.(placeResult).Placements; len(p) != 1 || len(p[0].Candidates) != 0 || !strings.Contains(p[0].Note, "新しい記事") {
		t.Fatalf("%+v", p)
	}
	// CLI からも同じ(shikii reflect place、JSON を標準入力)
	old := stdin
	defer func() { stdin = old }()
	stdin = strings.NewReader(fmt.Sprintf(`{"wiki":[{"sources":["%016x"],"title":"t","lesson":"l","applies_to":"a"}]}`, 300))
	cli := capture(t, func() error { return cmdReflect([]string{"place"}) })
	if !strings.Contains(cli, `"placements"`) || !strings.Contains(cli, "新しい記事") {
		t.Fatalf("shikii reflect place must answer like the MCP operation: %s", cli)
	}
}

// 0.8.3: 既存の記事への合流は教訓の文を書き直すことが多く、wiki ingest で直接保存されることも多い。
// 置いた時の草稿と文が変わっていても(本文に草稿が入っていれば)同じ草稿として突き合わせ、wiki ingest でも結果の正解を付ける。
// これが無いと「新しい記事にした」時だけ正解が付き、差し込み先の正解が全部「いいえ」になって線が引けなかった(このプロジェクトで 384 件全部 no)。
// 関係ない記事の保存は、その草稿の判定に正解を付けない。
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
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}})
	if err != nil {
		t.Fatal(err)
	}
	c := out.(placeResult).Placements[0].Candidates
	if c[0].PageID != ids["変更履歴は英語と日本語を揃える"] {
		t.Fatalf("一番近い記事が先頭: %+v", c)
	}
	// 関係ない記事を wiki ingest で保存しても、さっきの草稿の判定には正解を付けない
	other := notebook.Draft{Sources: []string{src}, Title: "ライセンスの表記", Lesson: "配布物には 3 つのライセンスファイルと第三者の表示を入れる", AppliesTo: "配布物を組む時"}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: other}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("関係ない保存で正解を付けない: %+v", place)
	}
	// 一番近い記事へ、教訓の文を書き直して wiki ingest で合流する(本文に草稿の文が入る)
	body := "## 見出し\n変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない(2026-10-24)。"
	merged := notebook.Draft{ID: c[0].PageID, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする",
		Lesson: "変更履歴は英語と日本語を揃える。見出しは「版 — 題」にし、未公開のまま出さない。", AppliesTo: "変更履歴を書く時",
		Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if l := place[c[0].SameTopic.TraceID]; l.Truth != "yes" || l.Weight != placeChosenWeight || l.Source != "outcome" {
		t.Fatalf("書き直して合流した記事は同じ話題「はい」: %+v", l)
	}
	if l := place[c[1].SameTopic.TraceID]; l.Truth != "no" || l.Weight != placeOtherWeight {
		t.Fatalf("選ばなかった記事は「いいえ」(部分): %+v", l)
	}
}
