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
			t.Fatalf("keeps commands and ranks title and applies-to over body-only matches: query=%q result=%+v err=%v", q, v.Lessons, err)
		}
	}
	for _, q := range []string{"update", "installed packages"} {
		v, err := b.Query(q)
		if err != nil || len(v.Lessons) == 0 {
			t.Fatalf("can search the body and command names: %q %v", q, err)
		}
	}
	if v, err := b.Query("orbit unrelated"); err != nil || len(v.Lessons) != 0 {
		t.Fatal("does not return a match on a common product name alone")
	}
	if p, err := b.Recall("installed packages"); err != nil || len(p) != 0 {
		t.Fatal("automatic delivery does not use body-only matches")
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
		t.Fatalf("returns every matching candidate apart from the 2 bodies: %+v %v", v, err)
	}
	for i, c := range v.Search.Candidates {
		if c.ID == "" || c.Revision == "" || c.Title == "" || c.AppliesTo == "" || len(c.Fields) == 0 || len(c.Terms) == 0 {
			t.Fatalf("candidates can be compared by applies-to and the matched parts: %+v", c)
		}
		if i < 2 && (c.ID != v.Lessons[i].ID || c.Revision != v.Lessons[i].Revision) {
			t.Fatal("candidate ranks match the revisions of the returned bodies")
		}
	}
	selected, err := b.Query("lesson:" + v.Search.Candidates[2].ID)
	if err != nil || len(selected.Lessons) != 1 || selected.Lessons[0].Body == "" {
		t.Fatal("can pick a page ranked 3rd or lower and go on to its body", err)
	}
	data, _ := json.Marshal(v)
	if strings.Contains(string(data), selected.Lessons[0].Body) {
		t.Fatal("does not deliver bodies of candidate-only pages in bulk")
	}
	for _, q := range []string{"該当しない話題", ""} {
		other, err := b.Query(q)
		if err != nil || (q == "" && other.Search != nil) || (q != "" && (other.Search == nil || other.Search.Total != 0 || len(other.Search.Candidates) != 0)) {
			t.Fatal("tells zero results and the full list apart from the search result limit", q, err)
		}
	}
	quiet, err := b.query("参加者 受付", true)
	if err != nil || quiet.Search != nil || len(quiet.Lessons) != RecallCandidates {
		t.Fatal("automatic-delivery candidates are at most RecallCandidates and not padded with the candidate list", err)
	}
}

// Automatic delivery (Recall) delivers lessons matching applies-to even for long requests (0.7.4).
// Up to 0.7.3 the gate was "the share of request words that matched", so the longer the request the more it was diluted and nothing arrived
// (in the 2026-10-23 experiment, a lesson with CHANGELOG.md in applies-to reached a CHANGELOG.md request 0 times out of 6).
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
		t.Fatalf("delivers only lessons matching applies-to even for a long request: %+v %v", p, err)
	}
	if p, err := b.Recall("README の誤字を直してください。質問はせず、最後まで進めてください。"); err != nil || len(p) != 0 {
		t.Fatalf("delivers nothing to a request that does not match: %+v %v", p, err)
	}
}

// Automatic-delivery gate (finding from the 0.7.4 audit): an identifier found in every page (e.g. shikii) plus a 2-kana pair does not deliver.
// A rare identifier (e.g. Makefile) delivers on a single-word match (2 words were needed before).
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
		t.Fatalf("an identifier found in every page plus kana does not deliver: %+v", p)
	}
	if p, _ := b.Recall("shikii statusも画面に出したほうが良いよね。クリックしたら詳細も見える感じで"); len(p) > 1 || (len(p) == 1 && !strings.Contains(p[0].AppliesTo, "画面")) {
		t.Fatalf("a ubiquitous identifier does not deliver unrelated pages: %+v", p)
	}
	if p, _ := b.Recall("Makefile で test を回して"); len(p) != 1 || p[0].ID != rm.Page.ID {
		t.Fatalf("a rare identifier delivers even alone: %+v", p)
	}
}

