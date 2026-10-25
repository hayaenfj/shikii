package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.0: for the knowledge page, /v1/wiki returns each page's last update and helpful / harmful counts (lesson_meta), an inbox of only the exceptions a person should look at
// (inbox: harmful lessons, repeated corrections, guards not yet enabled), and the number of pages with no kind or scene.
func TestWikiListCarriesLessonMetaAndInbox(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "inbox")
	id := memoryEffectComplete(t, st, scope, "inbox")
	key := "lesson:" + draft.ID + ":" + revision
	if out := memoryEffectSubmit(t, h, id, key, "harmful", "この教訓に従って古い手順を書いた", "本人が直した"); out["complete"] != true {
		t.Fatalf("save the harmful evaluation: %v", out)
	}
	if err := saveGuard(st.Dir, guard{ID: "draft-one", Kind: guardKindForbiddenWords, Title: "草稿の検査", Words: []string{"x"}}); err != nil { // a draft (disabled)
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
		t.Fatalf("each page's last update and harmful count: %+v", body.Meta)
	}
	var harmful, guardDraft bool
	for _, it := range body.Inbox {
		harmful = harmful || it.Kind == "harmful" && it.Lesson == draft.ID
		guardDraft = guardDraft || it.Kind == "guard_draft" && it.Guard == "draft-one"
	}
	if !harmful || !guardDraft {
		t.Fatalf("the inbox shows the harmful lesson and the guard not yet enabled: %+v", body.Inbox)
	}
	if body.Unclassified == nil || *body.Unclassified != 0 {
		t.Fatalf("number of pages with no kind or scene (returned even when 0): %v", body.Unclassified)
	}
}

// Audit 0.9.0 E1 and M8: the inbox groups items into one per live page, with a count. Repeated corrections show too.
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
				t.Fatalf("the 3 harmful items are grouped into 1 with count 3: %v", it)
			}
		case "repeated":
			repeated++
		}
	}
	if harmful != 1 || repeated != 1 {
		t.Fatalf("1 harmful item and 1 repeated correction: %v", inbox)
	}
}
