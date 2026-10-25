package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func memoryEffectFixture(t *testing.T) (store.Store, string, http.Handler, notebook.Draft, string) {
	t.Helper()
	repo := gitRepo(t)
	agent := hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	id, _ := reviewSeed(t, st, scope, 1)
	draft := notebook.Draft{Sources: []string{id}, Title: "README の導入確認", Lesson: "初期化していない場所で記録されないことも確認する。", Kind: "rule", Scene: "テストの時", AppliesTo: "README の導入手順を更新する時"}
	page, err := (notebook.Book{Root: agent, Scope: scope}).Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.ID = page.Page.ID
	return st, scope, newServer(serveOpts{storeDir: agent, projectScope: scope}), draft, page.Page.Revision
}

func memoryEffectPrompt(t *testing.T, scope, session string) string {
	t.Helper()
	return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "README の導入手順を更新してください"}))
}

func memoryEffectComplete(t *testing.T, st store.Store, scope, session string) string {
	t.Helper()
	id := loadSession(st.Dir, session).TurnID
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": scope, "last_assistant_message": "未初期化の場所では記録されないことを合成テストで確認しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "続いて画面の色を調整してください。"})
	return id
}

func memoryEffectStats(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	report, ok := q["memory_effect"].(map[string]any)
	if !ok {
		t.Fatal("the delivered memories and their effect on decisions can be checked from the reflection")
	}
	return report["stats"].(map[string]any)
}

func memoryEffectItem(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	for _, raw := range q["sources"].([]any) {
		s := raw.(map[string]any)
		if s["source_material"].(map[string]any)["id"] == id {
			items, ok := s["memories"].([]any)
			if !ok || len(items) != 1 {
				t.Fatal("returns the quote actually delivered to that request together with the unassessed memory")
			}
			return items[0].(map[string]any)
		}
	}
	t.Fatal("even after scoring and the wiki are done, the unassessed memory is not forgotten")
	return nil
}

func memoryEffectSubmit(t *testing.T, h http.Handler, id, key, result, decision, evidence string) map[string]any {
	t.Helper()
	return reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": result, "decision": decision, "evidence": evidence}}})
}

