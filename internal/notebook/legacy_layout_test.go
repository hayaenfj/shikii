package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

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

// 0.9.7: the store keeps the names 0.9.6 gave it (the hash of the path it was first opened at), so a store 0.9.6 wrote is read as it is,
// at its own path and, once its name is recorded, after a move; nothing is moved or renamed.
func TestStoreWrittenBy096KeepsItsNames(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", "v091"), root)
	before, _ := os.ReadDir(filepath.Join(root, "wiki", "lessons"))
	at := Book{Root: root, Scope: "/work/project"}
	if live, err := at.Live(); err != nil || len(live) != 2 {
		t.Fatalf("read at its own path: %+v %v", live, err)
	}
	moved := Book{Root: root, Scope: "/moved/somewhere/project"}
	if live, err := moved.Live(); err != nil || len(live) != 2 {
		t.Fatalf("read after a move: %+v %v", live, err)
	}
	if report, err := moved.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("nothing to fix after a move: %+v %v", report.Issues, err)
	}
	after, _ := os.ReadDir(filepath.Join(root, "wiki", "lessons"))
	if len(before) != len(after) || before[0].Name() != after[0].Name() || after[0].Name() != digest([]byte("/work/project")) {
		t.Fatalf("nothing is moved or renamed: %v -> %v", before, after)
	}
}

// A waiting source captured before 0.9.7 under a path the store does not know cannot be checked: it is not offered for sorting and lint
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

// A store under another name beside this one (written at another path by an older shikii, or copied in by hand) is named until every
// file in it is in this store.
func TestStoreUnderAnotherNameIsNamedUntilCopied(t *testing.T) {
	root := t.TempDir()
	b := Book{Root: root, Scope: "/work/p"}
	if _, err := b.Capture(Observation{Kind: "test", Title: "here", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "records", "wiki-sources", digest([]byte("/work/elsewhere")))
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "0000000000000001.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stores, err := b.OtherPathStores(); err != nil || len(stores) != 1 || !strings.HasSuffix(stores[0], filepath.Base(other)) {
		t.Fatalf("the other store is named: %v %v", stores, err)
	}
	own := filepath.Join(root, "records", "wiki-sources", store.StoreName(root, b.Scope))
	if err := os.WriteFile(filepath.Join(own, "0000000000000001.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stores, err := b.OtherPathStores(); err != nil || len(stores) != 0 {
		t.Fatalf("once copied it is not named: %v %v", stores, err)
	}
}
