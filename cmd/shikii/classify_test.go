package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.0: 版を上げる前に作った記事(種類も場面も無い)は、依頼の hook が見つけて 5 本ずつエージェントに種類と場面を付けさせ、
// 終える時に頼んだ記事が埋まっていなければ Stop が一度止める(組み込みの作法の検査 wiki-kinds)。埋まれば止めず、全部埋まれば頼まない。
// どのエージェントでも、付ける操作は shikii wiki classify(MCP なら shikii_wiki の classify)。
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
	if asked != 5 || !strings.Contains(out, "wiki classify") || !strings.Contains(out, "6 本") {
		t.Fatalf("種類と場面の無い記事の数と、5 本の id と付け方を頼む(%d 本): %s", asked, out)
	}
	stop := runHook(t, "stop", map[string]any{"session_id": "k", "cwd": repo, "last_assistant_message": "直した"})
	if !strings.Contains(stop, "block") || !strings.Contains(stop, "種類と場面") {
		t.Fatalf("頼んだ記事が埋まっていなければ一度止める: %s", stop)
	}
	// 頼んだ 5 本を埋めれば、次の依頼で残りの 1 本を頼み、埋めた後の Stop は止めない
	for _, p := range un[:5] {
		if _, err := book.Classify(p.ID, "rule", "作業する時"); err != nil {
			t.Fatal(err)
		}
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "k", "cwd": repo, "prompt": "次は画面を直して"})
	if !strings.Contains(out, un[5].ID) || !strings.Contains(out, "1 本") {
		t.Fatalf("残りの 1 本を頼む: %s", out)
	}
	if _, err := book.Classify(un[5].ID, "howto", "作業する時"); err != nil {
		t.Fatal(err)
	}
	if stop := runHook(t, "stop", map[string]any{"session_id": "k", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(stop, "種類と場面") {
		t.Fatalf("埋めれば止めない: %s", stop)
	}
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "k", "cwd": repo, "prompt": "さらに直して"}); strings.Contains(out, "wiki classify") {
		t.Fatalf("全部埋まれば頼まない: %s", out)
	}
	// status の作法の行に数が出る(頼んだ 2 回、最初から済んでいた 1 回、止めてから済ませた 0 回)
	if out := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(out, "wiki-kinds done 1 of 2 asked") {
		t.Fatalf("status に種類と場面の作法の数が出る:\n%s", out)
	}
}

// 頼んだ 5 本を埋めれば、頼んでいない 6 本目が残っていても、そのターンは止めない(残りは次の依頼で頼む)。
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
	if stop := runHook(t, "stop", map[string]any{"session_id": "m", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(stop, "種類と場面") {
		t.Fatalf("頼んだ記事を埋めれば、頼んでいない記事が残っていても止めない: %s", stop)
	}
}
