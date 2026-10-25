package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestCapturedSourcesStayScopedImmutableAndBounded(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/one"}
	o := Observation{Kind: "test", Title: "Smoke check", Content: "PASS", Reference: "go test ./...", Version: "fixture-v1", ObservedAt: time.Unix(1, 0)}
	r, err := b.Capture(o)
	if err != nil {
		t.Fatal(err)
	}
	again, err := b.Capture(o)
	if err != nil || again.ID != r.ID {
		t.Fatal("同じ観察の再送は増やさない", err)
	}
	if _, err := (Book{Root: b.Root, Scope: "/two"}).ReadSources([]string{r.ID}); err == nil {
		t.Fatal("別プロジェクトの出典を読まない")
	}
	for _, bad := range []Observation{
		{Kind: "confirmed", Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
		{Kind: o.Kind, Title: o.Title, Content: strings.Repeat("x", (32<<10)+1), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
		{Kind: o.Kind, Title: o.Title, Content: strings.Repeat("\x01", 32<<10), Reference: o.Reference, Version: o.Version, ObservedAt: o.ObservedAt},
	} {
		if _, err := b.Capture(bad); err == nil {
			t.Fatal("不明な種類や過大な本文・JSONを保存しない")
		}
	}
	dir, _ := b.directory(false, "sources")
	path := filepath.Join(dir, r.ID+".json")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "PASS", "FAIL", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadSources([]string{r.ID}); err == nil {
		t.Fatal("引用前の改変も内容IDで検出する")
	}
	if _, err := b.Ingest(draft(r.ID, "Changed source")); err == nil {
		t.Fatal("改変した原資料を教訓化できない")
	}
}

func TestSourceOriginalsSurviveRecordRemovalAndDetectChanges(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	st := store.Store{Dir: b.Root}
	request, summary := "README installation", strings.Repeat("A", 1500)
	rec := store.Record{TraceID: "0000000000000012", SpecID: "done-as-asked", Inputs: map[string]string{"request": request, "summary": summary}, At: time.Now()}
	rec.InputsHash = store.HashInputs(rec.Inputs)
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	full := summary + strings.Repeat("B", 18000) + "END-OF-ORIGINAL"
	if err := st.SaveRecordText(rec, map[string]string{"summary": full}); err != nil {
		t.Fatal(err)
	}
	f := store.Feedback{ID: "0000000000000013", Request: request, Summary: summary, Reply: "Include the startup check", Targets: []store.ReviewTarget{{Spec: rec.SpecID, TraceID: rec.TraceID}}}
	if err := st.AppendFeedback(b.Scope, f); err != nil {
		t.Fatal(err)
	}
	page, err := b.Ingest(draft(f.ID, "Installation observations"))
	if err != nil {
		t.Fatal(err)
	}
	if page.Page.Sources[0].TextHash == "" {
		t.Fatal("保存した原文のハッシュを出典に含める")
	}
	if err := os.RemoveAll(filepath.Join(b.Root, "records", "text")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(b.Root, "records", rec.SpecID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	material, err := b.ReadSources([]string{f.ID})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(material)
	if !strings.Contains(string(data), "END-OF-ORIGINAL") {
		t.Fatal("判定の記録を削除してもwikiの原文は残る")
	}
	dir, _ := b.directory(false, "sources")
	if err := os.WriteFile(filepath.Join(dir, f.ID+".text.json"), []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadSources([]string{f.ID}); err == nil {
		t.Fatal("長文の改変も検出する")
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 0 || !hasIssue(v.Issues, "changed-source-text") {
		t.Fatal("長文の改変した教訓を配送しない", err)
	}
}

// 0.9.2: フォルダを移した・名前を変えた後、wiki の置き場(sha256(パス) の名)が古い名のまま残ると、wiki は黙って空になっていた。
// lint は古い名の置き場を other-path-store として知らせる。置き場を新しい名に移した後も、前のパスで控えた原資料(id にパスが入る)は
// 新しいパスでは確かめられず、その記事を直す時に出典に使えない。lint は source-from-other-path として知らせる(会話の出典は知らせない)。
func TestLintFindsWikiLeftUnderOldPath(t *testing.T) {
	root := t.TempDir()
	old := Book{Root: root, Scope: "/work/old"}
	conv := seed(t, old, 1)
	if _, err := old.Ingest(draft(conv, "会話の出典の記事")); err != nil {
		t.Fatal(err)
	}
	o, err := old.Capture(Observation{Kind: "test", Title: "確かめた", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cited, err := old.Ingest(Draft{Title: "原資料の出典の記事", Lesson: "確かめてから書く。", AppliesTo: "a", Kind: "rule", Scene: "書く時", Sources: []string{o.ID}})
	if err != nil {
		t.Fatal(err)
	}
	moved := Book{Root: root, Scope: "/work/new"} // フォルダの名前を変えた後
	h := digest([]byte(old.Scope))
	stores, err := moved.OtherPathStores()
	if err != nil || strings.Join(stores, ",") != "records/wiki-sources/"+h+",wiki/lessons/"+h {
		t.Fatalf("古い名の 2 つの置き場を返す: %v %v", stores, err)
	}
	if again, _ := old.OtherPathStores(); len(again) != 0 {
		t.Fatalf("今のパスの置き場は数えない: %v", again)
	}
	report, err := moved.Lint()
	if err != nil {
		t.Fatal(err)
	}
	other := 0
	for _, issue := range report.Issues {
		if issue.Kind == "other-path-store" {
			other++
		}
	}
	if other != 2 {
		t.Fatalf("新しいパスに wiki がまだ無くても、古い名の置き場を知らせる: %+v", report.Issues)
	}
	// 原資料の置き場は新しい名へ写す(古い方は消さない)。写し終えた置き場は、もう知らせない
	n := digest([]byte(moved.Scope))
	if err := os.MkdirAll(filepath.Join(root, "records/wiki-sources", n), 0o700); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "records/wiki-sources", h))
	entries = slices.DeleteFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".") }) // 錠は置き場ごとに作る
	for i, e := range entries {
		data, _ := os.ReadFile(filepath.Join(root, "records/wiki-sources", h, e.Name()))
		if i == 0 {
			continue // 1 つ写し忘れた間は、まだ知らせる
		}
		_ = os.WriteFile(filepath.Join(root, "records/wiki-sources", n, e.Name()), data, 0o600)
	}
	if stores, _ := moved.OtherPathStores(); len(stores) != 2 {
		t.Fatalf("写し忘れたファイルがある間は知らせる: %v", stores)
	}
	data, _ := os.ReadFile(filepath.Join(root, "records/wiki-sources", h, entries[0].Name()))
	_ = os.WriteFile(filepath.Join(root, "records/wiki-sources", n, entries[0].Name()), data, 0o600)
	if stores, _ := moved.OtherPathStores(); strings.Join(stores, ",") != "wiki/lessons/"+h {
		t.Fatalf("写し終えた置き場は数えない: %v", stores)
	}
	// 記事の置き場は新しい名に移し、記事の出典のリンクの名も書き換える(手順の記事のとおり)
	if err := os.Rename(filepath.Join(root, "wiki/lessons", h), filepath.Join(root, "wiki/lessons", n)); err != nil {
		t.Fatal(err)
	}
	_ = filepath.Walk(filepath.Join(root, "wiki/lessons", n), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() && strings.HasSuffix(p, ".md") {
			data, _ := os.ReadFile(p)
			_ = os.WriteFile(p, []byte(strings.ReplaceAll(string(data), h, n)), 0o600)
		}
		return nil
	})
	report, err = moved.Lint()
	if err != nil {
		t.Fatal(err)
	}
	var fromOther []Issue
	for _, issue := range report.Issues {
		if issue.Kind == "source-from-other-path" {
			fromOther = append(fromOther, issue)
		} else {
			t.Errorf("移した後に残る指摘は、前のパスで控えた原資料だけ: %+v", issue)
		}
	}
	if len(fromOther) != 1 || fromOther[0].Page != cited.Page.ID || fromOther[0].Source != o.ID {
		t.Fatalf("前のパスで控えた原資料を知らせる: %+v", report.Issues)
	}
}
