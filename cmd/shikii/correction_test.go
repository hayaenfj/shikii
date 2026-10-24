package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func correctionSeed(t *testing.T) (store.Store, string, http.Handler, string) {
	t.Helper()
	st, scope, h, draft, _ := memoryEffectFixture(t)
	body := map[string]any{"operation": "submit", "wiki": []map[string]any{{
		"id": draft.ID, "sources": draft.Sources, "title": draft.Title,
		"lesson": draft.Lesson, "applies_to": draft.AppliesTo,
		"correction": map[string]string{"source_id": draft.Sources[0], "quote": "まだ英語が残っています", "action": "変更した案内の全文を日本語で確認する"},
	}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("人の訂正の引用と次に変える行動を教訓へ保存する")
	}
	ctx := memoryEffectPrompt(t, scope, "correction")
	if !strings.Contains(ctx, "まだ英語が残っています") || !strings.Contains(ctx, "変更した案内の全文を日本語で確認する") || len(ctx) > 1800 {
		t.Fatal("次の似た依頼へ訂正と行動を短く届ける")
	}
	id := memoryEffectComplete(t, st, scope, "correction")
	item := memoryEffectItem(t, h, id)
	return st, scope, h, item["quote"].(map[string]any)["key"].(string)
}

func TestCorrectionTracksBehaviorSeparatelyFromHelpfulness(t *testing.T) {
	for _, outcome := range []string{"followed", "repeated", "not_applicable", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			st, scope, h, key := correctionSeed(t)
			// 完了後の次の依頼へ進んでいても、評価対象は届いた時の依頼。
			items, _ := st.MemoryReport(scope)
			id := items.All[0].SourceID
			entry := map[string]string{"source_id": id, "key": key, "result": "unknown", "correction_result": outcome, "decision": "案内全文の確認を行った", "evidence": "合成の変更差分と確認記録を照合した"}
			body := map[string]any{"operation": "submit", "memory": []map[string]string{entry}}
			for range 2 {
				if reflectCall(t, h, body)["complete"] != true {
					t.Fatal("行動の観察を一度だけ保存する")
				}
			}
			q := reflectCall(t, h, map[string]any{"operation": "query"})
			summary := q["memory_effect"].(map[string]any)
			counts := summary["corrections"].(map[string]any)
			if counts["delivered"] != float64(1) || counts[outcome] != float64(1) || counts["pending"] != float64(0) || summary["stats"].(map[string]any)["helpful"] != float64(0) {
				t.Fatal("訂正に沿った観察と役立った観察を混同せず、再送で数を増やさない")
			}
			entry["correction_result"] = "changed"
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("不明な分類や後からの書き換えを拒否する")
			}
			entry["correction_result"] = map[string]string{"followed": "repeated", "repeated": "followed", "unknown": "followed", "not_applicable": "followed"}[outcome]
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("既存の他の項目が同じでも、訂正の評価だけを後から塗り替えない")
			}
			_, labels, _ := st.Counts("done-as-asked")
			if len(labels) != 0 {
				t.Fatal("訂正の観察を較正へ混ぜない")
			}
		})
	}
}

func TestCorrectionRequiresExactHumanReplyAndActualDelivery(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	for _, correction := range []map[string]string{
		{"source_id": draft.Sources[0], "quote": "人が言っていない訂正", "action": "全文を確認する"},
		{"source_id": "ffffffffffffffff", "quote": "まだ英語が残っています", "action": "全文を確認する"},
		{"source_id": draft.Sources[0], "quote": "まだ英語が残っています", "action": ""},
	} {
		data, _ := json.Marshal(map[string]any{"sources": draft.Sources, "title": draft.Title, "lesson": draft.Lesson, "applies_to": draft.AppliesTo, "correction": correction})
		var d notebook.Draft
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
		if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(d); err == nil {
			t.Fatal("訂正は同じ仕事の人の返答の原文と具体的な行動が必要")
		}
	}
	// 通常の教訓を、人の訂正が効いた例として採点させない。
	memoryEffectPrompt(t, scope, "ordinary")
	id := memoryEffectComplete(t, st, scope, "ordinary")
	key := memoryEffectItem(t, h, id)["quote"].(map[string]any)["key"].(string)
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "helpful", "correction_result": "followed", "decision": "確認した", "evidence": "合成の確認結果"}}}
	if reflectCall(t, h, body)["complete"] != false {
		t.Fatal("届けていない訂正へ行動の評価を付けない")
	}
}

func TestCorrectionKeepsDeliveredActionAfterLessonUpdate(t *testing.T) {
	st, scope, h, _ := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	book := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := book.Query("")
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(err)
	}
	page := v.Lessons[0]
	// 訂正を省略した通常の更新でも、既存の訂正と出典を失わない。
	source := page.Sources[0].ID
	updated, err := book.Ingest(notebook.Draft{ID: page.ID, Sources: []string{source}, Title: page.Title, Lesson: "新しい教訓に更新した。", Kind: "rule", Scene: "テストの時", AppliesTo: page.AppliesTo})
	if err != nil || updated.Page.Correction == nil || updated.Page.Correction.Action != page.Correction.Action {
		t.Fatal("通常の更新で訂正を消さない", err)
	}
	data, _ := json.Marshal(map[string]any{"id": page.ID, "sources": []string{source}, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo, "correction": map[string]string{"source_id": source, "quote": page.Correction.Quote, "action": "今後の別の行動"}})
	var d notebook.Draft
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	updated, err = book.Ingest(d)
	if err != nil || updated.Page.Revision == page.Revision {
		t.Fatal("行動の更新も新しい版にする", err)
	}
	item := memoryEffectItem(t, h, id)
	correction := item["quote"].(map[string]any)["correction"].(map[string]any)
	if correction["action"] != page.Correction.Action {
		t.Fatal("後の行動で、当時届けた訂正を説明し直さない")
	}
}

func TestCorrectionPendingAndUnknownDoNotClaimImprovement(t *testing.T) {
	st, scope, h, key := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	counts := q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["pending"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("届いただけで訂正が効いたとしない")
	}
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "unknown", "evidence": "行動の記録が無い"}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("以前の入力形式も不明として受け入れる")
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	counts = q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["unknown"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("省略や沈黙を改善の証拠にしない")
	}
}
