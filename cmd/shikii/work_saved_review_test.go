package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkSavedArticleReviewState(t *testing.T) {
	b, page, prepared, ref := batchEditFixture(t)
	old := prepared["maintenance"].([]any)[0].(map[string]any)["ref"]
	saved, err := workCall(t, b, "record", batchEditRequest(prepared["receipt"], ref, "new1"))
	if err != nil || saved["complete"] != true || saved["storage"].(map[string]any)["status"] != "saved" {
		t.Fatal("saving the submitted batch keeps its existing completion contract", saved, err)
	}
	if saved["review_complete"] != false || !strings.Contains(saved["next_action"].(string), "procedure_reviews") {
		t.Error("saving the article must explicitly leave its review pending", saved)
	}
	reviews, _ := saved["procedure_reviews"].([]any)
	if len(reviews) != 1 {
		t.Fatal("return the saved article's review", saved)
	}
	review := reviews[0].(map[string]any)
	live, err := b.LiveWhere(func(p notebook.Page) bool { return p.ID == page.ID })
	if err != nil || len(live) != 1 {
		t.Fatal(live, err)
	}
	got, _ := json.Marshal(review["article"])
	want, _ := json.Marshal(loopObject(t, live[0]))
	if string(got) != string(want) {
		t.Error("the reply must contain the actual saved article, including conditions and sources", string(got), string(want))
	}
	again, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"ref": saved["storage"].(map[string]any)["result_source"]}})
	if err != nil || again["review_complete"] != false {
		t.Error("retrying the saved report cannot settle the article review", again, err)
	}
	stale, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": []any{map[string]any{"ref": old, "reason": "An old reference must remain invalid after the edit."}}})
	if err != nil || stale["complete"] != false || stale["review_complete"] != false {
		t.Error("the original review key must remain stale", stale, err)
	}
	checked, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": []any{map[string]any{"ref": review["ref"], "reason": "Read the returned saved article and checked the retained owner approval and original sources."}}})
	if err != nil || checked["complete"] != true || checked["review_complete"] != true || checked["procedure_reviews"] != nil {
		t.Fatal("the returned article and review ref must finish without another query", checked, err)
	}
}

func TestWorkSavedArticleReviewRequiresCurrentArticle(t *testing.T) {
	for _, state := range []string{"missing", "malformed", "unreadable", "deleted"} {
		t.Run(state, func(t *testing.T) {
			b, page, prepared, ref := batchEditFixture(t)
			saved, err := workCall(t, b, "record", batchEditRequest(prepared["receipt"], ref, "new1"))
			if err != nil || saved["complete"] != true {
				t.Fatal(saved, err)
			}
			paths, err := filepath.Glob(filepath.Join(b.Root, "wiki", "lessons", "*", page.ID+".md"))
			if err != nil || len(paths) != 1 {
				t.Fatal(paths, err)
			}
			switch state {
			case "missing", "unreadable":
				err = os.Remove(paths[0])
				if err == nil && state == "unreadable" {
					err = os.Mkdir(paths[0], 0o700)
				}
			case "malformed":
				err = os.WriteFile(paths[0], []byte("damaged synthetic article"), 0o600)
			case "deleted":
				review := saved["procedure_reviews"].([]any)[0].(map[string]any)
				_, err = b.Manage(notebook.Change{ID: page.ID, Revision: review["revision"].(string), Action: "delete"})
			}
			if err != nil {
				t.Fatal(err)
			}
			again, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"ref": saved["storage"].(map[string]any)["result_source"]}})
			if err != nil {
				t.Fatal(err)
			}
			if state == "deleted" {
				if again["review_complete"] != true {
					t.Fatal("explicitly deleted knowledge needs no current-article review", again)
				}
			} else if again["complete"] != false || again["review_complete"] != false {
				t.Fatal("missing or unreadable saved knowledge must remain incomplete", again)
			} else if next, _ := again["next_action"].(string); !strings.Contains(next, "maintenance error") {
				t.Fatal("an unavailable article needs recovery guidance, not an instruction to read absent text", next)
			}
		})
	}
}

