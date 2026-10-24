package main

// 0.8.3 の再監査(新-1・残-1・残-2)を再現するテスト(監査者が写しで書いた物を取り込んだ)。

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 再監査の新-1(日本語): 同じ place で 2 件置き(A, B の順)、A を X へ書き直して合流。X の本文は長め(今までの本文 + A)。
func TestPlacementTwoDraftsInOnePlaceLabelsTheMergedOne(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
	b := notebook.Draft{Sources: []string{src}, Title: "変更履歴の項目", Lesson: "変更履歴は英語と日本語の両方に、版ごとに同じ項目を同じ順で書く", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	trA, trB := placedTrace(t, out, 0, pid), placedTrace(t, out, 1, pid)
	body := "## 揃え方\n変更履歴は英語と日本語の両方に、版ごとに同じ項目を書く。片方だけに足さない。順も同じにする。\n## 見出し\n見出しは「版 — 題」の形にし、未公開の見出しのまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは「版 — 題」にし、未公開のまま出さない。", AppliesTo: "変更履歴を書く時", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[trA].Truth != "yes" {
		t.Errorf("合流した A に「はい」が付かない(A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
	}
}

// 再監査の新-1(英語、置いた順が逆): 英語。B を置き、その後に関係ない A を置く。B を X へ長い英語の本文で合流。
func TestPlacementEnglishLaterUnrelatedDraftDoesNotWin(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "Status page first", "Commit author identity")
	pid := ids["Status page first"]
	b := notebook.Draft{Sources: []string{src}, Title: "Read status", Lesson: "Read the status page before changing any decision spec.", AppliesTo: "operating"}
	outB, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{b}})
	if err != nil {
		t.Fatal(err)
	}
	a := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", AppliesTo: "releasing"}
	outA, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}})
	if err != nil {
		t.Fatal(err)
	}
	trB, trA := placedTrace(t, outB, 0, pid), placedTrace(t, outA, 0, pid)
	body := "A model returns a probability. From the truths people and results supply, the tool measures how the model's confidence relates to how often it is right, and draws a line above which it may act on its own. Until the line exists, nothing is automatic. Every decision falls into a band: act proceeds automatically, ask goes to a person, discard does nothing, abstain means an input or an endpoint is missing. Truths live apart from the model, so changing models does not start you over. Alternative wordings can be scored on the same truths and the better one kept. The page and the status command show whether it is working: whether delivered lessons helped, how many turns did the routine, how many turns guards stopped and how many were fixed, how many decisions wait for a person, and which lines have been drawn. Read the status page before changing any decision spec."
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "Status page first", Lesson: "Status page first; read it before changing a decision spec.", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[trB].Truth != "yes" || place[trA].Truth != "" {
		t.Errorf("合流した B でなく、後に置いた関係ない A に付いた(A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
	}
}

// 再監査で問題無し(置き換え): 置き換え(replaces)で、教訓を書き直した新しい記事。
func TestPlacementReplacementWithRewrittenLesson(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}})
	if err != nil {
		t.Fatal(err)
	}
	tr := placedTrace(t, out, 0, pid)
	nd := notebook.Draft{Sources: []string{src}, Replaces: []string{pid}, Title: "変更履歴の見出しと言語", Lesson: "変更履歴は英語と日本語を揃え、見出しは「版 — 題」にして未公開のまま出さない。", AppliesTo: "変更履歴を書く時"}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: nd}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[tr].Truth != "yes" {
		t.Errorf("置き換えで「はい」が付かない")
	}
}

// 再監査の残-1: 英語。関係ない A を置いた(X は候補)後、置いていない直しで X を長い英語の本文で保存(wiki ingest の直し)。
func TestPlacementEnglishEditOfACandidatePage(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "Status page first", "Commit author identity")
	pid := ids["Status page first"]
	a := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", AppliesTo: "releasing"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}}); err != nil {
		t.Fatal(err)
	}
	body := "A model returns a probability. From the truths people and results supply, the tool measures how the model's confidence relates to how often it is right, and draws a line above which it may act on its own. Until the line exists, nothing is automatic. Every decision falls into a band: act proceeds automatically, ask goes to a person, discard does nothing, abstain means an input or an endpoint is missing. Truths live apart from the model, so changing models does not start you over. Alternative wordings can be scored on the same truths and the better one kept. The page and the status command show whether it is working: whether delivered lessons helped, how many turns did the routine, how many turns guards stopped and how many were fixed, how many decisions wait for a person, and which lines have been drawn."
	edit := notebook.Draft{ID: pid, Sources: []string{src}, Title: "Status page first", Lesson: "Read the status page first when running the tool.", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: edit}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if len(place) != 0 {
		t.Errorf("関係ない英語の直しで、保存していない草稿に正解が付いた: %d 件", len(place))
	}
}

