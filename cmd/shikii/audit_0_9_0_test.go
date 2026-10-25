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
		if s := runHook(t, "stop", map[string]any{"session_id": "z", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(s, "need a kind and a scene") {
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
