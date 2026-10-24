package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.0: 知識の画面のために /v1/wiki は、記事ごとの最終更新と役立った・害があった回数(lesson_meta)、人に見てほしい例外だけの受け箱
// (inbox: 害があった教訓、繰り返された訂正、有効にする前の止める検査)、種類と場面の無い記事の数を返す。
func TestWikiListCarriesLessonMetaAndInbox(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "inbox")
	id := memoryEffectComplete(t, st, scope, "inbox")
	key := "lesson:" + draft.ID + ":" + revision
	if out := memoryEffectSubmit(t, h, id, key, "harmful", "この教訓に従って古い手順を書いた", "本人が直した"); out["complete"] != true {
		t.Fatalf("害があった評価を保存する: %v", out)
	}
	if err := saveGuard(st.Dir, guard{ID: "draft-one", Kind: guardKindForbiddenWords, Title: "草稿の検査", Words: []string{"x"}}); err != nil { // 草稿(無効)
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/wiki", nil))
	var body struct {
		Meta map[string]struct {
			Updated string `json:"updated"`
			Helpful int    `json:"helpful"`
			Harmful int    `json:"harmful"`
		} `json:"lesson_meta"`
		Inbox []struct {
			Kind   string `json:"kind"`
			Lesson string `json:"lesson_id"`
			Guard  string `json:"guard_id"`
		} `json:"inbox"`
		Unclassified *int `json:"unclassified_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if m := body.Meta[draft.ID]; m.Updated == "" || m.Harmful != 1 {
		t.Fatalf("記事ごとの最終更新と害があった回数: %+v", body.Meta)
	}
	var harmful, guardDraft bool
	for _, it := range body.Inbox {
		harmful = harmful || it.Kind == "harmful" && it.Lesson == draft.ID
		guardDraft = guardDraft || it.Kind == "guard_draft" && it.Guard == "draft-one"
	}
	if !harmful || !guardDraft {
		t.Fatalf("受け箱に害があった教訓と、有効にする前の止める検査が出る: %+v", body.Inbox)
	}
	if body.Unclassified == nil || *body.Unclassified != 0 {
		t.Fatalf("種類と場面の無い記事の数(0 でも返す): %v", body.Unclassified)
	}
}

// 監査 0.9.0 の E1 と M8: 受け箱は生きている記事ごとに 1 件へまとめ、回数を付ける。繰り返された訂正も出る。
func TestInboxGroupsByLessonWithCount(t *testing.T) {
	agent, scope, _, src := audit083Setup(t)
	book := notebook.Book{Root: agent, Scope: scope}
	a, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "害のあった記事", Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: "訂正の記事", Lesson: "訂正の教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
	if err != nil {
		t.Fatal(err)
	}
	obs := []store.MemoryObservation{}
	for i := 0; i < 3; i++ {
		obs = append(obs, store.MemoryObservation{Request: "r", Quote: store.MemoryQuote{ID: a.Page.ID, Lesson: "教訓。"}, Assessment: &store.MemoryAssessment{Result: "harmful"}})
	}
	obs = append(obs, store.MemoryObservation{Request: "r", Quote: store.MemoryQuote{ID: c.Page.ID, Lesson: "訂正の教訓。"}, Assessment: &store.MemoryAssessment{Result: "unused", CorrectionResult: "repeated"}})
	_, inbox := knowledgeMeta(agent, scope, obs)
	var harmful, repeated int
	for _, it := range inbox {
		switch it["kind"] {
		case "harmful":
			harmful++
			if it["lesson_id"] != a.Page.ID || it["count"] != 3 {
				t.Fatalf("害の 3 件は 1 件にまとめて回数 3: %v", it)
			}
		case "repeated":
			repeated++
		}
	}
	if harmful != 1 || repeated != 1 {
		t.Fatalf("害は 1 件、繰り返された訂正も 1 件: %v", inbox)
	}
}