// audit R2: the request-side rate path also counts only content words (short requests delivered through 「を直」「直す」 and a ubiquitous identifier alone).
// Even in a wiki with fewer than 4 pages, a single word found in every page (the project name) does not deliver.
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
				t.Fatalf("delivers when content words match: %q → %+v", q, p)
			}
			continue
		}
		if len(p) != 0 {
			t.Fatalf("a word found in every page plus grammatical pairs does not deliver: %q → %+v", q, p)
		}
	}
}

// audit R3: overlapping 2-character pairs (表示 + 示す are the one word 「表示す」) do not count as 2 words. Digits alone are not identifiers. Thresholds are guarded by negative cases.
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
			t.Fatalf("overlapping pairs or digits alone do not deliver: %q → %+v", q, p)
		}
	}
	if p, _ := b.Recall("一覧の表示を直す"); len(p) == 0 || !strings.Contains(p[0].AppliesTo, "表示") {
		t.Fatalf("delivers when 2 different words match: %+v", p)
	}
}

// audit R3: guard the gate thresholds with negative cases. A match reaching neither request-side 0.35 nor page-side 0.15 (2 words of a long request matching a page with a long applies-to) does not deliver.
// In a wiki with a single page, words found only in that page count as "rare words", and a single identifier word delivers.
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
		t.Fatalf("2 of 8 request words (0.25) and 2 of 20 page words (0.1) do not deliver: %+v", p)
	}
	if p, _ := b.Recall("判定 画面 翻訳"); len(p) != 1 || p[0].Title != "長い適用条件" {
		t.Fatalf("delivers when almost all content words of the request match: %+v", p)
	}
	one := Book{Root: t.TempDir(), Scope: "/project"}
	s1 := seed(t, one, 1)
	d := draft(s1, "唯一の教訓")
	d.Lesson, d.AppliesTo = "決まり", "shikii の README を直す時"
	if _, err := one.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if p, _ := one.Recall("README を直して"); len(p) != 1 {
		t.Fatalf("in a wiki with one page there is nothing to compare, so one identifier word from that page delivers: %+v", p)
	}
}

