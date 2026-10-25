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
		t.Fatal("save the quote of the person's correction and the action to change next into the lesson")
	}
	ctx := memoryEffectPrompt(t, scope, "correction")
	if !strings.Contains(ctx, "まだ英語が残っています") || !strings.Contains(ctx, "変更した案内の全文を日本語で確認する") || len(ctx) > 1800 {
		t.Fatal("deliver the correction and the action, briefly, to the next similar request")
	}
	id := memoryEffectComplete(t, st, scope, "correction")
	item := memoryEffectItem(t, h, id)
	return st, scope, h, item["quote"].(map[string]any)["key"].(string)
}

func TestCorrectionTracksBehaviorSeparatelyFromHelpfulness(t *testing.T) {
	for _, outcome := range []string{"followed", "repeated", "not_applicable", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			st, scope, h, key := correctionSeed(t)
			// Even after moving on to the next request after completion, what is evaluated is the request at the time of delivery.
			items, _ := st.MemoryReport(scope)
			id := items.All[0].SourceID
			entry := map[string]string{"source_id": id, "key": key, "result": "unknown", "correction_result": outcome, "decision": "案内全文の確認を行った", "evidence": "合成の変更差分と確認記録を照合した"}
			body := map[string]any{"operation": "submit", "memory": []map[string]string{entry}}
			for range 2 {
				if reflectCall(t, h, body)["complete"] != true {
					t.Fatal("save the observation of the action only once")
				}
			}
			q := reflectCall(t, h, map[string]any{"operation": "query"})
			summary := q["memory_effect"].(map[string]any)
			counts := summary["corrections"].(map[string]any)
			if counts["delivered"] != float64(1) || counts[outcome] != float64(1) || counts["pending"] != float64(0) || summary["stats"].(map[string]any)["helpful"] != float64(0) {
				t.Fatal("do not confuse an observation that followed the correction with one that helped, and do not increase the count on resend")
			}
			entry["correction_result"] = "changed"
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("refuse unknown categories and later rewrites")
			}
			entry["correction_result"] = map[string]string{"followed": "repeated", "repeated": "followed", "unknown": "followed", "not_applicable": "followed"}[outcome]
			if reflectCall(t, h, body)["complete"] != false {
				t.Fatal("even if other existing fields are the same, do not repaint only the correction's evaluation later")
			}
			_, labels, _ := st.Counts("done-as-asked")
			if len(labels) != 0 {
				t.Fatal("do not mix observations of corrections into calibration")
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
			t.Fatal("a correction needs the original text of the person's reply in the same work and a concrete action")
		}
	}
	// Do not let an ordinary lesson be scored as an example of a person's correction taking effect.
	memoryEffectPrompt(t, scope, "ordinary")
	id := memoryEffectComplete(t, st, scope, "ordinary")
	key := memoryEffectItem(t, h, id)["quote"].(map[string]any)["key"].(string)
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "helpful", "correction_result": "followed", "decision": "確認した", "evidence": "合成の確認結果"}}}
	if reflectCall(t, h, body)["complete"] != false {
		t.Fatal("do not evaluate an action for a correction that was never delivered")
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
	// Even an ordinary update that omits the correction does not lose the existing correction and source.
	source := page.Sources[0].ID
	updated, err := book.Ingest(notebook.Draft{ID: page.ID, Sources: []string{source}, Title: page.Title, Lesson: "新しい教訓に更新した。", Kind: "rule", Scene: "テストの時", AppliesTo: page.AppliesTo})
	if err != nil || updated.Page.Correction == nil || updated.Page.Correction.Action != page.Correction.Action {
		t.Fatal("an ordinary update does not delete the correction", err)
	}
	data, _ := json.Marshal(map[string]any{"id": page.ID, "sources": []string{source}, "title": page.Title, "lesson": page.Lesson, "applies_to": page.AppliesTo, "correction": map[string]string{"source_id": source, "quote": page.Correction.Quote, "action": "今後の別の行動"}})
	var d notebook.Draft
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	updated, err = book.Ingest(d)
	if err != nil || updated.Page.Revision == page.Revision {
		t.Fatal("an update to the action also makes a new version", err)
	}
	item := memoryEffectItem(t, h, id)
	correction := item["quote"].(map[string]any)["correction"].(map[string]any)
	if correction["action"] != page.Correction.Action {
		t.Fatal("a later action does not re-explain the correction delivered back then")
	}
}

func TestCorrectionPendingAndUnknownDoNotClaimImprovement(t *testing.T) {
	st, scope, h, key := correctionSeed(t)
	r, _ := st.MemoryReport(scope)
	id := r.All[0].SourceID
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	counts := q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["pending"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("being delivered alone does not mean the correction took effect")
	}
	body := map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "unknown", "evidence": "行動の記録が無い"}}}
	if reflectCall(t, h, body)["complete"] != true {
		t.Fatal("the older input format is also accepted as unknown")
	}
	q = reflectCall(t, h, map[string]any{"operation": "query"})
	counts = q["memory_effect"].(map[string]any)["corrections"].(map[string]any)
	if counts["unknown"] != float64(1) || counts["followed"] != float64(0) {
		t.Fatal("do not treat omission or silence as evidence of improvement")
	}
}
