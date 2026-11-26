package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// Audit H of 0.31.0 (fourth round): tests for fixed lines that no shipped test guards. Each passes on the candidate and fails on
// the copy with that line taken out (the mutation named in its comment, _audit_h/mutate.py).

// M18: reflectBare keeps a delivered correction's mark (INVARIANT 5: "source_id, key, page and revision, the mark of a correction").
func TestAuditHGapBareKeepsTheCorrection(t *testing.T) {
	s := reflectSource{Memories: []store.MemoryObservation{{SourceID: "s1", Quote: store.MemoryQuote{Key: "lesson:1:1", ID: "1", Revision: "1", Correction: &store.Correction{Quote: strings.Repeat("q", 900), Action: "a"}}}}}
	bare := reflectBare(s)
	if len(bare.Memories) != 1 || bare.Memories[0].Quote.Key != "lesson:1:1" || bare.Memories[0].Quote.Correction == nil {
		t.Errorf("a delivery cut to its key keeps the mark of a correction (correction_result answers it): %+v", bare.Memories)
	}
}

// M32: shikii --help names the three operations of reflect, as shikii reflect --help and the guides do.
func TestAuditHGapUsageNamesReflectPlace(t *testing.T) {
	if entry := helpEntry(t, "shikii reflect "); !strings.Contains(entry, "query | place | submit") {
		t.Errorf("the entry of shikii reflect in shikii --help names place: %s", entry)
	}
}
