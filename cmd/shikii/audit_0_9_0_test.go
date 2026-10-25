package main

// Reproductions from the independent audit of 0.9.0 (added by the auditor; confirmed to fail before the fix).

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

// A1: when AGENTS.md is a link to another file (e.g. a symbolic link to CLAUDE.md), replacing turns the link into a plain file and the target stays old.
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
		t.Fatalf("the link was replaced by a plain file (still a link=%v), the target has the new section=%v", info.Mode()&os.ModeSymlink != 0, strings.Contains(string(got), "wiki classify"))
	}
}

// A2: when the person's text mentions the marker before the section (3 markers), the text between the person's text and the section disappears.
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
		t.Fatalf("the person's text disappeared / the old section remained:\n%s", got)
	}
}

// A3: a CRLF AGENTS.md (Windows autocrlf) gains one blank line (a line with only \r) on every replacement.
func TestAudit090AgentsCRLFNoStrayLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	crlf := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n") }
	if err := os.WriteFile(path, []byte(crlf("# 規則\n\n"+oldAgentsSection+"## 後ろの人の節\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ { // checkout turns it back to CRLF on every update
		if _, err := addAgentsSection(path); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		os.WriteFile(path, []byte(crlf(string(b))), 0o644)
	}
	b, _ := os.ReadFile(path)
	if n := strings.Count(crlf(string(b)), "\r\n\r\n## 後ろの人の節"); n != 0 || strings.Contains(crlf(string(b)), "\r\n\r\n\r\n") {
		t.Fatalf("blank lines increased:\n%q", b)
	}
}

// B1: if even one page in the wiki was edited by hand, Ingest refuses everything, so classify always fails. Yet the hook asks for 5 pages every time and Stop blocks every turn.
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
		t.Fatalf("page file not found: %v", matches)
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
		t.Fatalf("classify always fails (%v), yet Stop blocked %d turns", cerr, stops)
	}
}

// C1: when the placed draft restates the merge target's current lesson (a person said the same rule again), merging stopped giving a truth (0.8.3 gave one).
func TestAudit090PlaceMatchRestatedDraftStillLabeled(t *testing.T) {
	now := time.Now()
	page := "変更履歴は英語と日本語を揃える。"
	restated := "英語と日本語を揃える"
	recs := placedRecs(now, page, restated)
	body := "人がもう一度言った(2026-10-20)。"
	d := notebook.Draft{ID: "p1", Title: "変更履歴", Lesson: "変更履歴は英語と日本語を必ず揃える。", Body: &body}
	if got := matchPlacedDraft(d, recs, map[string]store.Label{}, map[string]string{page: "p1"}, now); got != restated {
		t.Fatalf("merging a restating draft gives no truth: got %q", got)
	}
}

// D1: classify overwrites the page's change reason (change_reason) with "gave kind and scene (0.9.0)", and the previous reason disappears from the page's display.
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
		t.Fatalf("the change reason was overwritten: %q", res.Page.ChangeReason)
	}
}

// E1: the inbox lists harmful evaluations of the same lesson once per count, and keeps showing those of replaced (no longer live) pages.
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
		t.Fatalf("%d harmful evaluations of a replaced page are listed in the inbox", len(inbox))
	}
}
