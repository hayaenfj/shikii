package main

// 0.9.0 の独立監査の再現(監査者が足した。直す前の状態で落ちることを確かめる)。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

const oldAgentsSection = agentsMarker + "\n## shikii(判定の作法)\n\n- 古い節\n" + agentsMarker + "\n"

// A1: AGENTS.md が別のファイルへのリンク(CLAUDE.md へのシンボリックリンクなど)だと、置き換えがリンクを普通のファイルにし、リンク先は古いまま。
func TestAudit090AgentsSymlinkKept(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("# 人の規則\n\n"+oldAgentsSection), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink("CLAUDE.md", link); err != nil {
		t.Fatal(err)
	}
	if _, err := addAgentsSection(link); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(link)
	got, _ := os.ReadFile(target)
	if info.Mode()&os.ModeSymlink == 0 || !strings.Contains(string(got), "wiki classify") {
		t.Fatalf("リンクが普通のファイルに置き換わった(リンクのまま=%v)、リンク先に新しい節がある=%v", info.Mode()&os.ModeSymlink != 0, strings.Contains(string(got), "wiki classify"))
	}
}

// A2: 人の文が節より前で印を書いている(印が 3 つ)と、人の文と節の間が消える。
func TestAudit090AgentsThreeMarkersKeepHumanText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	human := "# 作業規則\n\n- shikii の節は `" + agentsMarker + "` で囲まれている(手で触らない)\n- 人の大事な決まり: 本番には手で触らない\n\n"
	if err := os.WriteFile(path, []byte(human+oldAgentsSection), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := addAgentsSection(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "本番には手で触らない") || strings.Contains(string(got), "古い節") {
		t.Fatalf("人の文が消えた/古い節が残った:\n%s", got)
	}
}

// A3: CRLF の AGENTS.md(Windows の autocrlf)は、置き換えのたびに空行(\r だけの行)が 1 行ずつ増える。
func TestAudit090AgentsCRLFNoStrayLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	crlf := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n") }
	if err := os.WriteFile(path, []byte(crlf("# 規則\n\n"+oldAgentsSection+"## 後ろの人の節\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ { // 更新のたびに checkout が CRLF に戻す
		if _, err := addAgentsSection(path); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		os.WriteFile(path, []byte(crlf(string(b))), 0o644)
	}
	b, _ := os.ReadFile(path)
	if n := strings.Count(crlf(string(b)), "\r\n\r\n## 後ろの人の節"); n != 0 || strings.Contains(crlf(string(b)), "\r\n\r\n\r\n") {
		t.Fatalf("空行が増えた:\n%q", b)
	}
}

// B1: wiki に手で直された記事が 1 本でもあると Ingest は全部を断るので classify も必ず失敗する。それでも hook は毎回 5 本を頼み、Stop は毎ターン止める。
func TestAudit090ClassifyAskedOnlyWhenPossible(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	src := fmt.Sprintf("%016x", 900)
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "前の版の記事", Lesson: "前の版の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(a.Page.ID); err != nil {
		t.Fatal(err)
	}
	b, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "人が手で直す記事", Lesson: "手で直す教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "wiki", "lessons", "*", b.Page.ID+".md"))
	if len(matches) != 1 {
		t.Fatalf("記事のファイルが見つからない: %v", matches)
	}
	f, _ := os.OpenFile(matches[0], os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("\n人のメモ\n")
	f.Close()
	_, cerr := book.Classify(a.Page.ID, "rule", "作業する時")
	stops := 0
	for i := 0; i < 2; i++ {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "z", "cwd": repo, "prompt": fmt.Sprintf("README を直して %d", i)})
		if s := runHook(t, "stop", map[string]any{"session_id": "z", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(s, "種類と場面を付ける記事") {
			stops++
		}
	}
	if cerr != nil && stops > 0 {
		t.Fatalf("classify は必ず失敗する(%v)のに、Stop が %d ターン止めた", cerr, stops)
	}
}

// C1: 置いた草稿が合流先の今の教訓の言い直し(人が同じ決まりをもう一度言った時)だと、合流しても正解が付かなくなった(0.8.3 は付いた)。
func TestAudit090PlaceMatchRestatedDraftStillLabeled(t *testing.T) {
	now := time.Now()
	page := "変更履歴は英語と日本語を揃える。"
	restated := "英語と日本語を揃える"
	recs := placedRecs(now, page, restated)
	body := "人がもう一度言った(2026-10-20)。"
	d := notebook.Draft{ID: "p1", Title: "変更履歴", Lesson: "変更履歴は英語と日本語を必ず揃える。", Body: &body}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != restated {
		t.Fatalf("言い直しの草稿を合流しても正解が付かない: got %q", got)
	}
}

// D1: classify が記事の変更理由(change_reason)を「種類と場面を付けた(0.9.0)」で上書きし、記事の表示から前の理由が消える。
func TestAudit090ClassifyKeepsChangeReason(t *testing.T) {
	agent, scope, _, src := audit083Setup(t)
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "理由のある記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{ID: a.Page.ID, Sources: []string{src}, Title: "理由のある記事", Lesson: "直した教訓。", AppliesTo: "a", ChangeReason: "人の訂正で範囲を狭めた"}); err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(a.Page.ID); err != nil {
		t.Fatal(err)
	}
	res, err := book.Classify(a.Page.ID, "rule", "作業する時")
	if err != nil {
		t.Fatal(err)
	}
	if res.Page.ChangeReason != "人の訂正で範囲を狭めた" {
		t.Fatalf("変更理由が上書きされた: %q", res.Page.ChangeReason)
	}
}

// E1: 受け箱は同じ教訓の害の評価を回数分並べ、置き換えられた(生きていない)記事の物も出し続ける。
func TestAudit090InboxDedupesAndDropsDeadLessons(t *testing.T) {
	agent, scope, _, src := audit083Setup(t)
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "害のあった記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "置き換えた記事", Lesson: "新しい教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時", Replaces: []string{a.Page.ID}}); err != nil {
		t.Fatal(err)
	}
	obs := []store.MemoryObservation{}
	for i := 0; i < 3; i++ {
		obs = append(obs, store.MemoryObservation{Request: "r", Quote: store.MemoryQuote{ID: a.Page.ID, Lesson: "教訓。"}, Assessment: &store.MemoryAssessment{Result: "harmful"}})
	}
	_, inbox := knowledgeMeta(agent, scope, obs)
	if len(inbox) != 0 {
		t.Fatalf("置き換え済みの記事の害の評価が %d 件、受け箱に並ぶ", len(inbox))
	}
}
