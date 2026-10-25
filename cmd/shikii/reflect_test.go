package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func reflectCall(t *testing.T, h http.Handler, body map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(body)
	code, raw, err := callTool(h, "shikii_reflect", data)
	if err != nil || code != 200 {
		t.Fatalf("振り返りを一つの道具から呼ぶ: code=%d body=%s err=%v", code, raw, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReflectBatchRoundTrip(t *testing.T) {
	st, scope, h := reviewFixture(t)
	ids, traces := []string{}, []string{}
	for n := 1; n <= 2; n++ {
		id, trace := reviewSeed(t, st, scope, n)
		ids, traces = append(ids, id), append(traces, trace)
	}
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	sources := q["sources"].([]any)
	if len(sources) != 2 || sources[0].(map[string]any)["source_material"].(map[string]any)["id"] != ids[0] {
		t.Fatal("古い未処理会話から、採点とwikiの原文を一度だけ返す")
	}
	for _, raw := range sources {
		source := raw.(map[string]any)
		if source["wiki_pending"] != true || len(source["targets"].([]any)) != 1 {
			t.Fatal("各会話に未整理と採点対象をまとめる")
		}
	}
	payload := map[string]any{
		"reviews": []map[string]string{
			{"source_id": ids[0], "trace_id": traces[0], "truth": "no", "reason": "利用者が英語の残存を明示した"},
			{"source_id": ids[1], "trace_id": traces[1], "truth": "unknown", "reason": "結果を確認できない"},
		},
		"wiki": []map[string]any{{"sources": ids, "title": "READMEの言語", "lesson": "指定された言語で全体を確認する", "kind": "rule", "scene": "テストの時", "applies_to": "README の日本語化"}},
	}
	payload["operation"] = "submit"
	data, _ := json.Marshal(payload)
	old := stdin
	defer func() { stdin = old }()
	stdin = strings.NewReader(string(data))
	out := capture(t, func() error { return cmdReflect([]string{"submit"}) })
	var receipt map[string]any
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["complete"] != true {
		t.Fatal("採点と教訓を一回で保存する")
	}
	if reflectCall(t, h, payload)["complete"] != true {
		t.Fatal("再送しても同じ結果になる")
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	if len(q["sources"].([]any)) != 0 {
		t.Fatal("処理済みの会話を再度依頼しない")
	}
	grades, _ := st.Reviews("done-as-asked")
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || len(grades) != 2 || len(labels) != 0 {
		t.Fatal("一括保存でも仮採点を正解に混ぜない")
	}
}

func TestReflectHookWaitsAndOffersOnlyIDs(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	request := "合成の記録を調べてください。今回の内容を確認します。"
	for n := 1; n <= 2; n++ {
		reviewSeed(t, st, scope, n)
		ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": fmt.Sprint(n), "cwd": repo, "prompt": request}))
		if n == 1 && (strings.Contains(ctx, "submit") || strings.Contains(ctx, "ingest")) {
			t.Fatal("一会話ごとに採点と整理の作業を割り込ませない")
		}
		if n == 2 && (!strings.Contains(ctx, "shikii_reflect") || !strings.Contains(ctx, "reflect query") || strings.Contains(ctx, "まだ英語が残っています") || len(ctx) > 1800) {
			t.Fatalf("まとめる時も短い案内と出典IDだけを渡す: %s", ctx)
		}
	}
}

func TestReflectSingleOldSourceIsNotForgotten(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	if err := st.AppendFeedback(scope, store.Feedback{ID: "0000000000000001", Request: "database backup", Summary: "done", Reply: "check tomorrow", At: time.Now().Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "old", "cwd": repo, "prompt": "新しい画面について説明してください"}))
	if !strings.Contains(ctx, "reflect query") || !strings.Contains(ctx, "0000000000000001") {
		t.Fatal("一件でも一日経った会話は次の依頼で整理を促す")
	}
}

func TestReflectReportsPartialFailuresAndProtectsBoundary(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	for _, body := range []string{
		`{"operation":"query","project":"/other"}`,
		`{"operation":"query"} {"operation":"submit"}`,
		`{"operation":"submit","reviews":[{"source_id":"` + id + `","trace_id":"` + trace + `","truth":"yes","reason":"x","source":"audit"}]}`,
		`{"operation":"submit","wiki":[{"sources":["` + id + `"],"status":"confirmed"}]}`,
	} {
		code, _, err := callTool(h, "shikii_reflect", json.RawMessage(body))
		if err != nil || code < 400 {
			t.Fatalf("出典と操作を偽装できない: code=%d err=%v", code, err)
		}
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/reflect", strings.NewReader(`{"operation":"query"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://foreign.invalid")
	h.ServeHTTP(r, req)
	if r.Code != 403 {
		t.Fatal("別サイトからの操作を拒否する")
	}
	q := reflectCall(t, h, map[string]any{"operation": "submit", "reviews": []map[string]string{{"source_id": id, "trace_id": trace, "truth": "no", "reason": "訂正の原文"}}, "wiki": []map[string]any{{"sources": []string{"ffffffffffffffff"}}}})
	if q["complete"] != false || q["wiki"].([]any)[0].(map[string]any)["error"] == nil {
		t.Fatal("一部の失敗を隠さず、成功分を保持して再送可能にする")
	}
	grades, _ := st.Reviews("done-as-asked")
	if len(grades) != 1 {
		t.Fatal("成功した仮採点は失敗したwikiと独立して残す")
	}
	other := newServer(serveOpts{storeDir: st.Dir, projectScope: feedbackScope(gitRepo(t))})
	q = reflectCall(t, other, map[string]any{"operation": "query"})
	if len(q["sources"].([]any)) != 0 {
		t.Fatal("別のプロジェクトへ原文を返さない")
	}
}

func TestMemoryDeliversRelevantLessonOncePerRevision(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	id, _ := reviewSeed(t, st, scope, 1)
	b := notebook.Book{Root: agent, Scope: scope}
	draft := notebook.Draft{Sources: []string{id}, Title: "README の導入手順", Lesson: "モデルを起動する手順まで確認する。", Kind: "rule", Scene: "テストの時", AppliesTo: "README の導入手順を更新する時"}
	first, err := b.Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	request := "README の導入手順を更新してください"
	prompt := func(text string) string {
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "memory", "cwd": repo, "prompt": text}))
	}
	ctx := prompt(request)
	if !strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, id) || strings.Contains(ctx, "まだ英語が残っています") || strings.Contains(ctx, "ingest") || len(ctx) > 1800 {
		t.Fatalf("必要な教訓と出典だけを短く届ける: %s", ctx)
	}
	if ctx := prompt(request); strings.Contains(ctx, draft.Lesson) {
		t.Fatal("同じセッションに同じ版を繰り返し渡さない")
	}
	draft.ID, draft.Lesson = first.Page.ID, "モデルの起動と停止を確認する。"
	if _, err := b.Ingest(draft); err != nil {
		t.Fatal(err)
	}
	if ctx := prompt(request); !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("教訓が更新されたら新しい版を届ける")
	}
	if ctx := prompt("やりましょう。お願いします"); strings.Contains(ctx, draft.Lesson) {
		t.Fatal("短い同意から関係のない記憶を取り出さない")
	}
	if err := updateSession(agent, "memory", func(s *hookSession) {
		for i := range s.Memories {
			s.Memories[i].At = time.Now().Add(-2 * time.Hour)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if ctx := prompt(request); !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("長い作業では一時間後に必要な記憶を再び届ける")
	}
}

func TestMemoryMatchesConditionsInsteadOfIncidentalBody(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	id, _ := reviewSeed(t, st, scope, 1)
	b := notebook.Book{Root: agent, Scope: scope}
	_, err := b.Ingest(notebook.Draft{Sources: []string{id}, Title: "決済の監視", Lesson: "README の導入手順を更新する場合とは別の決済障害。", Kind: "rule", Scene: "テストの時", AppliesTo: "決済障害が発生した時"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "conditions", "cwd": repo, "prompt": "README の導入手順を更新してください"}))
	if strings.Contains(ctx, "決済障害") {
		t.Fatal("本文に偶然同じ語があっても、適用する場面が違う教訓は渡さない")
	}
	if strings.Contains(ctx, "まだ英語が残っています") {
		t.Fatal("wikiで整理した原文をもう一度重ねて渡さない")
	}
	ctx = feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "archived", "cwd": repo, "prompt": "README の起動手順を日本語にする " + id}))
	if strings.Contains(ctx, "まだ英語が残っています") {
		t.Fatal("整理済みの原文は、同じ依頼でも再び引用しない")
	}
}

