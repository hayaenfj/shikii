package main

import (
	"bytes"
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
		t.Fatalf("reflection is called through one tool: code=%d body=%s err=%v", code, raw, err)
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
		t.Fatal("returns the raw text for scoring and the wiki once, from the oldest unprocessed conversations")
	}
	for _, raw := range sources {
		source := raw.(map[string]any)
		if source["wiki_pending"] != true || len(source["targets"].([]any)) != 1 {
			t.Fatal("each conversation bundles the unsorted state and the scoring targets")
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
		t.Fatal("saves scores and lessons in one go")
	}
	if reflectCall(t, h, payload)["complete"] != true {
		t.Fatal("resending gives the same result")
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	if len(q["sources"].([]any)) != 0 {
		t.Fatal("processed conversations are not asked for again")
	}
	grades, _ := st.Reviews("done-as-asked")
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || len(grades) != 2 || len(labels) != 0 {
		t.Fatal("even a batch save does not mix provisional scores into truths")
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
			t.Fatal("scoring and sorting work is not cut in after every single conversation")
		}
		if n == 2 && (!strings.Contains(ctx, "shikii_reflect") || !strings.Contains(ctx, "reflect query") || strings.Contains(ctx, "まだ英語が残っています")) {
			t.Fatalf("even when gathering, only the guidance and source IDs are passed, not the conversation's raw text: %s", ctx)
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
		t.Fatal("even a single conversation that is a day old prompts sorting at the next request")
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
			t.Fatalf("sources and operations cannot be forged: code=%d err=%v", code, err)
		}
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/reflect", strings.NewReader(`{"operation":"query"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://foreign.invalid")
	h.ServeHTTP(r, req)
	if r.Code != 403 {
		t.Fatal("operations from another site are refused")
	}
	q := reflectCall(t, h, map[string]any{"operation": "submit", "reviews": []map[string]string{{"source_id": id, "trace_id": trace, "truth": "no", "reason": "訂正の原文"}}, "wiki": []map[string]any{{"sources": []string{"ffffffffffffffff"}}}})
	if q["complete"] != false || q["wiki"].([]any)[0].(map[string]any)["error"] == nil {
		t.Fatal("partial failures are not hidden; the successful part is kept and can be resent")
	}
	grades, _ := st.Reviews("done-as-asked")
	if len(grades) != 1 {
		t.Fatal("a successful provisional score stays, independent of the failed wiki item")
	}
	other := newServer(serveOpts{storeDir: st.Dir, projectScope: feedbackScope(gitRepo(t))})
	q = reflectCall(t, other, map[string]any{"operation": "query"})
	if len(q["sources"].([]any)) != 0 {
		t.Fatal("raw text is not returned to another project")
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
		t.Fatalf("delivers only the needed lesson and source, briefly: %s", ctx)
	}
	if ctx := prompt(request); strings.Contains(ctx, draft.Lesson) {
		t.Fatal("the same revision is not passed again in the same session")
	}
	draft.ID, draft.Lesson = first.Page.ID, "モデルの起動と停止を確認する。"
	if _, err := b.Ingest(draft); err != nil {
		t.Fatal(err)
	}
	if ctx := prompt(request); !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("when a lesson is updated, the new revision is delivered")
	}
	if ctx := prompt("やりましょう。お願いします"); strings.Contains(ctx, draft.Lesson) {
		t.Fatal("a short agreement does not pull out unrelated memories")
	}
	if err := updateSession(agent, "memory", func(s *hookSession) {
		for i := range s.Memories {
			s.Memories[i].At = time.Now().Add(-2 * time.Hour)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if ctx := prompt(request); !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("in long work, a needed memory is delivered again after an hour")
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
		t.Fatal("a lesson for a different scene is not passed even when its body happens to share words")
	}
	if strings.Contains(ctx, "まだ英語が残っています") {
		t.Fatal("raw text already sorted in the wiki is not passed again on top")
	}
	ctx = feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "archived", "cwd": repo, "prompt": "README の起動手順を日本語にする " + id}))
	if strings.Contains(ctx, "まだ英語が残っています") {
		t.Fatal("sorted raw text is not quoted again, even for the same request")
	}
}

