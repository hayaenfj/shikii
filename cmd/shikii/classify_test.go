package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.0: for pages made before the version bump (no kind or scene), the request hook finds them and has the agent give a kind and a scene 5 at a time;
// if the asked pages are still empty when the turn ends, Stop blocks once (the built-in ritual check wiki-kinds). Once filled it does not block, and when all are filled it stops asking.
// For every agent, filling them is done with shikii wiki classify (on MCP, classify of shikii_wiki).
func TestHookAsksToClassifyLegacyPagesAndStopsUntilDone(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	st := store.Store{Dir: root}
	src := fmt.Sprintf("%016x", 500)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	var ids []string
	for i := 0; i < 6; i++ {
		res, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("前の版の記事 %d", i), Lesson: fmt.Sprintf("前の版の教訓 %d。", i), AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
		if err != nil {
			t.Fatal(err)
		}
		if err := book.RewriteWithoutKindScene(res.Page.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.Page.ID)
	}
	un, _ := book.Unclassified()
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "k", "cwd": repo, "prompt": "README を直して"})
	asked := 0
	for _, p := range un {
		if strings.Contains(out, p.ID) {
			asked++
		}
	}
	if asked != 5 || !strings.Contains(out, "wiki classify") || !strings.Contains(out, "version bump): 6") {
		t.Fatalf("asks with the number of pages without a kind and scene, 5 ids and how to fill them (%d pages): %s", asked, out)
	}
	stop := runHook(t, "stop", map[string]any{"session_id": "k", "cwd": repo, "last_assistant_message": "直した"})
	if !strings.Contains(stop, "block") || !strings.Contains(stop, "need a kind and a scene") {
		t.Fatalf("blocks once if the asked pages are not filled: %s", stop)
	}
	// Once the 5 asked pages are filled, the next request asks for the remaining 1, and the Stop after filling it does not block
	for _, p := range un[:5] {
		if _, err := book.Classify(p.ID, "rule", "作業する時"); err != nil {
			t.Fatal(err)
		}
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "k", "cwd": repo, "prompt": "次は画面を直して"})
	if !strings.Contains(out, un[5].ID) || !strings.Contains(out, "version bump): 1") {
		t.Fatalf("asks for the remaining 1: %s", out)
	}
	if _, err := book.Classify(un[5].ID, "howto", "作業する時"); err != nil {
		t.Fatal(err)
	}
	if stop := runHook(t, "stop", map[string]any{"session_id": "k", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(stop, "need a kind and a scene") {
		t.Fatalf("does not block once filled: %s", stop)
	}
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "k", "cwd": repo, "prompt": "さらに直して"}); strings.Contains(out, "wiki classify") {
		t.Fatalf("does not ask once all are filled: %s", out)
	}
	// The ritual line in status shows the counts (asked 2 times, done from the start 1, done after a block 0)
	if out := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(out, "wiki-kinds done 1 of 2 asked") {
		t.Fatalf("status shows the counts of the kind-and-scene ritual:\n%s", out)
	}
}

