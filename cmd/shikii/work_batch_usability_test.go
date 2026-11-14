package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func batchEditFixture(t *testing.T) (notebook.Book, notebook.Page, map[string]any, string) {
	t.Helper()
	b, _ := loopBook(t)
	p := interimAddArticle(t, b, "Archive validation", "Keep the originals. Owner approval is required.")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Append the reported validation result."})
	if err != nil {
		t.Fatal(err)
	}
	return b, p, v, v["knowledge"].([]any)[0].(map[string]any)["ref"].(string)
}

func batchEditRequest(receipt any, target, evidence string) map[string]any {
	return map[string]any{
		"receipt":      receipt,
		"observations": []any{map[string]any{"kind": "test", "title": "Synthetic validation", "content": "Three archives were checked. Restore was not executed."}},
		"result":       map[string]any{"changed": "Appended the reported validation result, preserving approval.", "evidence": []string{"new1", evidence}},
		"notes":        []any{map[string]any{"target": target, "sources": []string{"new1"}, "change_reason": "Retain the bounded observation.", "edits": []any{map[string]any{"field": "body", "before": "Owner approval is required.", "after": "Owner approval is required. Three archives were checked. Restore was not executed."}}}},
	}
}

func TestWorkResultFollowsOnlyItsOwnBatchEdit(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "same batch", true: "another writer"}[external], func(t *testing.T) {
			b, page, p, ref := batchEditFixture(t)
			if external {
				old := workAfterWrite
				t.Cleanup(func() { workAfterWrite = old })
				workAfterWrite = func(kind string) error {
					if kind == "note" {
						live, err := b.LiveWhere(func(p notebook.Page) bool { return p.ID == page.ID })
						if err != nil || len(live) != 1 {
							t.Fatal(live, err)
						}
						_, err = b.Patch(notebook.Patch{ID: page.ID, Revision: live[0].Revision, Sources: []string{page.Sources[0].ID}, ChangeReason: "Another writer changes the condition.", Edits: []notebook.TextEdit{{Field: "body", Before: "Owner approval is required.", After: "A second approval is also required."}}})
						return err
					}
					return nil
				}
			}
			r, err := workCall(t, b, "record", batchEditRequest(p["receipt"], ref, ref))
			if err != nil {
				t.Fatal(err)
			}
			if external {
				if r["complete"] != false {
					t.Fatal("another writer must still invalidate the report", r)
				}
				return
			}
			if r["complete"] != true || r["storage"].(map[string]any)["status"] != "saved" {
				t.Fatal("one batch must save its own edit and result without recovery", r)
			}
			id := r["storage"].(map[string]any)["result_source"]
			again, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": id}})
			if err != nil || again["complete"] != true {
				t.Fatal("the saved result must retain its completed evidence binding", again, err)
			}
			live, _ := b.LiveWhere(func(p notebook.Page) bool { return p.ID == page.ID })
			if len(live) != 1 || !strings.Contains(live[0].Body, "Owner approval is required.") || !strings.Contains(live[0].Body, "Restore was not executed.") {
				t.Fatal("preserve the conditions and observation", live)
			}
		})
	}
}

func TestWorkEditedArticleReturnsCurrentMaintenance(t *testing.T) {
	b, page, p, ref := batchEditFixture(t)
	interimAddArticle(t, b, "Unrelated procedure", "Retain unrelated history.")
	req := batchEditRequest(p["receipt"], ref, "new1")
	r, err := workCall(t, b, "record", req)
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	reviews, _ := r["procedure_reviews"].([]any)
	if len(reviews) != 1 {
		t.Fatal("return the changed page's current review, without unrelated pages", r)
	}
	review := reviews[0].(map[string]any)
	if review["page_id"] != page.ID || review["revision"] == page.Revision {
		t.Fatal("review must name the saved revision", review)
	}
	checked, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "maintenance": []any{map[string]any{"ref": review["ref"], "reason": "Read the saved article and checked its original sources."}}})
	if err != nil || checked["complete"] != true {
		t.Fatal("the returned reference must be directly usable", checked, err)
	}
}

func TestWorkNoteAcceptsUnambiguousPreparedArticleID(t *testing.T) {
	b, page, p, ref := batchEditFixture(t)
	r, err := workCall(t, b, "record", batchEditRequest(p["receipt"], page.ID, "new1"))
	if err != nil || r["complete"] != true {
		t.Fatal("the ID of one prepared version must resolve to that reference", ref, r, err)
	}
	// The same ID must not silently select a newer revision after another read.
	interimArticleRef(t, b, p["receipt"], page.ID)
	req := batchEditRequest(p["receipt"], page.ID, "new1")
	r, err = workCall(t, b, "record", req)
	if err != nil || r["complete"] != false {
		t.Fatal("multiple prepared versions require an explicit reference", r, err)
	}
}
