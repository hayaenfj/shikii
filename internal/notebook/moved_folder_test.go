package notebook

import (
	"path/filepath"
	"testing"
	"time"
)

// 0.9.7: the wiki and its sources live under fixed names inside .shikii, not under a hash of the project's path. Before, moving or renaming
// the project folder emptied the wiki until a person followed eleven manual steps, and each version's audit found another place the steps
// missed. A Book opened at the new path sees everything the old path wrote, with no files touched in between.
func TestWikiNeedsNoFixingAfterTheFolderMoves(t *testing.T) {
	root := t.TempDir()
	old := Book{Root: root, Scope: "/work/old"}
	obs := Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}
	o, err := old.Capture(obs)
	if err != nil {
		t.Fatal(err)
	}
	stray, err := old.Capture(Observation{Kind: "test", Title: "not cited yet", Content: "x", Reference: "r", Version: "v1", ObservedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	d := Draft{Title: "page citing a source", Lesson: "Check before writing.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}
	page, err := old.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}

	moved := Book{Root: root, Scope: "/work/new"}
	if live, err := moved.Live(); err != nil || len(live) != 1 {
		t.Fatalf("the page written at the old path is there after the move: %+v %v", live, err)
	}
	if report, err := moved.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("nothing needs fixing after the move: %+v %v", report.Issues, err)
	}
	if pending, err := moved.PendingObservations(); err != nil || len(pending) != 1 || pending[0].ID != stray.ID {
		t.Fatalf("a source not cited yet is still offered for sorting: %+v %v", pending, err)
	}
	d.ID, d.Lesson = page.Page.ID, "Check before writing, twice."
	if _, err := moved.Ingest(d); err != nil {
		t.Fatalf("the page can be updated with its source: %v", err)
	}
	if _, err := moved.Ingest(Draft{Title: "other", Lesson: "l", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{stray.ID}}); err != nil {
		t.Fatalf("a source captured before the move can be cited: %v", err)
	}
	if again, err := moved.Capture(obs); err != nil || again.ID != o.ID {
		t.Fatalf("the same evidence has the same id wherever the folder is: %s %s %v", again.ID, o.ID, err)
	}
}

// A store written by the real v0.9.1 (testdata/v091, under /work/project) is read as it is at its own path and after a move, and can be updated.
func TestStoreWrittenByV091NeedsNoFixingAfterAMove(t *testing.T) {
	for _, scope := range []string{"/work/project", "/moved/somewhere/project"} {
		root := t.TempDir()
		copyTree(t, filepath.Join("testdata", "v091"), root)
		b := Book{Root: root, Scope: scope}
		if live, err := b.Live(); err != nil || len(live) != 2 {
			t.Fatalf("%s: both pages written by v0.9.1 are found: %+v %v", scope, live, err)
		}
		if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
			t.Fatalf("%s: pages written by v0.9.1 are not changed pages: %+v %v", scope, report.Issues, err)
		}
		if _, err := b.Classify("4961f1812030f9d6", "rule", "when sending invoices"); err != nil {
			t.Fatalf("%s: a page written by v0.9.1 can be updated: %v", scope, err)
		}
		if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
			t.Fatalf("%s: after the update the store is clean: %+v %v", scope, report.Issues, err)
		}
	}
}
