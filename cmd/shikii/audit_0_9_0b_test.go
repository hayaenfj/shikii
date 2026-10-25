package main

// 0.9.0 の再監査(監査者が足した)。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 新-1: 人の文の中にだけ印があり節の無い AGENTS.md に、アップデート(hook install)が節を作る(「節の無いファイルには作らない」に反する)。
func TestAudit090bHookInstallDoesNotCreateSectionForInlineMarker(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	human := "# 作業規則\n\n- shikii の節は `" + agentsMarker + "` で囲む決まりだが、この repo では節を置かない\n"
	os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(human), 0o644)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if b, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md")); string(b) != human {
		t.Fatalf("節の無い AGENTS.md に節を作った:\n%s", b)
	}
}

// 新-2: 別の記事の出典が欠けている(点検の問題は出るが、保存は断られない)だけで、hook は付ける記事を頼まなくなる。classify は通る。
func TestAudit090bSourceIssueElsewhereDoesNotBlockAsk(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	other := fmt.Sprintf("%016x", 778)
	if err := st.AppendFeedback(scope, store.Feedback{ID: other, Request: "r2", Summary: "s2", Reply: "y"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "前の版の記事", Lesson: "前の版の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(a.Page.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{other}, Title: "出典の欠けた記事", Lesson: "別の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"}); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(agent, "*", "*", "sources", other+".json"))
	if len(m) == 0 {
		m, _ = filepath.Glob(filepath.Join(agent, "**", other+".json"))
	}
	if len(m) == 0 {
		filepath.Walk(agent, func(p string, info os.FileInfo, err error) error {
			if err == nil && filepath.Base(p) == other+".json" {
				m = append(m, p)
			}
			return nil
		})
	}
	if len(m) != 1 {
		t.Fatalf("出典のファイルが見つからない: %v", m)
	}
	os.Remove(m[0])
	text, ids, err := classifyContext(agent, scope)
	if err != nil {
		t.Fatal(err)
	}
	_, cerr := book.Classify(a.Page.ID, "rule", "作業する時")
	if cerr == nil && len(ids) == 0 {
		t.Fatalf("classify は通る(%v)のに、hook は付ける記事を頼まず「付けられません」と言う: %s", cerr, text)
	}
}

// 確認: hook が頼んだ 5 本を順に付けると全部通り、Stop は止めない。違う記事を同時に付けても断られない。
func TestAudit090bClassifyFiveInOrderAndInParallel(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	src := fmt.Sprintf("%016x", 901)
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	var ids []string
	for i := 0; i < 10; i++ {
		r, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("前の版の記事 %d", i), Lesson: fmt.Sprintf("前の版の教訓 %d。", i), AppliesTo: "a", Kind: "rule", Scene: "作業する時", Related: func() []string {
			if len(ids) > 0 {
				return []string{ids[len(ids)-1]}
			}
			return nil
		}()})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Page.ID)
	}
	for _, id := range ids {
		if err := book.RewriteWithoutKindScene(id); err != nil {
			t.Fatal(err)
		}
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "q", "cwd": repo, "prompt": "README を直して"})
	s := loadSession(root, "q")
	if len(s.ClassifyAsked) != 5 {
		t.Fatalf("5 本頼む: %v", s.ClassifyAsked)
	}
	for _, id := range s.ClassifyAsked {
		if _, err := book.Classify(id, "fact", "作業する時"); err != nil {
			t.Fatalf("順に付けると通る: %s %v", id, err)
		}
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "q", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "need a kind and a scene") {
		t.Fatalf("付け終えたのに止めた: %s", out)
	}
	un, _ := book.Unclassified()
	var wg sync.WaitGroup
	errs := make([]error, len(un))
	for i, p := range un {
		wg.Add(1)
		go func(i int, id string) { defer wg.Done(); _, errs[i] = book.Classify(id, "fact", "作業する時") }(i, p.ID)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("違う記事を同時に付けても断らない: %s %v", un[i].ID, err)
		}
	}
	// 教訓・関連・変更理由は変わらない
	live, _ := book.Live()
	for _, p := range live {
		if !strings.HasPrefix(p.Lesson, "前の版の教訓") || (p.Title != "前の版の記事 0" && len(p.Related) != 1) {
			t.Fatalf("classify が教訓か関連を変えた: %+v", p)
		}
	}
}