// Once the 5 asked pages are filled, that turn is not blocked even if a 6th, not asked for, remains (the rest is asked for in the next request).
func TestClassifyRitualLooksOnlyAtTheAskedPages(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	st := store.Store{Dir: root}
	src := fmt.Sprintf("%016x", 501)
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	for i := 0; i < 6; i++ {
		res, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("前の版の記事 %d", i), Lesson: fmt.Sprintf("前の版の教訓 %d。", i), AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
		if err != nil {
			t.Fatal(err)
		}
		if err := book.RewriteWithoutKindScene(res.Page.ID); err != nil {
			t.Fatal(err)
		}
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": repo, "prompt": "README を直して"})
	un, _ := book.Unclassified()
	for _, p := range un[:5] {
		if _, err := book.Classify(p.ID, "fact", "作業する時"); err != nil {
			t.Fatal(err)
		}
	}
	if stop := runHook(t, "stop", map[string]any{"session_id": "m", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(stop, "need a kind and a scene") {
		t.Fatalf("once the asked pages are filled, it does not block even if pages not asked for remain: %s", stop)
	}
}

// Agents without hooks (Codex before trusting hooks, OpenCode) skimmed past the unclassified list at the end of the query reply (measured in 0.9.0).
// The query reply puts the unclassified guidance first, and the replies to saving (ingest), filling (classify) and saving the reflection (reflect submit)
// also carry the number of remaining unclassified pages, the next 5 and how to fill them. Once none remain, they are left out.
func TestResponsesCarryRemainingUnclassifiedPages(t *testing.T) {
	agent, scope, st, src := audit083Setup(t)
	_ = st
	book := notebook.Book{Root: agent, Scope: scope}
	var ids []string
	for i := 0; i < 2; i++ {
		res, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("前の版 %d", i), Lesson: fmt.Sprintf("前の版の教訓 %d。", i), AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
		if err != nil {
			t.Fatal(err)
		}
		if err := book.RewriteWithoutKindScene(res.Page.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.Page.ID)
	}
	enc := func(v any) map[string]any {
		b, _ := json.Marshal(v)
		var m map[string]any
		json.Unmarshal(b, &m)
		return m
	}
	view, err := runNotebook(agent, scope, notebookRequest{Operation: "query"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view)
	if !strings.HasPrefix(string(raw), `{"unclassified_how":`) {
		t.Fatalf("the query reply starts with the unclassified guidance: %.120s", raw)
	}
	saved, err := runNotebook(agent, scope, notebookRequest{Operation: "ingest", Draft: notebook.Draft{Sources: []string{src}, Title: "新しい決まり", Lesson: "新しい決まりの教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"}})
	if err != nil {
		t.Fatal(err)
	}
	if m := enc(saved); m["unclassified_count"] != float64(2) || m["unclassified_how"] == nil || len(m["unclassified"].([]any)) != 2 {
		t.Fatalf("the save reply carries the remaining unclassified pages: %v", m)
	}
	one, err := runNotebook(agent, scope, notebookRequest{Operation: "classify", ID: ids[0], Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if m := enc(one); m["unclassified_count"] != float64(1) {
		t.Fatalf("the reply after filling carries the remaining count: %v", m)
	}
	sub, err := runReflect(agent, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{{Sources: []string{src}, Title: "もう一つの決まり", Lesson: "もう一つの決まりの教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"}}})
	if err != nil {
		t.Fatal(err)
	}
	if m := enc(sub); m["unclassified_count"] != float64(1) {
		t.Fatalf("the reply to saving the reflection also carries the remaining count: %v", m)
	}
	last, err := runNotebook(agent, scope, notebookRequest{Operation: "classify", ID: ids[1], Kind: "fact", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if m := enc(last); m["unclassified_count"] != nil || m["unclassified"] != nil {
		t.Fatalf("left out once none remain: %v", m)
	}
}

// audit 0.9.0 M5: a note of pages asked for in an earlier turn does not block the current turn's Stop (only counted in the turn that asked).
func TestClassifyRitualIgnoresAsksFromAnotherTurn(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	scope := feedbackScope(repo)
	src := fmt.Sprintf("%016x", 700)
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": repo, "prompt": "README を直して"})
	book := notebook.Book{Root: root, Scope: scope}
	res, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "後から前の版になった記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.RewriteWithoutKindScene(res.Page.ID); err != nil {
		t.Fatal(err)
	}
	if err := updateSession(root, "w", func(s *hookSession) { s.ClassifyAsked, s.ClassifyAskedTurn = []string{res.Page.ID}, "前のターン" }); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "w", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "need a kind and a scene") {
		t.Fatalf("a note from an earlier turn does not block the current turn: %s", out)
	}
}

// The recheck of 0.9.0 (added by the auditor).

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