// 再監査で問題無し(同じ草稿を 2 回): 同じ草稿を 2 回置き、書き直して合流。
func TestPlacementSameDraftPlacedTwice(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
	for i := 0; i < 2; i++ {
		if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}}); err != nil {
			t.Fatal(err)
		}
	}
	body := "## 見出し\n見出しを「版 — 題」にする。公開前の見出しのまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にする。", AppliesTo: "変更履歴を書く時", Body: &body}
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
		t.Errorf("2 回置いた草稿の合流に「はい」が付かない")
	}
}

// 再監査の残-2: 完全一致の古い記録が今も付けられても、教訓を変えない合流に当たる。
func TestPlacementLabelableLeftoverExactDoesNotShadowMerge(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	// 記事を作った時の置いた記録の残り(候補の記事が今は無いので、正解が付かずに残っている物。実データでは 15 記事にある)
	if err := st.AppendRecord(store.Record{TraceID: "leftover1", SpecID: "wiki-place", SpecHash: "x", Model: "m", InputsHash: "h1", At: time.Now().Add(-48 * time.Hour),
		Inputs: map[string]string{"new_lesson": "変更履歴は英語と日本語を揃える。", "lesson": "コミットの説明を丁寧に残す。"}}); err != nil {
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

// 再監査の新-1(英語、1 回の place で 2 件): 英語。1 回の place で 2 件(A: 合流する、B: 関係ない別の話題)を置き、A を X へ長めの英語の本文で合流。
func TestPlacementEnglishTwoDraftsInOnePlace(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "Status page first", "Commit author identity")
	pid := ids["Status page first"]
	a := notebook.Draft{Sources: []string{src}, Title: "Read status", Lesson: "Read the status page before changing any decision spec.", AppliesTo: "operating"}
	b := notebook.Draft{Sources: []string{src}, Title: "Release needs consent", Lesson: "Never push or create a release until the person has reviewed it and explicitly said so.", AppliesTo: "releasing"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	trA, trB := placedTrace(t, out, 0, pid), placedTrace(t, out, 1, pid)
	body := "The status page shows whether the tool is working: which lines have been drawn, how many decisions wait for a person, whether delivered lessons helped, and how many turns the guards stopped. Read it first when something looks wrong, and read it before changing any decision spec, since a change of wording starts the truths over for that spec. It also says when the answers could not be separated with the current wording and model, and when there is only one answer so far."
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "Status page first", Lesson: "Read the status page first, and before changing any decision spec.", AppliesTo: "operating", Body: &body}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	place, _ := st.LabeledTraces("wiki-place")
	if place[trA].Truth != "yes" || place[trB].Truth != "" {
		t.Errorf("合流した A に付き、関係ない B には付かない(A=%q, B=%q)", place[trA].Truth, place[trB].Truth)
	}
}

// placedTrace は、置いた結果のうち draftIdx 番目の草稿の、記事 pid についての差し込み先の判定の trace。
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
	t.Fatalf("候補に %s が無い", pid)
	return ""
}

// 再監査の低: 置いてから 24 時間(placeMatchWindow)を過ぎた草稿には、書き直した合流でも正解を付けない。
func TestPlacementWindowSkipsDraftsPlacedLongAgo(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	ids := seedLessons(t, st, scope, "変更履歴は英語と日本語を揃える", "コミットの説明を丁寧に残す")
	pid := ids["変更履歴は英語と日本語を揃える"]
	a := notebook.Draft{Sources: []string{src}, Title: "変更履歴の見出し", Lesson: "変更履歴の見出しは「版 — 題」の形にし、未公開の見出しのまま出さない", AppliesTo: "変更履歴を書く時"}
	if _, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{a}}); err != nil {
		t.Fatal(err)
	}
	ageRecords(t, st, "wiki-place", a.Lesson, placeMatchWindow+time.Hour)
	body := "## 見出し\n見出しを「版 — 題」にする。公開前の見出しのまま出さない。"
	merged := notebook.Draft{ID: pid, Sources: []string{src}, Title: "変更履歴は英語と日本語を揃え、見出しは版と題にする", Lesson: "変更履歴は英語と日本語を揃える。見出しは版と題の形にする。", AppliesTo: "変更履歴を書く時", Body: &body}
	if cov := coverageOf(a.Lesson, merged); cov < placeMatchMin {
		t.Fatalf("時刻のほかは付く形でないと縛りを試せない: %.2f", cov)
	}
	if _, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: merged}); err != nil {
		t.Fatal(err)
	}
	if place, _ := st.LabeledTraces("wiki-place"); len(place) != 0 {
		t.Fatalf("前の日に置いた草稿に、あいまいな突き合わせで正解を付けない: %d 件", len(place))
	}
}
