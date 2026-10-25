package notebook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Also runs through JSON with an old-format reader, to detect unknown fields being silently dropped.
func articleDraft(t *testing.T, d Draft, fields map[string]any) Draft {
	t.Helper()
	data, _ := json.Marshal(d)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	for k, v := range fields {
		obj[k] = v
	}
	data, _ = json.Marshal(obj)
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func articleField(t *testing.T, p *Page, key string) any {
	t.Helper()
	data, _ := json.Marshal(p)
	var obj map[string]any
	_ = json.Unmarshal(data, &obj)
	return obj[key]
}

func TestNotebookArticleKeepsSummaryAndVersions(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	d := draft(seed(t, b, 1), "Installation")
	legacy, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := b.directory(false, "pages")
	legacyPath := filepath.Join(dir, "revisions", legacy.Page.ID+"-"+legacy.Page.Revision+".md")
	before, _ := os.ReadFile(legacyPath)
	body := "## Background\n\n" + strings.Repeat("Model lifecycle evidence. ", 120) + "\n\n## Unresolved\n\nquartzretention remains unverified."
	rich := articleDraft(t, d, map[string]any{"body": body})
	got, err := b.Ingest(rich)
	if err != nil || articleField(t, got.Page, "body") != body || got.Page.Lesson != d.Lesson || got.Page.Previous != legacy.Page.Revision {
		t.Fatalf("keeps the search summary and the long knowledge body separately: %+v %v", got, err)
	}
	v, err := b.Query("quartzretention")
	if err != nil || len(v.Lessons) != 1 || articleField(t, &v.Lessons[0], "body") != body {
		t.Fatalf("finds words that appear only in the body: %+v %v", v, err)
	}
	if recalled, err := b.Recall("quartzretention"); err != nil || len(recalled) != 0 {
		t.Fatal("does not deliver automatically on a chance match in a long body")
	}
	update := draft(d.Sources[0], d.Title)
	update.ID, update.Lesson = got.Page.ID, "Check startup and shutdown."
	next, err := b.Ingest(update)
	if err != nil || articleField(t, next.Page, "body") != body || next.Page.Previous != got.Page.Revision {
		t.Fatal("an old-format update does not erase the body", err)
	}
	cleared, err := b.Ingest(articleDraft(t, update, map[string]any{"body": ""}))
	if err != nil || articleField(t, cleared.Page, "body") != nil {
		t.Fatal("an explicit empty body clears it", err)
	}
	after, _ := os.ReadFile(legacyPath)
	if !bytes.Equal(before, after) {
		t.Fatal("does not rewrite an existing revision")
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("checks old-format and new-format revisions together: %+v %v", report, err)
	}
	for _, body := range []string{strings.Repeat("x", (16<<10)+1), string([]byte{0xff})} {
		update.Body = &body
		if _, err := b.Ingest(update); err == nil {
			t.Fatal("rejects an oversized body and invalid UTF-8")
		}
	}
	empty := ""
	if _, err := b.Ingest(Draft{Sources: d.Sources, Body: &empty}); err == nil {
		t.Fatal("passing only a body does not mark the sources as organized")
	}
}

func TestNotebookRelatedPagesAndExactLookup(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	a, err := b.Ingest(draft(seed(t, b, 1), "Startup"))
	if err != nil {
		t.Fatal(err)
	}
	d := draft(seed(t, b, 2), "Shutdown")
	linked, err := b.Ingest(articleDraft(t, d, map[string]any{"related": []string{a.Page.ID}}))
	if err != nil || articleField(t, linked.Page, "related") == nil {
		t.Fatal("saves relations between knowledge pages", err)
	}
	v, err := b.Query("lesson:" + linked.Page.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != linked.Page.ID {
		t.Fatal("reads a page directly from a related link", err)
	}
	if view, err := b.Query("lesson:ffffffffffffffff"); err != nil || len(view.Lessons) != 0 {
		t.Fatal("does not return another search result in place of a missing page")
	}
	dir, _ := b.directory(false, "pages")
	markdown, _ := os.ReadFile(filepath.Join(dir, linked.Page.ID+".md"))
	if !bytes.Contains(markdown, []byte("]("+a.Page.ID+".md)")) {
		t.Fatal("related pages open from the generated Markdown too")
	}
	d.ID = linked.Page.ID
	d.Lesson = "New shutdown summary."
	kept, err := b.Ingest(d)
	if err != nil || articleField(t, kept.Page, "related") == nil {
		t.Fatal("keeps omitted relations", err)
	}
	// Relations may be mutual. This means something different from a replacement cycle.
	reverse := draft(a.Page.Sources[0].ID, a.Page.Title)
	reverse.ID = a.Page.ID
	if _, err := b.Ingest(articleDraft(t, reverse, map[string]any{"related": []string{linked.Page.ID}})); err != nil {
		t.Fatal("allows mutual related links", err)
	}
	other := Book{Root: b.Root, Scope: "/other"}
	foreign, err := other.Ingest(draft(seed(t, other, 3), "Foreign"))
	if err != nil {
		t.Fatal(err)
	}
	for _, related := range [][]string{{d.ID}, {foreign.Page.ID}, {"ffffffffffffffff"}, {"../outside"}} {
		if _, err := b.Ingest(articleDraft(t, d, map[string]any{"related": related})); err == nil {
			t.Fatalf("relates only other pages of the same project: %v", related)
		}
	}
	duplicates := make([]string, 12)
	for i := range duplicates {
		duplicates[i] = a.Page.ID
	}
	merged, err := b.Ingest(articleDraft(t, d, map[string]any{"related": duplicates}))
	if err != nil || len(merged.Page.Related) != 1 {
		t.Fatal("relations are not refused by count; duplicates are removed", err)
	}
	_ = os.Remove(filepath.Join(dir, a.Page.ID+".md"))
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "missing-related-page") {
		t.Fatalf("detects related pages that disappeared later: %+v %v", report, err)
	}
	if _, err := b.Ingest(articleDraft(t, d, map[string]any{"related": []string{}})); err != nil {
		t.Fatal("an explicit empty array clears broken relations", err)
	}
}

func TestNotebookLegacyRevisionCompatibility(t *testing.T) {
	p := Page{ID: "0000000000000001", Title: "Example", Lesson: "Summary", AppliesTo: "Installation", Status: "hypothesis", Sources: []Source{{ID: "0000000000000002", Hash: "example"}}}
	legacyJSON := `{"id":"0000000000000001","title":"Example","lesson":"Summary","applies_to":"Installation","status":"hypothesis","sources":[{"id":"0000000000000002","hash":"example"}],"revision":""}`
	if revision(p) != digest([]byte(legacyJSON))[:16] {
		t.Fatal("does not change the hash of existing pages without body or relations")
	}
}
