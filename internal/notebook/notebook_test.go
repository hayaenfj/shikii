package notebook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func seed(t *testing.T, b Book, n int) string {
	t.Helper()
	id := fmt.Sprintf("%016x", n)
	err := (store.Store{Dir: b.Root}).AppendFeedback(b.Scope, store.Feedback{
		ID: id, Request: "README install instructions", Summary: "updated instructions", Reply: "model startup instructions are missing",
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func draft(id, title string) Draft {
	return Draft{Title: title, Lesson: "Check model startup instructions when updating README installation instructions.", AppliesTo: "README installation instructions", Kind: "rule", Scene: "README を直す時", Sources: []string{id}}
}

func hasIssue(issues []Issue, kind string) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func TestNotebookPreservesSourcesAndRevisionHistory(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	d := draft(id, "Installation instructions")
	first, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	sources, _ := b.directory(false, "sources")
	original, err := os.ReadFile(filepath.Join(sources, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := immutable(filepath.Join(sources, id+".json"), []byte("replacement")); err == nil {
		t.Fatal("保存した原資料を別の内容で上書きできない")
	}
	pages, _ := b.directory(false, "pages")
	log, _ := os.ReadFile(filepath.Join(pages, "log.md"))
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(pages, "log.md"))
	if !bytes.Equal(log, again) {
		t.Fatal("同じ取り込みを繰り返しても履歴を増やさない")
	}
	id2 := seed(t, b, 2)
	d.ID, d.Sources, d.Lesson = first.Page.ID, []string{id2}, "Also check model startup instructions on a fresh installation."
	second, err := b.Ingest(d)
	if err != nil || len(second.Page.Sources) != 2 || second.Page.Revision == first.Page.Revision {
		t.Fatalf("更新しても前の出典を保持する: %+v %v", second, err)
	}
	versions, _ := filepath.Glob(filepath.Join(pages, "revisions", first.Page.ID+"-*.md"))
	if len(versions) != 2 {
		t.Fatalf("前の版を消さない: %v", versions)
	}
	for i := 3; i < 265; i++ {
		seed(t, b, i)
	}
	material, err := b.ReadSources([]string{id})
	if err != nil || len(material["source_material"].([]store.Feedback)) != 1 {
		t.Fatalf("最近の会話から外れた原文も出典として残る: %v %v", material, err)
	}
	after, _ := os.ReadFile(filepath.Join(sources, id+".json"))
	if !bytes.Equal(original, after) {
		t.Fatal("教訓を更新しても原資料を書き換えない")
	}
	view, err := b.Query("model startup instructions")
	if err != nil || len(view.Lessons) != 1 || len(view.Issues) != 0 || len(view.Pending) != 2 {
		t.Fatalf("教訓と未整理の会話は別々に検索する: %+v %v", view, err)
	}
	for _, query := range []string{"startup", "model startup"} {
		view, err := b.Query(query)
		if err != nil || len(view.Lessons) != 1 {
			t.Fatalf("短い検索語でも長い教訓を見つける: query=%q lessons=%d err=%v", query, len(view.Lessons), err)
		}
	}
	if view, err := b.Query("billing payments"); err != nil || len(view.Lessons) != 0 {
		t.Fatal("関係のない検索語では教訓を返さない")
	}
	if info, err := os.Stat(filepath.Join(sources, id+".json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("原資料は本人だけが読める")
	}
}

func TestNotebookRejectsForeignSourcesTamperingAndSymlinks(t *testing.T) {
	t.Run("仕事の区切り", func(t *testing.T) {
		root := t.TempDir()
		a, b := Book{Root: root, Scope: "/a/project"}, Book{Root: root, Scope: "/b/project"}
		id := seed(t, a, 1)
		if _, err := a.Ingest(draft(id, "local lesson")); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Ingest(draft(id, "foreign")); err == nil {
			t.Fatal("同名の別プロジェクトの観察を引用してはいけない")
		}
		if _, err := b.ReadSources([]string{id}); err == nil {
			t.Fatal("原資料の読み出しも同じ仕事に限る")
		}
		if _, err := a.Ingest(draft("../../outside", "invalid")); err == nil {
			t.Fatal("出典IDを path にしてはいけない")
		}
	})
	t.Run("出典の変更", func(t *testing.T) {
		b := Book{Root: t.TempDir(), Scope: "/project"}
		id := seed(t, b, 1)
		if _, err := b.Ingest(draft(id, "original")); err != nil {
			t.Fatal(err)
		}
		dir, _ := b.directory(false, "sources")
		path := filepath.Join(dir, id+".json")
		original, _ := os.ReadFile(path)
		_ = os.WriteFile(path, bytes.ReplaceAll(original, []byte("missing"), []byte("present")), 0o600)
		view, err := b.Query("")
		if err != nil || len(view.Lessons) != 0 || !hasIssue(view.Issues, "changed-source") {
			t.Fatalf("出典が変わった教訓を参照から外す: %+v %v", view, err)
		}
		if _, err := b.ReadSources([]string{id}); err == nil {
			t.Fatal("変更された原文を確かな出典として返さない")
		}
		if _, err := b.Ingest(draft(id, "another title")); err == nil {
			t.Fatal("別の題で取り込み直しても出典の変更を正当化しない")
		}
		report, err := b.Lint()
		if err != nil || !hasIssue(report.Issues, "changed-source") {
			t.Fatalf("点検で出典の変更が分かる: %+v %v", report, err)
		}
		_ = os.Remove(path)
		view, _ = b.Query("")
		if len(view.Lessons) != 0 || !hasIssue(view.Issues, "missing-source") {
			t.Fatal("原文の消失も検出する")
		}
	})
	t.Run("生成ページの変更", func(t *testing.T) {
		b := Book{Root: t.TempDir(), Scope: "/project"}
		result, err := b.Ingest(draft(seed(t, b, 1), "original"))
		if err != nil {
			t.Fatal(err)
		}
		dir, _ := b.directory(false, "pages")
		f, err := os.OpenFile(filepath.Join(dir, result.Page.ID+".md"), os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("\nchanged body\n")
		f.Close()
		view, err := b.Query("")
		if err != nil || len(view.Lessons) != 0 || !hasIssue(view.Issues, "changed-page") {
			t.Fatal("本文だけを書き換えた時も検出する")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		root, outside := t.TempDir(), t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "wiki"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "wiki", "lessons")); err != nil {
			t.Fatal(err)
		}
		b := Book{Root: root, Scope: "/project"}
		if _, err := b.Ingest(draft(seed(t, b, 1), "escaped")); err == nil {
			t.Fatal("生成領域の symlink を通して書かない")
		}
		files, _ := os.ReadDir(outside)
		if len(files) != 0 {
			t.Fatal("生成領域の外を変更しない")
		}
	})
}

func TestNotebookReplacesLessonsAndMaintainsIndexOnASlowCycle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	old, err := b.Ingest(draft(seed(t, b, 1), "old instructions"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(seed(t, b, 2), "new instructions")
	d.Replaces = []string{old.Page.ID}
	newer, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	view, _ := b.Query("")
	if len(view.Lessons) != 1 || view.Lessons[0].ID != newer.Page.ID {
		t.Fatal("更新前の教訓を同時に勧めない")
	}
	d.ID, d.Replaces = old.Page.ID, []string{newer.Page.ID}
	if _, err := b.Ingest(d); err == nil {
		t.Fatal("教訓の置き換えを循環させない")
	}
	// 置き換えの解釈を訂正した時は解除でき、もう一度戻しても履歴を失わない。
	undo := draft(newer.Page.Sources[0].ID, newer.Page.Title)
	undo.ID, undo.Replaces = newer.Page.ID, []string{}
	restored, err := b.Ingest(undo)
	if err != nil {
		t.Fatal(err)
	}
	view, _ = b.Query("")
	if len(view.Lessons) != 2 {
		t.Fatal("明示した空の replaces で置き換えを解除できる")
	}
	undo.Replaces = []string{old.Page.ID}
	again, err := b.Ingest(undo)
	if err != nil || again.Page.Previous != restored.Page.Revision || again.Page.Revision == newer.Page.Revision {
		t.Fatal("同じ内容への復帰も、新しい変更として履歴を残す")
	}
	dir, _ := b.directory(false, "pages")
	_ = os.Remove(filepath.Join(dir, "index.md"))
	report, err := b.Lint()
	if err != nil || len(report.Repaired) != 1 || len(report.Issues) != 0 {
		t.Fatalf("索引は自動で直せる: %+v %v", report, err)
	}
	if err := b.MaybeLint(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.md"), []byte("stale index"), 0o600)
	if err := b.MaybeLint(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "index.md"))
	if string(data) != "stale index" {
		t.Fatal("同じ日に毎ターン点検を繰り返さない")
	}
	before := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, ".last-lint"), before, before)
	if err := b.MaybeLint(); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "index.md"))
	if string(data) == "stale index" {
		t.Fatal("次の周期では索引を直す")
	}
	_ = os.Remove(filepath.Join(dir, old.Page.ID+".md"))
	report, err = b.Lint()
	if err != nil || !hasIssue(report.Issues, "missing-replaced-page") {
		t.Fatal("消えたページへのリンクを検出する")
	}
}

func TestNotebookConcurrentIngestKeepsEveryLessonAndLog(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	ids := []string{}
	for i := 0; i < 12; i++ {
		ids = append(ids, seed(t, b, i))
	}
	dir, err := b.directory(true, "pages")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() { _, err := b.Ingest(draft(ids[0], "Instructions 0")); blocked <- err }()
	select {
	case err := <-blocked:
		unlock()
		t.Fatalf("他の更新中は取り込みを待つ: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.Ingest(draft(id, fmt.Sprintf("Instructions %d", i)))
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	pages, issues, err := b.pages()
	if err != nil || len(pages) != 12 || len(issues) != 0 {
		t.Fatalf("並列の取り込みで教訓を失わない: pages=%d issues=%v err=%v", len(pages), issues, err)
	}
	dir, _ = b.directory(false, "pages")
	log, _ := os.ReadFile(filepath.Join(dir, "log.md"))
	if strings.Count(string(log), " | ingest ") != 12 {
		t.Fatal("変更履歴も失わない")
	}
	report, err := b.Lint()
	if err != nil || len(report.Issues) != 0 || len(report.Repaired) != 0 {
		t.Fatalf("並列処理後も出典・索引・過去の版が整合する: %+v %v", report, err)
	}
}

// 0.9.0: 教訓のある記事は、種類(5 つのどれか)と場面(「〜する時」、24 文字まで)が無いと保存しない。どの経路(CLI・MCP・HTTP・振り返り)も
// ここを通るので、どのエージェントでも付く。既存の記事を直す時に省けば、その記事の種類と場面を引き継ぐ。前の版の記事(種類も場面も無い)は、
// 直す時に付けないと保存できない(版を上げた後に埋まっていく)。
func TestIngestRequiresKindAndScene(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	d := draft(id, "決まりの記事")
	d.Kind, d.Scene = "", ""
	if _, err := b.Ingest(d); err == nil || !strings.Contains(err.Error(), "kind") || !strings.Contains(err.Error(), "scene") {
		t.Fatalf("種類と場面が無い教訓は断り、直し方を返す: %v", err)
	}
	for _, bad := range []struct{ kind, scene string }{{"memo", "コミットする時"}, {"rule", ""}, {"rule", strings.Repeat("あ", 25)}, {"rule", "二行\n目"}} {
		d.Kind, d.Scene = bad.kind, bad.scene
		if _, err := b.Ingest(d); err == nil {
			t.Fatalf("種類は 5 つのどれか、場面は 24 文字までの 1 行: %+v", bad)
		}
	}
	d.Kind, d.Scene = "rule", "コミットする時"
	first, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	if first.Page.Kind != "rule" || first.Page.Scene != "コミットする時" {
		t.Fatalf("種類と場面を記事に残す: %+v", first.Page)
	}
	// 直す時に省けば引き継ぐ
	d.ID, d.Kind, d.Scene, d.Lesson = first.Page.ID, "", "", d.Lesson+" 追記。"
	again, err := b.Ingest(d)
	if err != nil || again.Page.Kind != "rule" || again.Page.Scene != "コミットする時" {
		t.Fatalf("直す時に省けば種類と場面を引き継ぐ: %+v %v", again.Page, err)
	}
	// 種類も場面も無い前の版の記事は、直す時に付けないと保存できない
	legacyDraft := draft(id, "前の版の記事")
	lres, err := b.Ingest(legacyDraft)
	if err != nil {
		t.Fatal(err)
	}
	legacy := *lres.Page
	if err := b.RewriteWithoutKindScene(legacy.ID); err != nil {
		t.Fatal(err)
	}
	d.ID, d.Title = legacy.ID, legacy.Title
	if _, err := b.Ingest(d); err == nil {
		t.Fatal("種類も場面も無い前の版の記事は、直す時に付ける")
	}
	d.Kind, d.Scene = "howto", "公開する時"
	if res, err := b.Ingest(d); err != nil || res.Page.Kind != "howto" {
		t.Fatalf("付ければ保存できる: %+v %v", res.Page, err)
	}
}

// 0.9.0: 版を上げる前に作った記事(種類も場面も無い)は Unclassified に並び、Classify で種類と場面だけを付けられる
// (教訓・題・本文はそのまま。新しい版になり、前の版へのリンクを持つ)。付ければ一覧から消える。
func TestClassifyLegacyPages(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	first, err := b.Ingest(draft(id, "決まりの記事"))
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	lres, err := b.Ingest(draft(id, "前の版の記事"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := *lres.Page
	if err := b.RewriteWithoutKindScene(legacy.ID); err != nil {
		t.Fatal(err)
	}
	un, err := b.Unclassified()
	if err != nil || len(un) != 1 || un[0].ID != legacy.ID {
		t.Fatalf("種類か場面の無い記事だけを並べる: %+v %v", un, err)
	}
	if _, err := b.Classify(legacy.ID, "memo", "公開する時"); err == nil {
		t.Fatal("種類は 5 つのどれか")
	}
	if _, err := b.Classify("00000000000000cc", "rule", "公開する時"); err == nil {
		t.Fatal("無い記事には付けられない")
	}
	res, err := b.Classify(legacy.ID, "howto", "公開する時")
	if err != nil {
		t.Fatal(err)
	}
	if p := res.Page; p.Kind != "howto" || p.Scene != "公開する時" || p.Lesson != legacy.Lesson || p.Title != legacy.Title || p.Previous == "" {
		t.Fatalf("種類と場面だけを付けた新しい版にする: %+v", p)
	}
	if un, _ := b.Unclassified(); len(un) != 0 {
		t.Fatalf("付ければ一覧から消える: %+v", un)
	}
}

// 0.9.0: wiki の検索(CLI・MCP・HTTP)の最初の返事に、種類と場面の無い記事の数と 5 本までを載せる(hook の無いエージェントにも届く)。
func TestQueryPageListsUnclassifiedPages(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	res, err := b.Ingest(draft(id, "前の版の記事"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := b.QueryPage("", "")
	if err != nil || v.UnclassifiedCount != 0 || len(v.Unclassified) != 0 {
		t.Fatalf("全部付いていれば出さない: %+v %v", v.Unclassified, err)
	}
	if err := b.RewriteWithoutKindScene(res.Page.ID); err != nil {
		t.Fatal(err)
	}
	v, err = b.QueryPage("", "")
	if err != nil || v.UnclassifiedCount != 1 || len(v.Unclassified) != 1 || v.Unclassified[0].ID != res.Page.ID || !strings.Contains(v.UnclassifiedHow, "classify") {
		t.Fatalf("種類と場面の無い記事の数と、付け方を載せる: %+v %d %q %v", v.Unclassified, v.UnclassifiedCount, v.UnclassifiedHow, err)
	}
}