// 0.9.1: reflection's submit does not limit the number of conversations, items or reviews handled at once (only the input size, 64 KB). Up to 0.9.0 it allowed 2 conversations, 4 provisional scores,
// 2 wiki sortings and 4 memory assessments, and refused by counting even the sources already on an existing page as conversations. Saving carries over the original sources
func TestReflectSubmitHasNoConversationLimit(t *testing.T) {
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
		t.Fatalf("the 3 sources already on the page are not counted; it is saved as a reflection on 1 new conversation: %v", q)
	}
	got, err := book.Query("lesson:" + page.Page.ID)
	if err != nil || len(got.Lessons) != 1 || len(got.Lessons[0].Sources) != 4 || !strings.Contains(got.Lessons[0].Lesson, "コマンドの例") {
		t.Fatalf("saved with 4 sources: %v %+v", err, got.Lessons)
	}
	// 3 new conversations and 3 wiki drafts can also be saved at once
	drafts := []map[string]any{}
	for i, id := range ids[2:5] {
		drafts = append(drafts, map[string]any{"sources": []string{id}, "title": "起動手順の確認 " + string(rune('A'+i)), "lesson": "起動手順の確認 " + string(rune('A'+i)) + " を README に書く。", "applies_to": "README の起動手順を直す時", "kind": "howto", "scene": "README を直す時"})
	}
	q = reflectCall(t, h, map[string]any{"operation": "submit", "wiki": drafts})
	if q["complete"] != true || len(q["wiki"].([]any)) != 3 {
		t.Fatalf("saves 3 new conversations and 3 drafts at once: %v", q)
	}
	// Reviews (maintenance) are not refused for their count either. A review with a stale key returns a per-item error (nothing is saved)
	stale := []map[string]any{}
	for i := 0; i < 3; i++ {
		stale = append(stale, map[string]any{"page_id": page.Page.ID, "revision": "ffffffffffffffff", "key": "stale", "reason": "古い見直し"})
	}
	q = reflectCall(t, h, map[string]any{"operation": "submit", "maintenance": stale})
	if q["complete"] != false || len(q["maintenance"].([]any)) != 3 {
		t.Fatalf("3 reviews are not refused for their count either, and a stale key returns a per-item error: %v", q)
	}
}

// 0.9.1: reflection input is up to 1 MiB (the same as writing a work record). At 64 KB, 1 to 3 large drafts filled it, so even with the count limit removed,
// the size acted as a limit on the number. When over, it says to split the drafts and send them in the next submit
func TestReflectInputUpToOneMiB(t *testing.T) {
	st, scope, h := reviewFixture(t)
	drafts := []map[string]any{}
	for n := 1; n <= 6; n++ {
		id, _ := reviewSeed(t, st, scope, n)
		body := strings.Repeat("起動手順の背景と理由を、確かめた手順とともに書く。", 300) // about 22 KB (cut below so it stays under the body limit of 16 KB)
		body = string([]rune(body)[:5000])
		drafts = append(drafts, map[string]any{"sources": []string{id}, "title": "起動手順の確認 " + string(rune('A'+n)), "lesson": "起動手順の確認 " + string(rune('A'+n)) + " を README に書く。", "body": body, "applies_to": "README の起動手順を直す時", "kind": "howto", "scene": "README を直す時"})
	}
	data, _ := json.Marshal(map[string]any{"operation": "submit", "wiki": drafts})
	if len(data) <= 64<<10 {
		t.Fatalf("this check uses input over 64 KB: %d", len(data))
	}
	q := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": drafts})
	if q["complete"] != true || len(q["wiki"].([]any)) != 6 {
		t.Fatalf("a reflection over 64 KB (under 1 MiB) is also saved: %d bytes %v", len(data), q)
	}
	// Input near 1 MiB (900 KiB) is also accepted at the HTTP endpoint (not refused for size; assessments missing content return per-item errors; re-audit GJ)
	nearly := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": "0000000000000001", "key": "lesson:x:y", "result": "unknown", "decision": "d", "evidence": strings.Repeat("e", 900<<10)}}}
	if q := reflectCall(t, h, nearly); q["complete"] != false || len(q["memory"].([]any)) != 1 {
		t.Fatalf("a 900 KiB reflection is not refused for its size: %v", q)
	}
	// Over 1 MiB it is refused, with a note to split and send (the CLI reads it the same way)
	big := map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{"0000000000000001"}, "body": strings.Repeat("あ", 400<<10)}}}
	raw, _ := json.Marshal(big)
	if _, err := decodeReflect(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "1 MiB") || !strings.Contains(err.Error(), "Split") {
		t.Fatalf("a reflection over 1 MiB is refused with a note to split and send: %d bytes %v", len(raw), err)
	}
}
