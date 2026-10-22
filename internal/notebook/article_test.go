package notebook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// JSON経由で旧形式の読み手でも実行し、未知の項目の黙殺を検出する。
func articleDraft(t *testing.T, d Draft, fields map[string]any) Draft {
	t.Helper()
	data, _ := json.Marshal(d)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	for k, v := range fields {
		obj[k] = v
	}
	data, _ = json.Marshal(obj)
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func articleField(t *testing.T, p *Page, key string) any {
	t.Helper()
	data, _ := json.Marshal(p)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	return obj[key]
}

func TestNotebookArticleKeepsSummaryAndVersions(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	d := draft(seed(t, b, 1), "Installation")
	legacy, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := b.directory(false, "pages")
	legacyPath := filepath.Join(dir, "revisions", legacy.Page.ID+"-"+legacy.Page.Revision+".md")
	before, _ := os.ReadFile(legacyPath)
	body := "## Background\n\n" + strings.Repeat("Model lifecycle evidence. ", 120) + "\n\n## Unresolved\n\nquartzretention remains unverified."
	rich := articleDraft(t, d, map[string]any{"body": body})
	got, err := b.Ingest(rich)
	if err != nil || articleField(t, got.Page, "body") != body || got.Page.Lesson != d.Lesson || got.Page.Previous != legacy.Page.Revision {
		t.Fatalf("検索用要約と長い知識本文を別に保持する: %+v %v", got, err)
	}
	v, err := b.Query("quartzretention")
	if err != nil || len(v.Lessons) != 1 || articleField(t, &v.Lessons[0], "body") != body {
		t.Fatalf("本文だけにある語でも探せる: %+v %v", v, err)
	}
	if recalled, err := b.Recall("quartzretention"); err != nil || len(recalled) != 0 {
		t.Fatal("長い本文の偶然の一致で自動配送しない")
	}
	update := draft(d.Sources[0], d.Title)
	update.ID, update.Lesson = got.Page.ID, "Check startup and shutdown."
	next, err := b.Ingest(update)
	if err != nil || articleField(t, next.Page, "body") != body || next.Page.Previous != got.Page.Revision {
		t.Fatal("旧形式の更新で本文を消さない", err)
	}
	cleared, err := b.Ingest(articleDraft(t, update, map[string]any{"body": ""}))
	if err != nil || articleField(t, cleared.Page, "body") != nil {
		t.Fatal("明示した空の本文で解除できる", err)
	}
	after, _ := os.ReadFile(legacyPath)
	if !bytes.Equal(before, after) {
		t.Fatal("既存の版を書き換えない")
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("旧形式と新形式の版を併せて検査できる: %+v %v", report, err)
	}
	for _, body := range []string{strings.Repeat("x", (16<<10)+1), string([]byte{0xff})} {
		update.Body = &body
		if _, err := b.Ingest(update); err == nil {
			t.Fatal("過大な本文と不正なUTF-8を受け付けない")
		}
	}
	empty := ""
	if _, err := b.Ingest(Draft{Sources: d.Sources, Body: &empty}); err == nil {
		t.Fatal("本文だけを渡して出典の整理済み扱いにしない")
	}
}

func TestNotebookRelatedPagesAndExactLookup(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	a, err := b.Ingest(draft(seed(t, b, 1), "Startup"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(seed(t, b, 2), "Shutdown")
	linked, err := b.Ingest(articleDraft(t, d, map[string]any{"related": []string{a.Page.ID}}))
	if err != nil || articleField(t, linked.Page, "related") == nil {
		t.Fatal("知識同士の関連を保存する", err)
	}
	v, err := b.Query("lesson:" + linked.Page.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != linked.Page.ID {
		t.Fatal("関連リンクからページを直接読める", err)
	}
	if view, err := b.Query("lesson:ffffffffffffffff"); err != nil || len(view.Lessons) != 0 {
		t.Fatal("存在しないページの代わりに別の検索結果を返さない")
	}
	dir, _ := b.directory(false, "pages")
	markdown, _ := os.ReadFile(filepath.Join(dir, linked.Page.ID+".md"))
	if !bytes.Contains(markdown, []byte("]("+a.Page.ID+".md)")) {
		t.Fatal("生成Markdownでも関連ページを開ける")
	}
	d.ID = linked.Page.ID
	d.Lesson = "New shutdown summary."
	kept, err := b.Ingest(d)
	if err != nil || articleField(t, kept.Page, "related") == nil {
		t.Fatal("省略した関連を保持する", err)
	}
	// 関連は相互に持てる。置き換えの循環とは意味が違う。
	reverse := draft(a.Page.Sources[0].ID, a.Page.Title)
	reverse.ID = a.Page.ID
	if _, err := b.Ingest(articleDraft(t, reverse, map[string]any{"related": []string{linked.Page.ID}})); err != nil {
		t.Fatal("相互の関連リンクを許す", err)
	}
	other := Book{Root: b.Root, Scope: "/other"}
	foreign, err := other.Ingest(draft(seed(t, other, 3), "Foreign"))
	if err != nil {
		t.Fatal(err)
	}
	for _, related := range [][]string{{d.ID}, {foreign.Page.ID}, {"ffffffffffffffff"}, {"../outside"}} {
		if _, err := b.Ingest(articleDraft(t, d, map[string]any{"related": related})); err == nil {
			t.Fatalf("同じプロジェクトの別ページだけを関連付ける: %v", related)
		}
	}
	duplicates := make([]string, 12)
	for i := range duplicates {
		duplicates[i] = a.Page.ID
	}
	merged, err := b.Ingest(articleDraft(t, d, map[string]any{"related": duplicates}))
	if err != nil || len(merged.Page.Related) != 1 {
		t.Fatal("関連は件数で拒否せず重複を除く", err)
	}
	_ = os.Remove(filepath.Join(dir, a.Page.ID+".md"))
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "missing-related-page") {
		t.Fatalf("後から消えた関連ページを検出する: %+v %v", report, err)
	}
	if _, err := b.Ingest(articleDraft(t, d, map[string]any{"related": []string{}})); err != nil {
		t.Fatal("明示した空配列で壊れた関連を解除できる", err)
	}
}

func TestNotebookLegacyRevisionCompatibility(t *testing.T) {
	p := Page{ID: "0000000000000001", Title: "Example", Lesson: "Summary", AppliesTo: "Installation", Status: "hypothesis", Sources: []Source{{ID: "0000000000000002", Hash: "example"}}}
	legacyJSON := `{"id":"0000000000000001","title":"Example","lesson":"Summary","applies_to":"Installation","status":"hypothesis","sources":[{"id":"0000000000000002","hash":"example"}],"revision":""}`
	if revision(p) != digest([]byte(legacyJSON))[:16] {
		t.Fatal("本文と関連を持たない既存のハッシュを変えない")
	}
}
