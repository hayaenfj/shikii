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
	if err != nil || quiet.Search != nil || len(quiet.Lessons) != RecallCandidates {
		t.Fatal("自動配送の候補は RecallCandidates 件までで、候補一覧で膨らませない", err)
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

// 自動配送の門(0.7.4 の監査の指摘): どの記事にもある識別子(例: shikii)と仮名の 2 文字だけでは届けない。
// 珍しい識別子(例: Makefile)は 1 語の一致でも届ける(以前は 2 語が要った)。
func TestRecallIgnoresUbiquitousIdentifiersAndDeliversOnRareOnes(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	for i, a := range []string{"shikiiの変更履歴を追加・更新する時", "shikiiでコミットする時", "shikiiのライセンスを説明する時", "shikiiの公開作業を報告する時", "shikiiの画面を直す時"} {
		d := draft(source, "教訓"+string(rune('A'+i)))
		d.Lesson, d.AppliesTo = "shikii についての決まり "+string(rune('A'+i)), a
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	mk := draft(source, "Makefile の決まり")
	mk.Lesson, mk.AppliesTo = "make test を先に回す。", "Makefile を触る時"
	rm, err := b.Ingest(mk)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := b.Recall("shikii の説明を直して"); len(p) != 0 {
		t.Fatalf("どの記事にもある識別子と仮名だけでは届けない: %+v", p)
	}
	if p, _ := b.Recall("shikii statusも画面に出したほうが良いよね。クリックしたら詳細も見える感じで"); len(p) > 1 || (len(p) == 1 && !strings.Contains(p[0].AppliesTo, "画面")) {
		t.Fatalf("遍在する識別子で関係ない記事を届けない: %+v", p)
	}
	if p, _ := b.Recall("Makefile で test を回して"); len(p) != 1 || p[0].ID != rm.Page.ID {
		t.Fatalf("珍しい識別子は 1 語でも届ける: %+v", p)
	}
}

// 監査 R2: 依頼側の割合の経路も内容語だけで数える(短い依頼で「を直」「直す」と遍在する識別子だけで届いていた)。
// 記事が 4 未満の wiki でも、全記事にある語(プロジェクト名)1 語では届けない。
func TestRecallGateCountsOnlyContentTermsOnEveryPath(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	for i, a := range []string{"fooの画面を直す時", "fooのテストを回す時", "fooの文書を書く時"} {
		d := draft(source, "教訓"+string(rune('A'+i)))
		d.Lesson, d.AppliesTo = "foo の決まり "+string(rune('A'+i)), a
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"foo の説明を直して", "README を直す", "誤字を直す", "foo のテストを回して"} {
		p, _ := b.Recall(q)
		if q == "foo のテストを回して" {
			if len(p) != 1 || !strings.Contains(p[0].AppliesTo, "テスト") {
				t.Fatalf("内容語が合えば届く: %q → %+v", q, p)
			}
			continue
		}
		if len(p) != 0 {
			t.Fatalf("全記事にある語と文法の組だけでは届けない: %q → %+v", q, p)
		}
	}
}

// 監査 R3: 重なる 2 文字の組(表示+示す は「表示す」1 語)を 2 語に数えない。数字だけは識別子にしない。しきい値は負例で守る。
func TestRecallCountsOverlappingBigramsOnceAndKeepsThresholds(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	for i, a := range []string{"一覧を表示する時", "2026 年の記録を読む時", "変更履歴を更新する時", "画面を直す時", "テストを書く時"} {
		d := draft(source, "教訓"+string(rune('A'+i)))
		d.Lesson, d.AppliesTo = "決まり "+string(rune('A'+i)), a
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"表示する", "更新する", "2026-10-23 のログを見て"} {
		if p, _ := b.Recall(q); len(p) != 0 {
			t.Fatalf("重なる組や数字だけでは届けない: %q → %+v", q, p)
		}
	}
	if p, _ := b.Recall("一覧の表示を直す"); len(p) == 0 || !strings.Contains(p[0].AppliesTo, "表示") {
		t.Fatalf("別の語が 2 つ合えば届く: %+v", p)
	}
}

// 監査 R3: 門のしきい値を負例で守る。依頼側 0.35・記事側 0.15 のどちらにも届かない一致(長い依頼の 2 語が、長い適用条件の記事に合うだけ)は届けない。
// 記事が 1 本の wiki では、その記事にしか無い語も「珍しい語」として扱い、1 語の識別子でも届く。
func TestRecallThresholdsAndSinglePageWiki(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	long := draft(source, "長い適用条件")
	long.Lesson, long.AppliesTo = "決まり", "判定 画面 配送 記録 監査 公開 文書 翻訳 検証 設計 手順 索引 更新 削除 要約 分類 比較 評価 を扱う時"
	if _, err := b.Ingest(long); err != nil {
		t.Fatal(err)
	}
	for i, a := range []string{"木材を切る時", "川を渡る時", "山に登る時", "海で泳ぐ時"} {
		d := draft(source, "他の教訓"+string(rune('A'+i)))
		d.Lesson, d.AppliesTo = "決まり", a
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := b.Recall("判定 画面 木星 土星 火星 水星 金星 天王"); len(p) != 0 {
		t.Fatalf("依頼の 8 語のうち 2 語(0.25)、記事の 20 語のうち 2 語(0.1)では届けない: %+v", p)
	}
	if p, _ := b.Recall("判定 画面 翻訳"); len(p) != 1 || p[0].Title != "長い適用条件" {
		t.Fatalf("依頼の内容語がほぼ全部合えば届く: %+v", p)
	}
	one := Book{Root: t.TempDir(), Scope: "/project"}
	s1 := seed(t, one, 1)
	d := draft(s1, "唯一の教訓")
	d.Lesson, d.AppliesTo = "決まり", "shikii の README を直す時"
	if _, err := one.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if p, _ := one.Recall("README を直して"); len(p) != 1 {
		t.Fatalf("記事が 1 本の wiki では比べようが無いので、その記事の識別子 1 語で届く: %+v", p)
	}
}
