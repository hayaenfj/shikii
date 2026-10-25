package main

// The recheck of 0.9.0 (added by the auditor).

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

// New-2: just because another page is missing its source (lint reports a problem, but saving is not refused), the hook stops asking for pages to classify. classify passes.
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
		t.Fatalf("source file not found: %v", m)
	}
	os.Remove(m[0])
	text, ids, err := classifyContext(agent, scope)
	if err != nil {
		t.Fatal(err)
	}
	_, cerr := book.Classify(a.Page.ID, "rule", "作業する時")
	if cerr == nil && len(ids) == 0 {
		t.Fatalf("classify passes (%v), yet the hook asks for no pages and says it cannot classify: %s", cerr, text)
	}
}

// Check: classifying the 5 pages the hook asked for, one by one, all pass and Stop does not block. Classifying different pages at the same time is not refused.
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
		t.Fatalf("asks for 5: %v", s.ClassifyAsked)
	}
	for _, id := range s.ClassifyAsked {
		if _, err := book.Classify(id, "fact", "作業する時"); err != nil {
			t.Fatalf("passes when done in order: %s %v", id, err)
		}
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "q", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "need a kind and a scene") {
		t.Fatalf("blocked even though classifying is done: %s", out)
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
			t.Fatalf("classifying different pages at the same time is not refused: %s %v", un[i].ID, err)
		}
	}
	// Lesson, related and change reason do not change
	live, _ := book.Live()
	for _, p := range live {
		if !strings.HasPrefix(p.Lesson, "前の版の教訓") || (p.Title != "前の版の記事 0" && len(p.Related) != 1) {
			t.Fatalf("classify changed the lesson or related: %+v", p)
		}
	}
}
