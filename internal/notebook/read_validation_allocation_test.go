//go:build !race

package notebook

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Race instrumentation has a different allocation budget; semantic checks run in both builds.
func TestReadSnapshotValidationWorkingSet(t *testing.T) {
	b, _, source := readViewFixture(t)
	ids := []string{}
	for i := range 50 {
		d := draft(source, fmt.Sprintf("Bounded validation %d", i))
		body := strings.Repeat("s", 16<<10)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Page.ID)
	}
	view := b.ReadSnapshot()
	for _, id := range ids {
		if _, ok, err := view.CurrentPage(id); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 3 {
		if err := view.ValidateRead(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	if n := (after.TotalAlloc - before.TotalAlloc) / 3; n > 512<<10 {
		t.Fatalf("commit validation must use bounded scratch space, got %d bytes per validation", n)
	}
}
