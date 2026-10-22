package notebook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRevisionLookupRejectsTamperedHistoryAndUnsafeReferences(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/project"}
	p, err := b.Ingest(draft(seed(t, b, 1), "Versioned article"))
	if err != nil {
		t.Fatal(err)
	}
	query := "lesson:" + p.Page.ID + "@" + p.Page.Revision
	for _, q := range []string{"lesson:../../outside@" + p.Page.Revision, "lesson:" + p.Page.ID + "@../../outside", query + "@extra"} {
		if _, err := b.Query(q); err == nil {
			t.Fatal("版の参照にpathを指定できない")
		}
	}
	dir, _ := b.directory(false, "pages")
	path := filepath.Join(dir, "revisions", p.Page.ID+"-"+p.Page.Revision+".md")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(data, []byte("\nchanged\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Query(query); err == nil {
		t.Fatal("過去の版の本文の改変を検出する")
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(dir, "revisions")
	moved := filepath.Join(dir, "old-revisions")
	if err := os.Rename(history, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, history); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Query(query); err == nil {
		t.Fatal("過去の版の保存先のsymlinkをたどらない")
	}
}