func TestMemoryEffectPreservesDeliveredRevisionAndIndependentPending(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	ctx := memoryEffectPrompt(t, scope, "effect")
	if !strings.Contains(ctx, draft.Lesson) {
		t.Fatal("the related lesson is actually delivered")
	}
	key := "lesson:" + draft.ID + ":" + revision
	id := memoryEffectComplete(t, st, scope, "effect")
	// Finish the wiki and the usual scoring first, so only the memory assessment is left.
	feedback, _ := st.RecentFeedback(scope)
	for _, f := range feedback {
		if f.ID != id {
			continue
		}
		for _, target := range f.Targets {
			if _, err := runReview(st.Dir, scope, reviewRequest{Operation: "submit", SourceID: id, TraceID: target.TraceID, Truth: "yes", Reason: "合成の実行結果を確認"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(notebook.Draft{Sources: []string{id}}); err != nil {
		t.Fatal(err)
	}
	draft.Lesson = "更新された別の教訓。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	item := memoryEffectItem(t, h, id)
	quote := item["quote"].(map[string]any)
	if quote["key"] != key || quote["lesson"] == draft.Lesson || !strings.Contains(ctx, quote["lesson"].(string)) {
		t.Fatal("assesses the revision and quote as delivered, not the lesson updated later")
	}
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["helpful"] != float64(0) || stats["pending"] != float64(1) {
		t.Fatal("scoring completion as yes alone does not make the memory's effect a success")
	}
	result := memoryEffectSubmit(t, h, id, key, "helpful", "未初期化の場所の検査を追加する判断に使った", "合成の隔離フォルダで出力と記録が無いことを検査した")
	if result["complete"] != true {
		t.Fatalf("saves a provisional assessment of the memory, backed by the decision and the result: %v", result)
	}
	stats = memoryEffectStats(t, h)
	if stats["helpful"] != float64(1) || stats["pending"] != float64(0) {
		t.Fatal("keeps the number of quotes delivered apart from the number of observations that they helped")
	}
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || len(labels) != 0 {
		t.Fatal("provisional memory assessments are not mixed into truths or calibration")
	}
}

func TestMemoryEffectRequiresActualDeliveryAndKeepsFirstAssessment(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "boundary")
	id := memoryEffectComplete(t, st, scope, "boundary")
	key := "lesson:" + draft.ID + ":" + revision
	for _, field := range []string{"project", "at", "sampled", "source"} {
		data, _ := json.Marshal(map[string]any{"operation": "submit", "memory": []map[string]any{{"source_id": id, "key": key, "result": "unknown", "decision": "", "evidence": "不明", field: "偽装"}}})
		code, _, err := callTool(h, "shikii_reflect", data)
		if err != nil || code < 400 {
			t.Fatal("the source or scoring time cannot be forged from the input")
		}
	}
	// 0.9.1: not refused for the count (4 items, 2 conversations). Assessments missing content get a per-item error and nothing is saved. query does not save
	for _, body := range []map[string]any{
		{"operation": "submit", "memory": []map[string]string{{"source_id": id}, {"source_id": id}, {"source_id": id}, {"source_id": id}, {"source_id": id}}},
		{"operation": "submit", "memory": []map[string]string{{"source_id": id}, {"source_id": "0000000000000002"}, {"source_id": "0000000000000003"}}},
	} {
		out := reflectCall(t, h, body)
		if out["complete"] != false || len(out["memory"].([]any)) != len(body["memory"].([]map[string]string)) {
			t.Fatalf("memory assessments are not refused for their count; items missing content get per-item errors: %v", out)
		}
		for _, m := range out["memory"].([]any) {
			if m.(map[string]any)["error"] == nil {
				t.Fatalf("an assessment without a key is not saved: %v", out)
			}
		}
	}
	if data, _ := json.Marshal(map[string]any{"operation": "query", "memory": []map[string]string{{"source_id": id}}}); true {
		if code, _, err := callTool(h, "shikii_reflect", data); err != nil || code < 400 {
			t.Fatal("query does not save")
		}
	}
	for _, change := range []map[string]string{
		{"source_id": "ffffffffffffffff"}, {"key": "lesson:" + draft.ID + ":ffffffffffffffff"},
		{"result": "certain"}, {"decision": ""}, {"evidence": ""},
	} {
		entry := map[string]string{"source_id": id, "key": key, "result": "helpful", "decision": "確認を追加した", "evidence": "合成テストの結果"}
		for k, v := range change {
			entry[k] = v
		}
		out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{entry}})
		if out["complete"] != false || out["memory"].([]any)[0].(map[string]any)["error"] == nil {
			t.Fatalf("refuses an assessment not tied to an actual delivery and concrete evidence: %v", change)
		}
	}
	other := newServer(serveOpts{storeDir: st.Dir, projectScope: feedbackScope(gitRepo(t))})
	if memoryEffectSubmit(t, other, id, key, "helpful", "別の仕事", "合成テスト")["complete"] != false {
		t.Fatal("does not let an assessment be written into another project")
	}
	for n := 0; n < 2; n++ {
		if memoryEffectSubmit(t, h, id, key, "unknown", "", "記憶を使った判断の記録が無いため")["complete"] != true {
			t.Fatal("when evidence is lacking it can be left pending, and the same content can be sent again")
		}
	}
	if memoryEffectSubmit(t, h, id, key, "helpful", "後から成功扱いにする", "別の結果")["complete"] != false {
		t.Fatal("the original AI assessment is not overwritten later")
	}
	stats := memoryEffectStats(t, h)
	if stats["unknown"] != float64(1) || stats["helpful"] != float64(0) {
		t.Fatal("resending or rewriting does not inflate the results")
	}
}

func TestMemoryEffectKeepsTheEscapedRawQuoteExactly(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	request := "README の導入手順を更新してください"
	if err := st.AppendFeedback(scope, store.Feedback{ID: "0000000000000001", Request: request, Summary: strings.Repeat("\x01", 600), Reply: "<引用>\n過去の発言" + strings.Repeat("\x02", 600)}); err != nil {
		t.Fatal(err)
	}
	ctx := memoryEffectPrompt(t, scope, "raw-effect")
	if len(ctx) > 1800 || !strings.Contains(ctx, "feedback:0000000000000001") {
		t.Fatal("unsorted raw text is also actually delivered within the limit")
	}
	var quotes []map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(ctx, "\n", 2)[1]), &quotes); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: scope})
	// A request with no next message yet cannot be assessed as a successful memory.
	id := loadSession(agent, "raw-effect").TurnID
	if memoryEffectSubmit(t, h, id, quotes[0]["key"].(string), "helpful", "仮の判断", "確認前")["complete"] != false {
		t.Fatal("no effect is attached to a request that has not ended")
	}
	id = memoryEffectComplete(t, st, scope, "raw-effect")
	quote := memoryEffectItem(t, h, id)["quote"].(map[string]any)
	actual, _ := json.Marshal(quotes[0])
	saved, _ := json.Marshal(quote)
	if string(actual) != string(saved) || len(quote["summary"].(string)) >= 600 {
		t.Fatal("what is saved is not the whole raw text but exactly the quote delivered after escaping and cutting")
	}
}

