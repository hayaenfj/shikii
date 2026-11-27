package notebook

import (
	"strings"
	"testing"
)

// A patch that is refused has to say why, in words the caller can act on. The first version of the refusals said "invalid patch edit 1"
// for a missing field, a field written "text", text that is not UTF-8 and text over 16 KiB alike, and "must match exactly once" for a
// before that matched nowhere and for one that matched many times alike (found when an agent built a patch from the refusal of a body
// save, which said only "before/after edits").

var patchFieldNames = []string{"title", "lesson", "applies_to", "body", "rationale", "kind", "scene"}

// A refusal of an edit is one short sentence: it names the edit, the reason, and what to do, and nothing else.
const patchRefusalMax = 320

func TestPatchRefusalsSayTheReasonTheCountAndTheFieldsThatExist(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Precise editing")
	body := "alpha beta. gamma beta. aaa."
	d.Body = &body
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := b.Query("lesson:" + saved.Page.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := initial.Lessons[0]
	refused := func(t *testing.T, edits ...TextEdit) string {
		t.Helper()
		_, err := b.Patch(Patch{ID: p.ID, Revision: p.Revision, Sources: d.Sources, ChangeReason: "A checked edit.", Edits: edits})
		if err == nil {
			t.Fatalf("the edits %v were accepted", edits)
		}
		if len(err.Error()) > patchRefusalMax {
			t.Errorf("the refusal is %d bytes, over %d: %s", len(err.Error()), patchRefusalMax, err)
		}
		return err.Error()
	}
	ok := TextEdit{Field: "body", Before: "alpha", After: "ALPHA"}
	for _, c := range []struct {
		name  string
		edits []TextEdit
		want  []string // every one of these is in the refusal
		not   []string // and none of these
	}{
		{"an edit without a field", []TextEdit{{Before: "alpha", After: "ALPHA"}}, append([]string{"edit 1", "no field"}, patchFieldNames...), []string{"invalid patch edit"}},
		{"an edit with a field that does not exist", []TextEdit{{Field: "text", Before: "alpha", After: "ALPHA"}}, append([]string{"edit 1", `"text"`}, patchFieldNames...), []string{"invalid patch edit", "no field"}},
		{"the second edit is the one that names no field", []TextEdit{ok, {Before: "gamma", After: "GAMMA"}}, append([]string{"edit 2", "no field"}, patchFieldNames...), []string{"edit 1"}},
		{"a before that is nowhere in the field", []TextEdit{{Field: "body", Before: "delta", After: "DELTA"}}, []string{"edit 1", "0 times", "body", "shikii wiki query lesson:" + p.ID + " "}, []string{"exactly once", "lesson:<id>"}},
		{"a before that is in the field twice", []TextEdit{{Field: "body", Before: "beta", After: "BETA"}}, []string{"edit 1", "2 times", "body", "once"}, []string{"0 times", "shikii wiki query"}},
		{"a before whose matches overlap", []TextEdit{{Field: "body", Before: "aa", After: "AA"}}, []string{"edit 1", "2 times"}, nil},
		{"a before that is in the lesson nowhere", []TextEdit{{Field: "lesson", Before: "delta", After: "DELTA"}}, []string{"0 times", "lesson"}, []string{"body"}},
		{"the second edit looks for what the first one removed", []TextEdit{{Field: "body", Before: "alpha", After: "omega"}, {Field: "body", Before: "alpha", After: "ALPHA"}}, []string{"edit 2", "0 times", "earlier edits"}, []string{"edit 1"}},
		{"a before that is not UTF-8", []TextEdit{{Field: "body", Before: "alpha\xff", After: "ALPHA"}}, []string{"edit 1", "before", "UTF-8"}, []string{"invalid patch edit"}},
		{"an after that is not UTF-8", []TextEdit{{Field: "body", Before: "alpha", After: "ALPHA\xff"}}, []string{"edit 1", "after", "UTF-8"}, []string{"invalid patch edit", "before is not"}},
		{"a before over 16 KiB", []TextEdit{{Field: "body", Before: strings.Repeat("x", 16<<10+1), After: "y"}}, []string{"edit 1", "before", "16384", "16385"}, []string{"invalid patch edit", "UTF-8"}},
		{"an after over 16 KiB", []TextEdit{{Field: "body", Before: "alpha", After: strings.Repeat("x", 16<<10+7)}}, []string{"edit 1", "after", "16384", "16391"}, []string{"invalid patch edit", "UTF-8", "before is"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := refused(t, c.edits...)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("the refusal lacks %q: %s", w, got)
				}
			}
			for _, w := range c.not {
				if strings.Contains(got, w) {
					t.Errorf("the refusal must not say %q: %s", w, got)
				}
			}
		})
	}
	// negative: the same patch with an edit that is right is saved, so the refusals above are about the edit and not about the setup
	if _, err := b.Patch(Patch{ID: p.ID, Revision: p.Revision, Sources: d.Sources, ChangeReason: "A checked edit.", Edits: []TextEdit{ok}}); err != nil {
		t.Fatalf("the edit that is right is refused: %v", err)
	}
}

// The way on of a refused body save names the patch and what an edit of it holds: the field (an edit without field is refused), then
// before and after. The text said only "before/after edits", so a patch built from it had no field.
const patchEditShape = `"edits":[{"field":"body","before":`

func TestTheBodyLossRefusalsShowTheShapeOfAPatchEdit(t *testing.T) {
	for name, c := range bodyRefusalCases(t) {
		text := c.err.Error()
		patch, shape, ending := strings.Index(text, "shikii wiki patch"), strings.Index(text, patchEditShape), strings.Index(text, "unique ending")
		if patch < 0 || shape < patch || shape > ending {
			t.Errorf("%s: the refusal must show %s between naming the patch and the unique ending (patch %d, shape %d, unique ending %d): %s", name, patchEditShape, patch, shape, ending, text)
		}
	}
}
