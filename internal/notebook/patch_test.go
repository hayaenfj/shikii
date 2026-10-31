package notebook

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPatchRejectsAmbiguityAndKeepsEvidence(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Precise editing")
	body := "duplicate duplicate"
	why := "Keep supported reasons."
	d.Body = &body
	d.Rationale = &why
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	p := *saved.Page
	initial, err := b.Query("lesson:" + p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p = initial.Lessons[0]
	patch := Patch{ID: p.ID, Revision: p.Revision, Sources: d.Sources, ChangeReason: "A checked edit.", Edits: []TextEdit{{Field: "body", Before: "duplicate", After: "one"}}}
	if _, err := b.Patch(patch); err == nil {
		t.Fatal("ambiguous text was replaced")
	}
	for _, edit := range []TextEdit{{Field: "status", Before: "hypothesis", After: "confirmed"}, {Field: "body", Before: "", After: "append"}, {Field: "body", Before: body, After: strings.Repeat("x", (16<<10)+1)}, {Field: "lesson", Before: p.Lesson, After: ""}} {
		patch.Edits = []TextEdit{edit}
		if _, err := b.Patch(patch); err == nil {
			t.Fatal("unsafe edit accepted", edit.Field)
		}
	}
	patch.Edits = []TextEdit{{Field: "body", Before: body, After: "A replacement."}}
	preview, err := b.PreviewPatch(patch)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := b.Query("lesson:" + p.ID)
	if !reflect.DeepEqual(before.Lessons[0], p) {
		t.Fatal("preview wrote the article")
	}
	applied, err := b.Patch(patch)
	if err != nil || !reflect.DeepEqual(preview.Page, applied.Page) {
		t.Fatal("preview and save disagree", preview, applied, err)
	}
	if applied.Page.Rationale != why || !reflect.DeepEqual(applied.Page.Sources, p.Sources) {
		t.Fatal("unrelated evidence was lost")
	}
	patch.Revision = applied.Page.Revision
	_, err = b.Manage(Change{ID: p.ID, Revision: patch.Revision, Action: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Patch(patch); !errors.Is(err, ErrArticleConflict) {
		t.Fatal("a patch revived a deleted page", err)
	}
}