// 0.9.1: 振り返りの「一度に会話 2 件まで」は、今回新しく振り返る会話だけを数える。既存の記事に足す草稿に、記事に元からある出典を
// 書き並べても数えない(保存は元の出典を引き継ぐので、書かなくてもよい)。断る時は、数えた会話と、元の出典が引き継がれることを伝える
func TestReflectLimitCountsOnlyNewConversations(t *testing.T) {
	st, scope, h := reviewFixture(t)
	ids := []string{}
	for n := 1; n <= 5; n++ {
		id, _ := reviewSeed(t, st, scope, n)
		ids = append(ids, id)
	}
	book := notebook.Book{Root: st.Dir, Scope: scope}
	page, err := book.Ingest(notebook.Draft{Sources: ids[:3], Title: "起動手順は日本語で書く", Lesson: "README の起動手順は日本語で書く。", AppliesTo: "README の起動手順を直す時", Kind: "rule", Scene: "README を直す時"})
	if err != nil {
		t.Fatal(err)
	}
	draft := map[string]any{"id": page.Page.ID, "sources": append(append([]string{}, ids[:3]...), ids[3]), "title": "起動手順は日本語で書く", "lesson": "README の起動手順は日本語で書き、コマンドの例も付ける。", "applies_to": "README の起動手順を直す時", "change_reason": "コマンドの例を足した"}
	q := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{draft}})
	if q["complete"] != true {
		t.Fatalf("記事に元からある出典 3 つは数えず、新しい会話 1 つの振り返りとして保存する: %v", q)
	}
	got, err := book.Query("lesson:" + page.Page.ID)
	if err != nil || len(got.Lessons) != 1 || len(got.Lessons[0].Sources) != 4 || !strings.Contains(got.Lessons[0].Lesson, "コマンドの例") {
		t.Fatalf("出典 4 つで保存される: %v %+v", err, got.Lessons)
	}
	// 新しい会話が 3 つなら、今までどおり断る。断りの文に数えた会話と、元の出典は引き継がれることを書く
	data, _ := json.Marshal(map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": ids[2:5]}}})
	_, raw, _ := callTool(h, "shikii_reflect", data)
	msg := string(raw)
	if !strings.Contains(msg, "最大2件") || !strings.Contains(msg, ids[3]) || !strings.Contains(msg, ids[4]) || strings.Contains(msg, ids[0]) || !strings.Contains(msg, "引き継") {
		t.Fatalf("新しい会話 3 つは断り、数えた会話と、元の出典は引き継がれることを伝える: %s", msg)
	}
}
