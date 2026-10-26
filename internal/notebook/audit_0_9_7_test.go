package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 0.9.7 audit B-4: a page's source links through any hash-named folder were taken as unchanged. Only the names this store had before
// 0.9.7 are accepted; a link edited to another folder is a changed page.
func TestAuditHandEditedLinkToAnyHashIsUnchanged(t *testing.T) {
	root := t.TempDir()
	b := Book{Root: root, Scope: "/work/p"}
	o, err := b.Capture(Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(Draft{Title: "page", Lesson: "Check.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "wiki", "lessons", storeDirName)
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			p := filepath.Join(dir, e.Name())
			data, _ := os.ReadFile(p)
			edited := strings.ReplaceAll(string(data), "wiki-sources/"+storeDirName+"/", "wiki-sources/"+strings.Repeat("e", 64)+"/")
			if edited != string(data) {
				n++
				_ = os.WriteFile(p, []byte(edited), 0o600)
			}
		}
	}
	if n != 1 {
		t.Fatalf("precondition: %d", n)
	}
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "changed-page") {
		t.Fatalf("a link edited by hand is a changed page: %+v %v", report.Issues, err)
	}
}
