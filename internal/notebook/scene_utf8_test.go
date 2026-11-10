package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// A scene given on the command line arrives as raw bytes. One that is not
// UTF-8 was written into the page as a binary value and came back from a reply
// with a replacement character; it is refused like every other text.
func TestSceneThatIsNotUTF8IsRefused(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	saved, err := b.Ingest(draft(seed(t, b, 1), "Scene encoding"))
	if err != nil {
		t.Fatal(err)
	}
	broken := "when a\xffb changes"
	if _, err := b.Classify(saved.Page.ID, "rule", broken); err == nil {
		t.Error("classify saved a scene that is not UTF-8")
	}
	d := draft(seed(t, b, 2), "Another scene")
	d.Scene = broken
	if _, err := b.Ingest(d); err == nil {
		t.Error("ingest saved a scene that is not UTF-8")
	}
	edited, err := b.Ingest(draft(seed(t, b, 3), "Edited scene"))
	if err != nil {
		t.Fatal(err)
	}
	p := *edited.Page
	if _, err := b.Manage(Change{ID: p.ID, Revision: p.Revision, Action: "edit", Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: broken}); err == nil {
		t.Error("an edit saved a scene that is not UTF-8")
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(data) || strings.Contains(string(data), "!!binary") {
			t.Errorf("%s is not plain UTF-8 text", filepath.Base(file))
		}
	}
	// A scene in another script is still accepted.
	japanese := "\u30b3\u30df\u30c3\u30c8\u3059\u308b\u6642"
	classified, err := b.Classify(saved.Page.ID, "rule", japanese)
	if err != nil || classified.Page.Scene != japanese {
		t.Fatalf("a valid scene is refused: %v", err)
	}
}
