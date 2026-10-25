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
		t.Fatal("届けた記憶と判断への寄与を、振り返りから確認できる")
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
				t.Fatal("その依頼へ実際に届けた引用と未評価の記憶を一緒に返す")
			}
			return items[0].(map[string]any)
		}
	}
	t.Fatal("採点とwikiが済んでも、記憶の未評価を忘れない")
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
		t.Fatal("関連する教訓を実際に届ける")
	}
	key := "lesson:" + draft.ID + ":" + revision
	id := memoryEffectComplete(t, st, scope, "effect")
	// wikiや通常の採点を先に済ませ、記憶の評価だけが残る状態にする。
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
		t.Fatal("後から更新された教訓ではなく、届けた時の版と引用を評価する")
	}
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["helpful"] != float64(0) || stats["pending"] != float64(1) {
		t.Fatal("完了をyesと採点しただけで、記憶の効果を成功にしない")
	}
	result := memoryEffectSubmit(t, h, id, key, "helpful", "未初期化の場所の検査を追加する判断に使った", "合成の隔離フォルダで出力と記録が無いことを検査した")
	if result["complete"] != true {
		t.Fatalf("判断と結果を根拠に記憶の仮評価を保存する: %v", result)
	}
	stats = memoryEffectStats(t, h)
	if stats["helpful"] != float64(1) || stats["pending"] != float64(0) {
		t.Fatal("引用を届けた数と、役立ったという観察の数を分ける")
	}
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || len(labels) != 0 {
		t.Fatal("記憶の仮評価を正解や較正に混ぜない")
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
			t.Fatal("出典や採点日時を入力から偽装させない")
		}
	}
	// 0.9.1: 数(4 件・2 会話)では断らない。中身の足りない評価は項目ごとの誤りで返し、何も保存しない。query では保存しない
	for _, body := range []map[string]any{
		{"operation": "submit", "memory": []map[string]string{{"source_id": id}, {"source_id": id}, {"source_id": id}, {"source_id": id}, {"source_id": id}}},
		{"operation": "submit", "memory": []map[string]string{{"source_id": id}, {"source_id": "0000000000000002"}, {"source_id": "0000000000000003"}}},
	} {
		out := reflectCall(t, h, body)
		if out["complete"] != false || len(out["memory"].([]any)) != len(body["memory"].([]map[string]string)) {
			t.Fatalf("記憶の評価は数で断らず、中身の足りない項目を項目ごとの誤りで返す: %v", out)
		}
		for _, m := range out["memory"].([]any) {
			if m.(map[string]any)["error"] == nil {
				t.Fatalf("key の無い評価は保存しない: %v", out)
			}
		}
	}
	if data, _ := json.Marshal(map[string]any{"operation": "query", "memory": []map[string]string{{"source_id": id}}}); true {
		if code, _, err := callTool(h, "shikii_reflect", data); err != nil || code < 400 {
			t.Fatal("query では保存しない")
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
			t.Fatalf("実際の配送と具体的な根拠に結び付かない評価を拒否する: %v", change)
		}
	}
	other := newServer(serveOpts{storeDir: st.Dir, projectScope: feedbackScope(gitRepo(t))})
	if memoryEffectSubmit(t, other, id, key, "helpful", "別の仕事", "合成テスト")["complete"] != false {
		t.Fatal("別の仕事へ評価を書かせない")
	}
	for n := 0; n < 2; n++ {
		if memoryEffectSubmit(t, h, id, key, "unknown", "", "記憶を使った判断の記録が無いため")["complete"] != true {
			t.Fatal("証拠が足りない時は保留でき、同じ内容を再送できる")
		}
	}
	if memoryEffectSubmit(t, h, id, key, "helpful", "後から成功扱いにする", "別の結果")["complete"] != false {
		t.Fatal("元のAI評価を後から塗り替えない")
	}
	stats := memoryEffectStats(t, h)
	if stats["unknown"] != float64(1) || stats["helpful"] != float64(0) {
		t.Fatal("再送や書き換えで成績を増やさない")
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
		t.Fatal("未整理の原文も上限内で実際に届ける")
	}
	var quotes []map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(ctx, "\n", 2)[1]), &quotes); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: scope})
	// 次の発言がまだ無い依頼を、成功した記憶として評価させない。
	id := loadSession(agent, "raw-effect").TurnID
	if memoryEffectSubmit(t, h, id, quotes[0]["key"].(string), "helpful", "仮の判断", "確認前")["complete"] != false {
		t.Fatal("終了前の依頼に効果を付けない")
	}
	id = memoryEffectComplete(t, st, scope, "raw-effect")
	quote := memoryEffectItem(t, h, id)["quote"].(map[string]any)
	actual, _ := json.Marshal(quotes[0])
	saved, _ := json.Marshal(quote)
	if string(actual) != string(saved) || len(quote["summary"].(string)) >= 600 {
		t.Fatal("保存するのは全原文ではなく、エスケープと切り詰め後に届いた引用そのもの")
	}
}

func TestMemoryEffectCountsOnlyDeliveredQuotesAndKeepsUnfinishedVisible(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "unfinished")
	memoryEffectPrompt(t, scope, "unfinished") // 同じ版は実際には届かない。
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "unrelated", "cwd": scope, "prompt": "決済障害の監視を説明してください。"})
	stats := memoryEffectStats(t, h)
	if stats["delivered"] != float64(1) || stats["pending"] != float64(1) || stats["awaiting_feedback"] != float64(1) {
		t.Fatal("重複抑止と検索に出なかった依頼を配送数へ足さず、未完了も分母に残す")
	}
	other := newServer(serveOpts{storeDir: st.Dir, projectScope: feedbackScope(gitRepo(t))})
	if memoryEffectStats(t, other)["delivered"] != float64(0) {
		t.Fatal("別の仕事に記憶の引用や評価を見せない")
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/wiki", nil))
	var view map[string]any
	if json.Unmarshal(r.Body.Bytes(), &view) != nil || view["memory_effect"] == nil {
		t.Fatal("知識画面にも同じ集計を返す")
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
			t.Fatal("役立たなかった例や不明も同じ単位で保存する")
		}
	}
	stats := memoryEffectStats(t, h)
	for _, name := range []string{"helpful", "unused", "harmful", "unknown"} {
		if stats[name] != float64(1) {
			t.Fatalf("良い例だけを集計しない: %s=%v", name, stats[name])
		}
	}
	if stats["delivered"] != float64(4) || stats["pending"] != float64(0) {
		t.Fatal("届けた件数は全分類と未評価の合計になる")
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
			t.Fatal("一つの依頼に届いた二つの記憶を別々に評価する")
		}
		stats := memoryEffectStats(t, h)
		if stats["delivered"] != float64(2) || stats["helpful"] != float64(1) || stats["unused"] != float64(1) {
			t.Fatal("依頼数ではなく実際に届いた記憶を単位にする")
		}
	})
}
