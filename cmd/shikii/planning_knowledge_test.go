package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestPlanningReadsFullArticleAndPinsEvidenceVersion(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p := planningWikiFixture(t, root, feedbackScope(repo))
	body := "CSV exports retain the existing filter even when the list is empty."
	d := notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson, Kind: "rule", Scene: "テストの時", AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}, Body: &body}
	r, err := book.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	p = *r.Page
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	payload["pages"] = []string{p.ID}
	q, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"], "query": payload["query"], "pages": payload["pages"]})
	if err != nil {
		t.Fatal("必要な本文を選んで判断に渡せる", err)
	}
	key := "article:" + p.ID + ":" + p.Revision
	if !strings.Contains(q["source_material"].(map[string]any)[key].(string), body) {
		t.Fatal("選んだ本文の原文と版を渡す")
	}
	payload["agreed"] = map[string]any{"summary": "Keep filters for empty export results.", "evidence": []map[string]string{{"source": "current_user", "quote": "keep the existing filters"}, {"source": key, "quote": body}}}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatal(err)
	}
	records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	data, _ := json.Marshal(records[0])
	var record map[string]any
	_ = json.Unmarshal(data, &record)
	context, ok := record["knowledge_context"].(map[string]any)
	if !ok || len(context["used"].([]any)) != 1 || len(context["available"].([]any)) != 1 {
		t.Fatal("候補・実際の引用・当時の利用可能な版を別々に残す", string(data))
	}
	stats, err := (store.Store{Dir: root}).KnowledgeReport()
	if err != nil || stats.Prepared != 1 || stats.WithMatches != 1 || stats.WithCitations != 1 || stats.Citations != 1 || stats.ContextBytes < len(body) {
		t.Fatal("検索の候補と引用を効果の評価から分けて数える", stats, err)
	}
	newBody := "CSV exports have a revised behavior."
	d.Body = &newBody
	if _, err := book.Ingest(d); err != nil {
		t.Fatal(err)
	}
	old, err := book.Query("lesson:" + p.ID + "@" + p.Revision)
	if err != nil || len(old.Lessons) != 1 || old.Lessons[0].Body != body {
		t.Fatal("評価で当時の本文へ戻れる。後の知識を混ぜない", old, err)
	}
}

func TestPlanningRejectsArticleChangesDuringDecision(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	model := fakeAgentModel(t, func(string) float64 { close(entered); <-release; return .9 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	p := planningWikiFixture(t, root, feedbackScope(repo))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	payload["agreed"] = map[string]any{"summary": "Keep the agreed CSV conditions.", "evidence": []map[string]string{{"source": "current_user", "quote": "agreed CSV columns"}, {"source": "lesson:" + p.ID + ":" + p.Revision, "quote": p.Lesson}}}
	done := make(chan error, 1)
	go func() { _, err := planningCall(t, "submit", payload); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("判定を開始できない")
	}
	d := notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson + " Recheck the delimiter.", Kind: "rule", Scene: "テストの時", AppliesTo: p.AppliesTo, Sources: []string{p.Sources[0].ID}}
	_, editErr := (notebook.Book{Root: root, Scope: feedbackScope(repo)}).Ingest(d)
	close(release)
	err := <-done
	if editErr != nil {
		t.Fatal(editErr)
	}
	if err == nil {
		t.Fatal("判定中に引用した記事の変更を検出する")
	}
	if records, _ := (store.Store{Dir: root}).Records("needs-plan", 0); len(records) != 0 {
		t.Fatal("古い根拠の判定は保存しない")
	}
}

// 0.8.1: 画面の「知識を判断に使った記録」がずっと 0 だった(エージェントが検索語を付けていなかった)。話題を選ぶのはエージェントのまま、
// 検索語を付けずに query したら「まだ探していない」と返し、hook の文も「話題の語を query に付けて探す」手順にする。
func TestPlanningTellsTheAgentWhenTheWikiWasNotSearched(t *testing.T) {
	repo, root, _ := planningFixture(t)
	planningWikiFixture(t, root, feedbackScope(repo))
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if ctx := feedbackContext(t, out); !strings.Contains(ctx, "putting topic words in query") || strings.Contains(ctx, "必要なwikiは") {
		t.Fatalf("hook は wiki を探す手順として頼む: %s", ctx)
	}
	payload := planningPayload(root)
	q, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"]})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(q["context_notice"]), "wiki はまだ探していません") || q["wiki_context"] != nil {
		t.Fatalf("検索語が無ければ探さず、探していないと伝える: %v %v", q["context_notice"], q["wiki_context"])
	}
	q, err = planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": payload["turn_id"], "query": "CSV export columns"})
	if err != nil || q["wiki_context"] == nil || strings.Contains(fmt.Sprint(q["context_notice"]), "まだ探していません") {
		t.Fatalf("検索語を付ければ探す: %v %v", q, err)
	}
}

// 0.8.1: 判定モデルが混雑・停止していても、計画の整理は保存して成功で返す(作法の検査に止められない)。判定は見送る。
func TestPlanningSubmitSavesTheContextEvenWhenTheModelIsDown(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	hookLocalURL = "http://127.0.0.1:1" // 判定モデルに届かない
	res, err := planningCall(t, "submit", planningPayload(root))
	if err != nil {
		t.Fatalf("モデルに届かなくても整理は保存する: %v", err)
	}
	if res["recorded"] == true || !strings.Contains(fmt.Sprint(res["reason"]), "the decision model could not be reached") {
		t.Fatalf("判定は見送ったと言う: %v", res)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}); strings.Contains(out, "計画の整理") {
		t.Fatalf("整理を保存したので作法の検査は止めない: %s", out)
	}
}