func TestMemoryEffectCountsOnlyDeliveredQuotesAndKeepsUnfinishedVisible(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "unfinished")
	memoryEffectPrompt(t, scope, "unfinished") // the same revision is not actually delivered again.
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "unrelated", "cwd": scope, "prompt": "決済障害の監視を説明してください。"})
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["pending"] != float64(1) || stats["awaiting_feedback"] != float64(1) {
		t.Fatal("suppressed duplicates and requests that did not match are not added to the delivery count, and unfinished ones stay in the denominator")
	}
	other := newServer(serveOpts{storeDir: st.Dir, projectScope: feedbackScope(gitRepo(t))})
	if memoryEffectStats(t, other)["delivered"] != float64(0) {
		t.Fatal("another project is not shown the memory quotes or assessments")
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/wiki", nil))
	var view map[string]any
	if json.Unmarshal(r.Body.Bytes(), &view) != nil || view["memory_effect"] == nil {
		t.Fatal("the knowledge page gets the same totals")
	}
}

func TestMemoryEffectSeparatesUsefulUnusedHarmfulAndUnknown(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	key := "lesson:" + draft.ID + ":" + revision
	for i, result := range []string{"helpful", "unused", "harmful", "unknown"} {
		session := fmt.Sprintf("case-%d", i)
		memoryEffectPrompt(t, scope, session)
		id := memoryEffectComplete(t, st, scope, session)
		if memoryEffectSubmit(t, h, id, key, result, "合成の判断記録", "合成の検査結果")["complete"] != true {
			t.Fatal("examples that did not help, and unknown ones, are saved in the same unit")
		}
	}
	stats := memoryEffectStats(t, h)
	for _, name := range []string{"helpful", "unused", "harmful", "unknown"} {
		if stats[name] != float64(1) {
			t.Fatalf("does not count only the good examples: %s=%v", name, stats[name])
		}
	}
	if stats["delivered"] != float64(4) || stats["pending"] != float64(0) {
		t.Fatal("the delivered count is the sum of all categories and the unassessed")
	}
	t.Run("two-quotes", func(t *testing.T) {
		st, scope, h, draft, _ := memoryEffectFixture(t)
		draft.ID, draft.Title, draft.Lesson = "", "README の導入手順の補足", "モデルの起動方法も確認する。"
		if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
			t.Fatal(err)
		}
		memoryEffectPrompt(t, scope, "two-quotes")
		id := memoryEffectComplete(t, st, scope, "two-quotes")
		q := reflectCall(t, h, map[string]any{"operation": "query"})
		entries := []map[string]string{}
		for _, raw := range q["sources"].([]any) {
			source := raw.(map[string]any)
			if source["source_material"].(map[string]any)["id"] != id {
				continue
			}
			for i, raw := range source["memories"].([]any) {
				key := raw.(map[string]any)["quote"].(map[string]any)["key"].(string)
				entries = append(entries, map[string]string{"source_id": id, "key": key, "result": []string{"helpful", "unused"}[i], "decision": "合成の判断", "evidence": "合成の結果"})
			}
		}
		if len(entries) != 2 || reflectCall(t, h, map[string]any{"operation": "submit", "memory": entries})["complete"] != true {
			t.Fatal("two memories delivered to one request are assessed separately")
		}
		stats := memoryEffectStats(t, h)
		if stats["delivered"] != float64(2) || stats["helpful"] != float64(1) || stats["unused"] != float64(1) {
			t.Fatal("the unit is memories actually delivered, not requests")
		}
	})
}