// 確認: 印の組の数え方の端(空のファイル、末尾に改行の無いファイル、0.8.3 が作った節、閉じの印で終わり改行の無いファイル、2 回目は何もしない)。
func TestAudit090bAgentsSectionEdges(t *testing.T) {
	old083 := agentsMarker + "\n## shikii(判定の作法)\n\n- 同じ種類の判断は自分で決めず\n- 詳しい作法: ~/.claude/skills/shikii/SKILL.md(shikii init が置く)\n" + agentsMarker + "\n"
	cases := []struct{ name, in, wantPrefix, wantSuffix string }{
		{"空", "", agentsMarker, agentsMarker + "\n"},
		{"改行なし", "# 規則", "# 規則\n\n" + agentsMarker, agentsMarker + "\n"},
		{"0.8.3 の節(改行なしに足した)", "# 規則\n\n" + old083, "# 規則\n\n" + agentsMarker, agentsMarker + "\n"},
		{"閉じで終わり改行なし", "# 規則\n\n" + strings.TrimSuffix(old083, "\n"), "# 規則\n\n" + agentsMarker, agentsMarker + "\n"},
		{"節の後に人の文", "# 規則\n\n" + old083 + "\n## 後ろ\n", "# 規則\n\n" + agentsMarker, agentsMarker + "\n\n## 後ろ\n"},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "AGENTS.md")
		os.WriteFile(path, []byte(c.in), 0o644)
		if _, err := addAgentsSection(path); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		b, _ := os.ReadFile(path)
		got := string(b)
		if !strings.HasPrefix(got, c.wantPrefix) || !strings.HasSuffix(got, c.wantSuffix) || strings.Count(got, agentsMarker) != 2 || !strings.Contains(got, "wiki classify") {
			t.Fatalf("%s: %q", c.name, got)
		}
		if did, err := addAgentsSection(path); did || err != nil {
			t.Fatalf("%s: 2 回目は何もしない: %v %v", c.name, did, err)
		}
		if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
			t.Fatalf("%s: 一時ファイルが残った: %v", c.name, entries)
		}
	}
	// repo の中のリンク(CRLF)はリンクのまま、CRLF のまま
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(strings.ReplaceAll("# 規則\n\n"+old083, "\n", "\r\n")), 0o600)
	os.Symlink("CLAUDE.md", filepath.Join(dir, "AGENTS.md"))
	if did, err := addAgentsSection(filepath.Join(dir, "AGENTS.md")); !did || err != nil {
		t.Fatal(did, err)
	}
	info, _ := os.Lstat(filepath.Join(dir, "AGENTS.md"))
	real, _ := os.Stat(filepath.Join(dir, "CLAUDE.md"))
	b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if info.Mode()&os.ModeSymlink == 0 || real.Mode().Perm() != 0o600 || strings.Contains(strings.ReplaceAll(string(b), "\r\n", ""), "\n") || !strings.Contains(string(b), "wiki classify") {
		t.Fatalf("リンク・権限・CRLF: link=%v perm=%v %q", info.Mode()&os.ModeSymlink != 0, real.Mode().Perm(), b)
	}
}

// 穴: 印だけの行が 3 つ(節の中に人が印だけの行を書いた)なら触らない。len(marks) == 2 の条件を外しても今のテストは落ちない。
func TestAudit090bAgentsThreeMarkerLinesUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	in := "# 規則\n\n" + agentsMarker + "\n## shikii(判定の作法)\n\n- 古い\n" + agentsMarker + "\n- 人が節の中に足した大事な決まり\n" + agentsMarker + "\n"
	os.WriteFile(path, []byte(in), 0o644)
	addAgentsSection(path)
	if b, _ := os.ReadFile(path); string(b) != in {
		t.Fatalf("印だけの行が 3 つなら触らない:\n%s", b)
	}
}
