package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Where the store puts a decision when the spec id is not a plain name. The store joins the id into the path as
// it is, so the id decides the folder; only a path that leaves the store is refused. This is what spec.Validate
// has to keep out: the test records where each id lands and holds the one property the store itself guarantees.
func TestRecordsFollowTheSpecIDAndNeverLeaveTheStore(t *testing.T) {
	outer := t.TempDir()
	dir := filepath.Join(outer, "store")
	st := Store{Dir: dir}
	record := func(id string) error {
		return st.AppendRecord(Record{TraceID: NewTraceID(), SpecID: id, Type: "choice", Raw: map[string]float64{"a": 0.6, "b": 0.4}, Top: "a", P: 0.6, Band: "ask", At: time.Now()})
	}

	for _, c := range []struct {
		id, lands string
	}{
		{"plain", "records/plain.jsonl"},
		{"a/b", "records/a/b.jsonl"},
		{"../labels/x", "labels/x.jsonl"},
		{"guards", "records/guards.jsonl"},
	} {
		if err := record(c.id); err != nil {
			t.Fatalf("id %q: %v", c.id, err)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(c.lands))); err != nil {
			t.Fatalf("id %q: expected the record at %s: %v", c.id, c.lands, err)
		}
		t.Logf("spec id %q -> %s", c.id, c.lands)
	}
	ids, err := st.SpecIDs()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SpecIDs() lists %v (the decisions of \"a/b\" and \"guards\" are not listed; \"x\" is listed from labels/)", ids)

	// The one thing the store refuses by itself: a path outside its folder.
	for _, id := range []string{"../../outside", "../../../outside"} {
		err := record(id)
		if err == nil || !strings.Contains(err.Error(), "outside the storage location") {
			t.Fatalf("id %q must be refused as outside the store, got %v", id, err)
		}
	}
	var escaped []string
	_ = filepath.Walk(outer, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasPrefix(path, dir+string(filepath.Separator)) {
			escaped = append(escaped, path)
		}
		return nil
	})
	if len(escaped) != 0 {
		t.Fatalf("files were written outside the store: %v", escaped)
	}
}