// Measured 2026-10-23: one identifier word (hook) inside a long request delivered an unrelated page (how to return to a session).
// A single rare identifier delivers only when the request has few content words (3 or fewer) or the request-side rate is 0.35 or more.
func TestRecallStrongIdentifierNeedsAShortRequestOrCoverage(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	for i, a := range []string{"hook の実装と監査の経緯を見直したい時", "CHANGELOG を書く時", "画面の色を決める時"} {
		d := draft(source, "教訓"+string(rune('A'+i)))
		d.Lesson, d.AppliesTo = "決まり "+string(rune('A'+i)), a
		if i == 0 {
			d.Title = "版を出す前に監査を入れる"
		}
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := b.Recall("CHANGELOG を直して"); len(p) != 1 || !strings.Contains(p[0].AppliesTo, "CHANGELOG") {
		t.Fatalf("for a short request, a single rare identifier delivers: %+v", p)
	}
	long := "次に見ること 移した教訓が実際に依頼の前に届くかは、次の依頼で hook の memory の行を見て確かめます。公開は指示があってからです。これですね"
	if p, _ := b.Recall(long); len(p) != 0 {
		t.Fatalf("a single identifier word inside a long request does not deliver: %+v", p)
	}
	// A kanji word without kana (監査) matching the page title delivers for a 1–2 word request. A match only in applies-to, or a run ending in kana (表示す), does not (existing tests)
	if p, _ := b.Recall("監査して"); len(p) != 1 || !strings.Contains(p[0].Title, "監査") {
		t.Fatalf("for a short request, one kanji word matching the title delivers: %+v", p)
	}
	if p, _ := b.Recall("経緯を教えて"); len(p) != 0 {
		t.Fatalf("a word matching only applies-to does not deliver: %+v", p)
	}
}

// audit R4: fix the gate boundaries from both sides. A single identifier delivers with up to 3 content words (not with 4); the 0.35 rate has examples above and below;
// a kanji word matching the title delivers with up to 2 content words (not with 3); the title path does not deliver runs containing hiragana (表示す); title words must also be rare.
func TestRecallGateBoundariesOnBothSides(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	add := func(title, applies string) {
		d := draft(source, title)
		d.Lesson, d.AppliesTo = "決まり", applies
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	add("版を出す前に監査を入れる", "hook の実装と監査の経緯を見直したい時")
	add("教訓B", "CHANGELOG を書く時")
	add("教訓C", "画面の色を決める時")
	for _, c := range []struct {
		q    string
		want string
	}{
		{"監査を頼む", "監査"},  // 2 content words → delivered by the title word
		{"監査と翻訳と索引", ""}, // 3 content words → not delivered by the title word
		{"監査の結果を文書と翻訳と索引と図に反映して", ""},
		{"CHANGELOG と翻訳と索引を見直す", ""}, // 4 content words, rate 0.25 → a single identifier does not deliver
	} {
		p, _ := b.Recall(c.q)
		if (c.want == "" && len(p) != 0) || (c.want != "" && (len(p) != 1 || !strings.Contains(p[0].Title, c.want))) {
			t.Fatalf("%q → %+v", c.q, p)
		}
	}
	// Upper side of the 0.35 rate: when 4 of 5 content words match, it delivers even if the identifier is ubiquitous
	b2 := Book{Root: t.TempDir(), Scope: "/project"}
	s2 := seed(t, b2, 1)
	for i, a := range []string{"CHANGELOG を書く時(画面・文書・記録)", "画面と文書と記録の色を決める時", "画面と文書と記録の幅を決める時"} {
		d := draft(s2, "教訓"+string(rune('A'+i)))
		d.Lesson, d.AppliesTo = "決まり", a
		if _, err := b2.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := b2.Recall("CHANGELOG と翻訳と画面と文書と記録を決める"); len(p) == 0 || !strings.Contains(p[0].AppliesTo, "CHANGELOG") {
		t.Fatalf("with a high enough rate, an identifier delivers even for a longer request: %+v", p)
	}
	// Title path: runs containing hiragana do not deliver. Title words must also be rare
	b3 := Book{Root: t.TempDir(), Scope: "/project"}
	s3 := seed(t, b3, 1)
	for _, title := range []string{"一覧を表示する", "更新しても消えない記録", "画面の色", "画面の幅", "画面の字"} {
		d := draft(s3, title)
		d.Lesson, d.AppliesTo = "決まり", "その時"
		if _, err := b3.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"表示する", "更新して", "画面", "画面を直す"} {
		if p, _ := b3.Recall(q); len(p) != 0 {
			t.Fatalf("the title path also does not deliver runs containing hiragana or ubiquitous words: %q → %+v", q, p)
		}
	}
}

// audit R4: the katakana long-vowel mark (ー) does not split words. 「サーバーを直して」 reaches the title 「サーバーの起動」.
func TestRecallKeepsKatakanaLongVowel(t *testing.T) {
	if terms := searchTerms("サーバー"); len(terms) == 0 {
		t.Fatal("katakana words containing the long-vowel mark disappear")
	}
	b := Book{Root: t.TempDir(), Scope: "/project"}
	source := seed(t, b, 1)
	for _, title := range []string{"サーバーの起動", "画面の色", "記録の形"} {
		d := draft(source, title)
		d.Lesson, d.AppliesTo = "決まり", "その時"
		if _, err := b.Ingest(d); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := b.Recall("サーバーを直して"); len(p) != 1 || !strings.Contains(p[0].Title, "サーバー") {
		t.Fatalf("delivers on a word containing the long-vowel mark: %+v", p)
	}
}
