package main

// 監査 0.8.3 の H1・M1・M2・M3・L1 を再現するテスト(監査者が写しで書いた物を取り込んだ)。

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

// A: 英語の、置いたが保存しなかった草稿に、関係ない記事の保存で正解が付く
func TestPlacementIgnoresUnrelatedEnglishSaves(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	seedLessons(t, st, scope, "Keep both changelogs in step", "Commit author identity")
	draft := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", AppliesTo: "releasing"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}}); err != nil {
		t.Fatal(err)
	}
	body := "A model returns a probability. From the truths people and results supply, the tool measures how the model's confidence relates to how often it is right, and draws a line above which it may act on its own. Until the line exists, nothing is automatic. Every decision falls into a band: act proceeds automatically, ask goes to a person, discard does nothing, abstain means an input or an endpoint is missing. Truths live apart from the model, so changing models does not start you over. Alternative wordings can be scored on the same truths and the better one kept. The page and the status command show whether it is working: whether delivered lessons helped, how many turns did the routine, how many turns guards stopped and how many were fixed, how many decisions wait for a person, and which lines have been drawn."
	other := notebook.Draft{Sources: []string{src}, Title: "Status page", Lesson: "Read the status page first when running the tool.", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: other}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("関係ない英語の記事の保存で、保存していない草稿に正解が付いた: %d 件", len(place))
	}
}

// B: 既存の記事の教訓のまま本文に足して合流すると、その教訓と同じ文の古い(正解の付かない)置いた記録が完全一致で先に当たり、今の草稿に正解が付かない
func TestPlacementLeftoverExactRecordDoesNotShadowMerge(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	// 記事を作った時の置いた記録の残り(候補の記事が今は無いので、正解が付かずに残っている物。実データでは 15 記事にある)
	if err := st.AppendRecord(store.Record{TraceID: "leftover1", SpecID: "wiki-place", SpecHash: "x", Model: "m", InputsHash: "h1", At: time.Now().Add(-48 * time.Hour),
		Inputs: map[string]string{"new_lesson": "変更履歴は英語と日本語を揃える。", "lesson": "今は無い記事の教訓"}}); err != nil {
		t.Fatal(err)
	}
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
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
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃える", Lesson: "変更履歴は英語と日本語を揃える。", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if l := place[tr]; l.Truth != "yes" {
		t.Fatalf("教訓を変えずに本文へ足した合流に正解が付かない: %+v (付いた数 %d)", l, len(place))
	}
}

// C: 前に合流して正解の付かなかった古い置いた草稿(本文に原文のまま入っている)が、今の書き直した草稿より高い点で先に当たる
func TestPlacementPrefersTheNewestDraftOfTheMergedPage(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	old := notebook.Draft{Sources: []string{src}, Title: "変更履歴の両方", Lesson: "変更履歴は版ごとに英語と日本語の両方へ同じ項目を書き、片方だけに足さない", AppliesTo: "変更履歴を書く時"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{old}}); err != nil {
		t.Fatal(err)
	}
	ageRecords(t, st, "wiki-place", old.Lesson, 48*time.Hour) // 古い草稿は前の日に置いた(再監査: 同じ時に置いた 2 件は点数で選ぶ)
	draft := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
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
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは「版 — 題」にし、未公開のまま出さない。", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if l := place[tr]; l.Truth != "yes" {
		t.Fatalf("今の草稿に正解が付かず、古い草稿に付いた: %+v (付いた数 %d)", l, len(place))
	}
}

// D: 機械の正解(context)や同じ入力の付け直しも数えて「自動で線を引く途中」と言うが、自動の較正は人の正解(入力ごとに 1 件)しか数えず動かない
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
		// 入力は 5 種類だけ(同じ入力に何度も付いた)。半分は機械の正解(context)
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
		t.Fatalf("status は自動で引くと言うが、自動の較正は動かない(人の正解・入力ごとの数が min_n 未満)")
	}
}

// E: 自動の較正を試したのは m1 なのに、試していない m2 の鍵まで「試したが分けられない」と出る
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
		t.Fatalf("m1 で試した控えで、m2 の鍵が「試したが分けられない」と出る:\n%s", out)
	}
}

// 監査 0.8.3 の H1(合流の側): 置いた時の候補に無かった記事(置いた後に作った記事)を、関係ない英語の長い本文で直しても、その草稿に正解を付けない。
func TestPlacementIgnoresEditsOfPagesThatWereNotCandidates(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	seedLessons(t, st, scope, "Keep both changelogs in step", "Commit author identity")
	draft := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", AppliesTo: "releasing"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{draft}}); err != nil {
		t.Fatal(err)
	}
	later := seedLessons(t, st, scope, "Status page first")["Status page first"] // 置いた後に作った記事(候補に入っていない)
	body := "Read the status page first. Never push or create a release until the person has reviewed it and explicitly said so; the status page shows what is waiting."
	edit := notebook.Draft{ID: later, Sources: []string{src}, Title: "Status page first", Lesson: "Status page first.", AppliesTo: "Status page first を扱う時", Body: &body}
	if cov := coverageOf(draft.Lesson, edit); cov < placeMatchMin {
		t.Fatalf("この負例は被覆が下限を越えないと候補の条件を試せない: %.2f", cov)
	}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: edit}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("候補に無かった記事の直しで、その草稿に正解を付けない: %d 件", len(place))
	}
}

// ageRecords は、置いた草稿(new_lesson)の記録の時刻を d だけ前にずらす(古く置いた草稿を作る)。
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
