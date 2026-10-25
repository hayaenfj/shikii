package notebook

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit 0.9.2 (translation): tests that tie the English texts to what other code and older stores rely on.

// copyTree copies testdata into a fresh store (the testdata itself is never written).
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// T1: testdata/v091 is a store written by the real v0.9.1 (a page with a correction, a related page, a reason for the change, a body and
// two revisions; built from the v0.9.1 tag with its internal/notebook). main reads its pages as unchanged and can update them.
// TestPagesWrittenWithOldLabelsStayValid renders the old page with legacyLabels itself, so it cannot notice when legacyLabels drifts from 0.9.1.
func TestAuditTranslationStoreWrittenByV091(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", "v091"), root)
	b := Book{Root: root, Scope: "/work/project"}
	const invoice, install = "4961f1812030f9d6", "d9aa97698883d93e"
	report, err := b.Lint()
	if err != nil || len(report.Issues) != 0 {
		t.Fatalf("pages written by v0.9.1 are not changed pages: %+v %v", report.Issues, err)
	}
	live, err := b.Live()
	if err != nil || len(live) != 2 {
		t.Fatalf("both pages written by v0.9.1 are found: %+v %v", live, err)
	}
	for _, p := range live {
		if p.ID == invoice && (p.Correction == nil || len(p.Related) != 1 || p.ChangeReason == "" || p.Body == "") {
			t.Fatalf("the v0.9.1 page keeps its correction, related page, reason and body: %+v", p)
		}
	}
	res, err := b.Classify(invoice, "rule", "when sending invoices")
	if err != nil {
		t.Fatalf("a page written by v0.9.1 can be updated: %v", err)
	}
	dir, _ := b.directory(false, "pages")
	md, _ := os.ReadFile(filepath.Join(dir, invoice+".md"))
	if japanese.Match(md) || !strings.Contains(string(md), "The person's correction: always run the check script") || !strings.Contains(string(md), "## Related") {
		t.Fatalf("the updated page is written with the English labels and keeps its fields: %s", md)
	}
	if res.Page == nil || res.Page.Previous != "25162fe9c6010c36" {
		t.Fatalf("the update follows the v0.9.1 revision: %+v", res.Page)
	}
	if report, err = b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("after the update the store is clean: %+v %v", report.Issues, err)
	}
}

// T3: the guidance for pages without a kind and a scene keeps the command and its arguments (not only the word "classify").
func TestAuditTranslationUnclassifiedHowKeepsTheCommand(t *testing.T) {
	for _, must := range []string{"shikii wiki classify <page ID> <rule|howto|criteria|fact|history> <scene>", "shikii_wiki operation classify with id, kind and scene"} {
		if !strings.Contains(unclassifiedHow, must) {
			t.Errorf("unclassifiedHow lost %q: %s", must, unclassifiedHow)
		}
	}
}
