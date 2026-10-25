package notebook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit 0.9.2 (behavior), low 3: a page written by 0.9.1 (with the fixed words of before 0.9.2) and saved again by 0.9.2 with the same content
// (or classified again with the same kind and scene) was refused with "an immutable wiki source or revision has changed": the content is the same,
// so the revision is the same, but the revision file's bytes use the old words. A revision file that is the same page (matchesPage) is accepted.
func TestAuditResaveUnchangedLegacyPage(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	id := seed(t, b, 1)
	body := "Exit 1 means a mismatch."
	d := Draft{Title: "Check invoice totals", Lesson: "Run the check before sending invoices.", AppliesTo: "invoices", Kind: "howto", Scene: "when checking invoices",
		Sources: []string{id}, ChangeReason: "the script was added", Body: &body}
	res, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := b.renderPage(*res.Page, legacyLabels)
	dir, _ := b.directory(false, "pages")
	revisionFile := filepath.Join(dir, "revisions", res.Page.ID+"-"+res.Page.Revision+".md")
	for _, f := range []string{filepath.Join(dir, res.Page.ID+".md"), revisionFile} {
		if err := os.WriteFile(f, old, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d.ID = res.Page.ID
	if _, err := b.Ingest(d); err != nil {
		t.Errorf("saving the same content again: %v", err)
	}
	if _, err := b.Classify(res.Page.ID, "howto", "when checking invoices"); err != nil {
		t.Errorf("classifying again with the same kind and scene: %v", err)
	}
	if kept, _ := os.ReadFile(revisionFile); !bytes.Equal(kept, old) {
		t.Error("the revision file written by the earlier version is not rewritten")
	}
	// a revision file that is not the same page is still refused
	if err := os.WriteFile(revisionFile, append(append([]byte{}, old...), []byte("tampered\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(d); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Errorf("a changed revision file is refused: %v", err)
	}
}
