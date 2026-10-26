package notebook

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// An older shikii (0.9.6, e.g. its MCP server still running after the update) keeps writing the wiki under the path's hash after 0.9.7
// moved that folder to the fixed name. The store side merges such leftovers (records/feedback etc.); does the wiki side?
func TestFull1OlderShikiiWritesWikiAfterMigration(t *testing.T) {
	root := t.TempDir()
	scope := "/work/p"
	b := Book{Root: root, Scope: scope}
	// the older shikii's store at the update moment
	o1, err := b.Capture(Observation{Kind: "test", Title: "one", Content: "PASS 1", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(Draft{Title: "first page", Lesson: "Check one.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o1.ID}}); err != nil {
		t.Fatal(err)
	}
	toLegacyLayout(t, root, scope)
	// 0.9.7 opens it: the hash folder becomes the fixed name
	if live, err := b.Live(); err != nil || len(live) != 1 {
		t.Fatalf("precondition: %+v %v", live, err)
	}
	// the older shikii writes a second page into its hash folder (it makes the folder again)
	other := Book{Root: t.TempDir(), Scope: scope}
	o2, err := other.Capture(Observation{Kind: "test", Title: "two", Content: "PASS 2", Reference: "go test", Version: "v1", ObservedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Ingest(Draft{Title: "second page from the older shikii", Lesson: "Check two.", AppliesTo: "b", Kind: "rule", Scene: "when testing", Sources: []string{o2.ID}}); err != nil {
		t.Fatal(err)
	}
	toLegacyLayout(t, other.Root, scope)
	h := digest([]byte(scope))
	for _, area := range storeAreas {
		os.MkdirAll(filepath.Join(root, area), 0o700)
		if out, err := exec.Command("cp", "-R", filepath.Join(other.Root, area, h), filepath.Join(root, area, h)).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	live, err := b.Live()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 2 {
		stores, _ := b.OtherPathStores()
		t.Fatalf("B1 (wiki): the page the older shikii wrote after the migration is not in the wiki: %d page(s); OtherPathStores names it as a previous path's store: %v", len(live), stores)
	}
}
