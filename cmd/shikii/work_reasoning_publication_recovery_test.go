package main

import (
	"errors"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func interruptedPublication(t *testing.T, patch bool) (string, notebook.Book, map[string]any, string, notebook.Page) {
	t.Helper()
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	note := map[string]any{"title": "Published before checkpoint", "lesson": "Keep the owner condition.", "body": "Published body.", "applies_to": "owner export", "kind": "howto", "scene": "synthetic review", "sources": []string{"new1"}}
	if patch {
		page := interimAddArticle(t, b, "Existing policy", "Original body.")
		ref := interimArticleRef(t, b, p["receipt"], page.ID)
		note = map[string]any{"target": ref, "sources": []string{"new1"}, "change_reason": "Correct the observed owner condition.", "edits": []any{map[string]any{"field": "body", "before": "Original body.", "after": "Published body."}}}
	}
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "note" {
			return errors.New("synthetic note checkpoint interruption")
		}
		return nil
	}
	failed, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Original publication evidence", "content": "The owner condition was checked."}}, "notes": []any{note}})
	workAfterWrite = old
	if e != nil || failed["complete"] != false {
		t.Fatal("published interruption fixture failed")
	}
	pages, e := b.Live()
	if e != nil || len(pages) != 1 {
		t.Fatal("article was not actually published")
	}
	ref := ""
	for key := range loadSession(root, "reasoning").Reasoning.RequiredNotes {
		ref = key
		break
	}
	if ref == "" {
		t.Fatal("required publication missing")
	}
	return root, b, p, ref, pages[0]
}

func TestReasoningPublishedNoteRecoversAfterPrepare(t *testing.T) {
	for _, patch := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "patch"}[patch], func(t *testing.T) {
			root, b, _, required, page := interruptedPublication(t, patch)
			next, e := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "lesson:" + page.ID})
			if e != nil {
				t.Fatal("publication reprepare failed")
			}
			target := "new"
			if patch {
				target = interimArticleRef(t, b, next["receipt"], page.ID)
			}
			recovered, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{map[string]any{"ref": required, "target": target}}})
			if e != nil || recovered["complete"] != true {
				t.Error("same published revision could not finish its checkpoint")
				return
			}
			live, e := b.Live()
			if e != nil || len(live) != 1 || live[0].ID != page.ID || live[0].Revision != page.Revision {
				t.Fatal("publication retry duplicated or rewrote the article")
			}
			if workHash(live[0].Sources) != workHash(page.Sources) {
				t.Fatal("publication retry rebound original sources")
			}
			end := reasoningInput("finish", "current")
			end["result"] = "The original publication and source checkpoint were confirmed."
			saved := saveReasoning(t, b, next["receipt"], end, nil)
			if loopObject(t, saved["reasoning"])["complete"] != true {
				t.Fatal("publication recovery did not finish")
			}
		})
	}
}

func TestReasoningPublishedNoteNeedsCurrentOriginalTarget(t *testing.T) {
	for _, mode := range []string{"changed", "retired", "different-content"} {
		t.Run(mode, func(t *testing.T) {
			root, b, _, required, page := interruptedPublication(t, false)
			sources := []string{page.Sources[0].ID}
			if mode == "changed" {
				_, e := b.Patch(notebook.Patch{ID: page.ID, Revision: page.Revision, Sources: sources, ChangeReason: "A separate synthetic update.", Edits: []notebook.TextEdit{{Field: "body", Before: "Published body.", After: "External body."}}})
				if e != nil {
					t.Fatal("concurrent update fixture failed")
				}
			} else if mode == "retired" {
				_, e := b.Ingest(notebook.Draft{Title: "Replacement policy", Lesson: "Keep the replacement condition.", AppliesTo: "owner export", Kind: "howto", Scene: "synthetic review", Sources: sources, Replaces: []string{page.ID}})
				if e != nil {
					t.Fatal("retirement fixture failed")
				}
			}
			next, e := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "owner export"})
			if e != nil {
				t.Fatal("changed publication reprepare failed")
			}
			note := map[string]any{"ref": required, "target": "new"}
			if mode == "different-content" {
				note = map[string]any{"ref": required, "target": "new", "title": "Unrelated replacement note", "lesson": "A different condition.", "applies_to": "theme", "kind": "fact", "scene": "synthetic review", "sources": []string{"current"}}
			}
			result, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{note}})
			if e == nil && result["complete"] == true {
				t.Fatal("changed or missing publication silently completed")
			}
			if len(loadSession(root, "reasoning").Reasoning.RequiredNotes) != 1 {
				t.Fatal("changed publication obligation was lost")
			}
			if mode == "changed" {
				target := interimArticleRef(t, b, next["receipt"], page.ID)
				corrected := map[string]any{"ref": required, "target": target, "sources": []string{"current"}, "change_reason": "Repair the current original article.", "edits": []any{map[string]any{"field": "body", "before": "External body.", "after": "Repaired body."}}}
				fixed, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{corrected}})
				if e != nil || fixed["complete"] != true {
					t.Fatal("current original target could not be repaired")
				}
			}
		})
	}
}