func TestWorkSavedArticleReviewNotHidden(t *testing.T) {
	b, source := loopBook(t)
	pages := []notebook.Page{}
	for _, title := range []string{"Amber", "Beryl", "Cobalt", "Diamond", "Emerald"} {
		pages = append(pages, loopPage(t, b, source, map[string]any{"title": title, "body": "Owner approval is required.", "kind": "howto"}))
	}
	tasks, err := b.Maintenance()
	if err != nil || len(tasks) != 2 {
		t.Fatal(tasks, err)
	}
	var page notebook.Page
	for _, p := range pages {
		if p.ID != tasks[0].PageID && p.ID != tasks[1].PageID {
			page = p
			break
		}
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + page.ID, "request": "Append the reported validation result."})
	if err != nil {
		t.Fatal(err)
	}
	ref := prepared["knowledge"].([]any)[0].(map[string]any)["ref"].(string)
	saved, err := workCall(t, b, "record", batchEditRequest(prepared["receipt"], ref, "new1"))
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	reviews, _ := saved["procedure_reviews"].([]any)
	if len(reviews) != 1 || reviews[0].(map[string]any)["page_id"] != page.ID {
		t.Fatal("unrelated articles must not consume the saved article's review slots", saved)
	}
}

func TestWorkSavedArticleReviewRejectsAnotherWriter(t *testing.T) {
	b, page, prepared, ref := batchEditFixture(t)
	saved, err := workCall(t, b, "record", batchEditRequest(prepared["receipt"], ref, "new1"))
	if err != nil {
		t.Fatal(err)
	}
	review := saved["procedure_reviews"].([]any)[0].(map[string]any)
	_, err = b.Patch(notebook.Patch{ID: page.ID, Revision: review["revision"].(string), Sources: []string{page.Sources[0].ID}, ChangeReason: "Another writer adds a condition.", Edits: []notebook.TextEdit{{Field: "body", Before: "Owner approval is required.", After: "Owner and operator approval are required."}}})
	if err != nil {
		t.Fatal(err)
	}
	checked, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": []any{map[string]any{"ref": review["ref"], "reason": "Only the earlier returned article was read."}}})
	if err != nil || checked["complete"] != false || checked["review_complete"] != false {
		t.Fatal("another writer's update must invalidate the returned review", checked, err)
	}
}

func TestWorkSavedArticleReviewBoundedProgress(t *testing.T) {
	b, source := loopBook(t)
	body := strings.Repeat("Retain the source and its conditions.\n", 300) + "Owner approval is required."
	pages := []notebook.Page{}
	for _, title := range []string{"Amber export", "Cobalt export", "Emerald export"} {
		pages = append(pages, loopPage(t, b, source, map[string]any{"title": title, "body": body, "kind": "howto"}))
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + pages[0].ID, "request": "Append checked validation results to three articles."})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{prepared["knowledge"].([]any)[0].(map[string]any)["ref"].(string)}
	for _, page := range pages[1:] {
		refs = append(refs, interimArticleRef(t, b, prepared["receipt"], page.ID))
	}
	var saved map[string]any
	for _, batch := range [][]string{refs[:2], refs[2:]} {
		notes := []any{}
		for _, ref := range batch {
			notes = append(notes, map[string]any{"target": ref, "sources": []string{"new1"}, "change_reason": "Append the bounded result.", "edits": []any{map[string]any{"field": "body", "before": "Owner approval is required.", "after": "Owner approval is required. Restore was not executed."}}})
		}
		saved, err = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Validation report", "content": "Restore was not executed."}}, "notes": notes})
		if err != nil || saved["complete"] != true {
			t.Fatal(saved, err)
		}
	}
	for _, count := range []int{2, 1} {
		reviews, _ := saved["procedure_reviews"].([]any)
		if len(reviews) != count || saved["review_complete"] != false {
			t.Fatal("return at most two complete articles, and continue with the remaining review", count, len(reviews), saved["review_complete"])
		}
		checked := []any{}
		for _, raw := range reviews {
			review := raw.(map[string]any)
			article := review["article"].(map[string]any)
			if article["body"] != body+" Restore was not executed." || len(article["sources"].([]any)) != 2 {
				t.Fatal("the fixed batch must retain full bodies and both old and new sources", article)
			}
			checked = append(checked, map[string]any{"ref": review["ref"], "reason": "Read the full returned article, its final approval condition and both source references."})
		}
		saved, err = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "maintenance": checked})
		if err != nil || saved["complete"] != true {
			t.Fatal(saved, err)
		}
	}
	if saved["review_complete"] != true || saved["procedure_reviews"] != nil {
		t.Fatal("all three explicit reviews must settle without another prepare or query", saved)
	}
}
