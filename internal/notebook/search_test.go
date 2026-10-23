package notebook

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchKeepsCommandsAndRanksApplicableFields(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	var target string
	for i, title := range []string{"Orbit display", "Orbit history", "Orbit update"} {
		d := draft(source, title)
		d.Lesson, d.AppliesTo = "Orbit command reference", "Orbit maintenance"
		body := "Use orbit update to refresh installed packages."
		d.Body = &body
		if i == 2 {
			d.AppliesTo = "orbit update installation"
		}
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			target = r.Page.ID
		}
	}
	for _, q := range []string{"orbit update", "ORBIT UPDATE"} {
		v, err := b.Query(q)
		if err != nil || len(v.Lessons) == 0 || v.Lessons[0].ID != target {
			t.Fatalf("コマンドを保持し題と適用条件を本文だけの一致より優先する: query=%q result=%+v err=%v", q, v.Lessons, err)
		}
	}
	for _, q := range []string{"update", "installed packages"} {
		v, err := b.Query(q)
		if err != nil || len(v.Lessons) == 0 {
			t.Fatalf("本文とコマンド名を検索できる: %q %v", q, err)
		}
	}
	if v, err := b.Query("orbit unrelated"); err != nil || len(v.Lessons) != 0 {
		t.Fatal("一般的な製品名だけの一致を返さない")
	}
	if p, err := b.Recall("installed packages"); err != nil || len(p) != 0 {
		t.Fatal("自動配送は本文だけの一致を使わない")
	}
}

func TestSearchShowsBoundedCandidatesBeyondFullArticles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	for i := range 10 {
		source := seed(t, b, i+1)
		d := draft(source, "参加者の受付 "+source)
		d.Lesson, d.AppliesTo = "受付の規定を確認する", "説明会の参加者受付"
		body := "BODY_ONLY_" + source
		d.Body = &body
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	v, err := b.Query("参加者 受付")
	if err != nil || v.Search == nil || v.Search.Total != 10 || len(v.Search.Candidates) != 10 || len(v.Lessons) != 2 {
		t.Fatalf("本文2件とは別に一致した全候補を返す: %+v %v", v, err)
	}
	for i, c := range v.Search.Candidates {
		if c.ID == "" || c.Revision == "" || c.Title == "" || c.AppliesTo == "" || len(c.Fields) == 0 || len(c.Terms) == 0 {
			t.Fatalf("候補を適用条件と一致箇所から比較できる: %+v", c)
		}
		if i < 2 && (c.ID != v.Lessons[i].ID || c.Revision != v.Lessons[i].Revision) {
			t.Fatal("候補の順位と返した本文の版が対応する")
		}
	}
	selected, err := b.Query("lesson:" + v.Search.Candidates[2].ID)
	if err != nil || len(selected.Lessons) != 1 || selected.Lessons[0].Body == "" {
		t.Fatal("3位以降の記事を選んで本文へ進める", err)
	}
	data, _ := json.Marshal(v)
	if strings.Contains(string(data), selected.Lessons[0].Body) {
		t.Fatal("候補だけの記事の本文まで一括配送しない")
	}
	for _, q := range []string{"該当しない話題", ""} {
		other, err := b.Query(q)
		if err != nil || (q == "" && other.Search != nil) || (q != "" && (other.Search == nil || other.Search.Total != 0 || len(other.Search.Candidates) != 0)) {
			t.Fatal("0件と全体一覧を検索結果の上限と区別する", q, err)
		}
	}
	quiet, err := b.query("参加者 受付", true)
	if err != nil || quiet.Search != nil || len(quiet.Lessons) != 2 {
		t.Fatal("自動配送を候補一覧で膨らませない", err)
	}
}

// 自動配送(Recall)は、依頼文が長くても適用条件に合う教訓を届ける(0.7.4)。
// 0.7.3 までは「依頼の語のうち一致した割合」で門を決めていたので、長い依頼ほど薄まって届かなかった
// (2026-10-23 の実験で、適用条件に CHANGELOG.md と書いた教訓が、CHANGELOG.md への依頼に 6 回中 0 回しか届かなかった)。
func TestRecallDeliversForLongRequestsByPageCoverage(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	a := draft(source, "変更履歴は英語と日本語を揃える")
	a.Lesson, a.AppliesTo = "変更履歴を足す時は CHANGELOG.md と CHANGELOG_ja.md の両方に同じ項目を書く。", "CHANGELOG.md や変更履歴に項目を追加・更新する時"
	ra, err := b.Ingest(a)
	if err != nil {
		t.Fatal(err)
	}
	o := draft(source, "出力には log.Printf を使う")
	o.Lesson, o.AppliesTo = "fmt.Println ではなく log.Printf を使う。", "main.go に値を出力する行を足す時"
	if _, err := b.Ingest(o); err != nil {
		t.Fatal(err)
	}
	long := "CHANGELOG.md に 1.1.0 の項目として「設定ファイルの読み込みに対応」を足してください。質問はせず、最後まで進めてください。"
	if p, err := b.Recall(long); err != nil || len(p) != 1 || p[0].ID != ra.Page.ID {
		t.Fatalf("長い依頼でも適用条件に合う教訓だけを届ける: %+v %v", p, err)
	}
	if p, err := b.Recall("README の誤字を直してください。質問はせず、最後まで進めてください。"); err != nil || len(p) != 0 {
		t.Fatalf("合わない依頼には届けない: %+v %v", p, err)
	}
}