func TestReasoningUnpublishedNoteRetriesAfterPrepare(t *testing.T) {
	for _, patch := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "patch"}[patch], func(t *testing.T) {
			_, root, b, p := reasoningFixture(t)
			saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
			note := map[string]any{"title": "Not published yet", "lesson": "Keep the checked condition.", "body": "Prepared body.", "applies_to": "owner export", "kind": "howto", "scene": "synthetic review", "sources": []string{"current"}}
			id := ""
			if patch {
				page := interimAddArticle(t, b, "Existing unpublished policy", "Original body.")
				id = page.ID
				note = map[string]any{"target": interimArticleRef(t, b, p["receipt"], id), "sources": []string{"current"}, "change_reason": "Correct the observed condition.", "edits": []any{map[string]any{"field": "body", "before": "Original body.", "after": "Prepared body."}}}
			}
			old := workAfterWrite
			t.Cleanup(func() { workAfterWrite = old })
			workAfterWrite = func(kind string) error {
				if kind == "note-preview" {
					return errors.New("synthetic interruption before publication")
				}
				return nil
			}
			failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{note}})
			workAfterWrite = old
			if err != nil || failed["complete"] != false {
				t.Fatal("unpublished interruption fixture failed")
			}
			pages, err := b.Live()
			if err != nil || !patch && len(pages) != 0 || patch && (len(pages) != 1 || pages[0].Body != "Original body.") {
				t.Fatal("the proposed revision was already published")
			}
			next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "owner export"})
			if err != nil {
				t.Fatal("unpublished preparation failed")
			}
			required := ""
			for ref := range loopObject(t, next["reasoning"])["pending_notes"].(map[string]any) {
				required = ref
				break
			}
			target := "new"
			if patch {
				target = interimArticleRef(t, b, next["receipt"], id)
			}
			recovered, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{map[string]any{"ref": required, "target": target}}})
			if err != nil || recovered["complete"] != true {
				t.Error("an unpublished reservation could not retry its same intent")
				return
			}
			pages, err = b.Live()
			if err != nil || len(pages) != 1 || pages[0].Body != "Prepared body." {
				t.Fatal("the original unpublished intent was not saved")
			}
		})
	}
}

func TestReasoningChangedPublicationRepairsWithSameReceipt(t *testing.T) {
	root, b, p, required, page := interruptedPublication(t, false)
	_, err := b.Patch(notebook.Patch{ID: page.ID, Revision: page.Revision, Sources: []string{page.Sources[0].ID}, ChangeReason: "A separate synthetic change.", Edits: []notebook.TextEdit{{Field: "body", Before: "Published body.", After: "External body."}}})
	if err != nil {
		t.Fatal("changed publication fixture failed")
	}
	target := interimArticleRef(t, b, p["receipt"], page.ID)
	repaired, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"ref": required, "target": target, "sources": []string{"current"}, "change_reason": "Repair the current original article.", "edits": []any{map[string]any{"field": "body", "before": "External body.", "after": "Repaired body."}}}}})
	if err != nil || repaired["complete"] != true {
		t.Fatal("same receipt repair failed")
	}
	end := reasoningInput("finish", "current")
	end["result"] = "The current original article was repaired."
	finished, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": end})
	if err != nil || finished["complete"] != true || loopObject(t, finished["reasoning"])["complete"] != true {
		t.Error("a superseded publication reservation kept the repaired work pending")
	}
	if len(loadSession(root, "reasoning").Reasoning.RequiredNotes) != 0 {
		t.Fatal("required note was not resolved")
	}
}

func TestReasoningPublicationReservationsStayInTheirWork(t *testing.T) {
	note := &workRequiredNote{ExpectedWork: "earlier-work", ExpectedKeys: []string{"same-local-key"}}
	w := &workState{Nonce: "current-work", ActiveNote: "required-a", Reasoning: &reasoningProgress{RequiredNotes: map[string]*workRequiredNote{"required-a": note}}, Expected: map[string]store.KnowledgeReference{"same-local-key": {ID: "article-b", Revision: "revision-b"}}, Pending: map[string]notebook.Draft{}, PendingOrigins: map[string]string{}, PendingKeys: map[string]string{}}
	finishRequiredPublication(w, note)
	if _, exists := w.Expected["same-local-key"]; !exists {
		t.Error("an earlier work retired another work's reservation")
	}
	w.Expected["same-local-key"] = store.KnowledgeReference{ID: "article-b", Revision: "revision-b"}
	w.Expected["own-key"] = store.KnowledgeReference{ID: "article-a", Revision: "revision-a"}
	bindRequiredPublication(w, w.Expected["own-key"], "same-intent", "own-key")
	finishRequiredPublication(w, note)
	if _, exists := w.Expected["same-local-key"]; !exists {
		t.Error("rebinding inherited a foreign work reservation")
	}
	if _, exists := w.Expected["own-key"]; exists {
		t.Error("the current required note did not retire its own reservation")
	}
}
