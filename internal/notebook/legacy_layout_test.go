package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// toLegacyLayout turns a store into what versions before 0.9.7 wrote: both areas named by sha256 of the project's path, and the pages'
// source links through that name.
func toLegacyLayout(t *testing.T, root, scope string) {
	t.Helper()
	h := digest([]byte(scope))
	for _, area := range storeAreas {
		if err := os.Rename(filepath.Join(root, area, storeDirName), filepath.Join(root, area, h)); err != nil {
			t.Fatal(err)
		}
	}
	_ = filepath.Walk(filepath.Join(root, "wiki", "lessons", h), func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() && strings.HasSuffix(p, ".md") {
			data, _ := os.ReadFile(p)
			_ = os.WriteFile(p, []byte(strings.ReplaceAll(string(data), "wiki-sources/"+storeDirName+"/", "wiki-sources/"+h+"/")), 0o600)
		}
		return nil
	})
}

// legacySource saves a waiting source record with the id versions before 0.9.7 gave it: the hash of the path it was captured under and its content.
func legacySource(t *testing.T, b Book, capturedAt string, o Observation) string {
	t.Helper()
	dir, err := b.directory(true, "sources")
	if err != nil {
		t.Fatal(err)
	}
	o.ObservedAt = o.ObservedAt.UTC()
	data, _ := json.Marshal(o)
	o.ID = digest(append([]byte(capturedAt+"\n"), data...))[:16]
	data, _ = json.Marshal(o)
	if err := os.WriteFile(filepath.Join(dir, o.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	ids, _ := capturedIDs(dir)
	list, _ := json.Marshal(append(ids, o.ID))
	if err := os.WriteFile(filepath.Join(dir, ".captured.json"), list, 0o600); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// A store written before 0.9.7 is moved to the fixed names the first time it is opened, at its own path or after the folder moved, and
// its pages stay unchanged pages.
func TestStoreWrittenBefore097MovesToTheFixedNames(t *testing.T) {
	for _, openAt := range []string{"/work/old", "/work/new"} {
		root := t.TempDir()
		old := Book{Root: root, Scope: "/work/old"}
		o, err := old.Capture(Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Ingest(Draft{Title: "page citing a source", Lesson: "Check.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
			t.Fatal(err)
		}
		toLegacyLayout(t, root, old.Scope)
		b := Book{Root: root, Scope: openAt}
		if live, err := b.Live(); err != nil || len(live) != 1 {
			t.Fatalf("%s: the page is found: %+v %v", openAt, live, err)
		}
		if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
			t.Fatalf("%s: nothing to fix: %+v %v", openAt, report.Issues, err)
		}
		for _, area := range storeAreas {
			if _, err := os.Lstat(filepath.Join(root, area, digest([]byte(old.Scope)))); !os.IsNotExist(err) {
				t.Fatalf("%s: %s was moved to the fixed name", openAt, area)
			}
		}
	}
}

// A waiting source captured before 0.9.7 under a path the store no longer knows cannot be checked: it is not offered for sorting and lint
// names it. Once the path is remembered (a move found by the hook remembers the previous path), it checks and is offered again.
func TestOldSourceIDsCheckWithTheRememberedPath(t *testing.T) {
	root := t.TempDir()
	b := Book{Root: root, Scope: "/work/new"}
	id := legacySource(t, b, "/work/old", Observation{Kind: "test", Title: "waiting", Content: "x", Reference: "r", Version: "v1", ObservedAt: time.Unix(3, 0)})
	if v, err := b.Query(""); err != nil || len(v.Observations) != 0 {
		t.Fatalf("a source that cannot be checked is not offered, and the wiki stays readable: %+v %v", v.Observations, err)
	}
	if report, err := b.Lint(); err != nil || !hasIssue(report.Issues, "source-from-other-path") {
		t.Fatalf("lint names it: %+v %v", report.Issues, err)
	}
	if err := RememberPath(root, "/work/old"); err != nil {
		t.Fatal(err)
	}
	if v, err := b.Query(""); err != nil || len(v.Observations) != 1 || v.Observations[0].ID != id {
		t.Fatalf("with the previous path remembered it checks and is offered: %+v %v", v.Observations, err)
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("and nothing is left to fix: %+v %v", report.Issues, err)
	}
}

// Two stores written before 0.9.7 under different paths (a rename fixed by hand by copying): the current path's becomes the store, and the
// other is named until every file in it has been copied.
func TestLeftoverStoreFromAnotherPathIsNamedUntilCopied(t *testing.T) {
	root := t.TempDir()
	old := Book{Root: root, Scope: "/work/old"}
	conv := seed(t, old, 1)
	if _, err := old.Ingest(draft(conv, "page citing a conversation")); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Capture(Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	toLegacyLayout(t, root, old.Scope)
	h, n := digest([]byte(old.Scope)), digest([]byte("/work/new"))
	var skipped string
	for _, area := range storeAreas {
		_ = filepath.Walk(filepath.Join(root, area, h), func(p string, info os.FileInfo, err error) error {
			if err != nil || strings.HasPrefix(info.Name(), ".") && !info.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(filepath.Join(root, area, h), p)
			if info.IsDir() {
				return os.MkdirAll(filepath.Join(root, area, n, rel), 0o700)
			}
			if skipped == "" && area == "records/wiki-sources" {
				skipped = p // one file left uncopied
				return nil
			}
			data, _ := os.ReadFile(p)
			return os.WriteFile(filepath.Join(root, area, n, rel), []byte(strings.ReplaceAll(string(data), h, n)), 0o600)
		})
	}
	moved := Book{Root: root, Scope: "/work/new"}
	if stores, err := moved.OtherPathStores(); err != nil || strings.Join(stores, ",") != "records/wiki-sources/"+h {
		t.Fatalf("the old store with a file left uncopied is named: %v %v", stores, err)
	}
	data, _ := os.ReadFile(skipped)
	rel, _ := filepath.Rel(filepath.Join(root, "records/wiki-sources", h), skipped)
	_ = os.WriteFile(filepath.Join(root, "records/wiki-sources", storeDirName, rel), data, 0o600)
	if stores, err := moved.OtherPathStores(); err != nil || len(stores) != 0 {
		t.Fatalf("a fully copied old store is not named: %v %v", stores, err)
	}
}
