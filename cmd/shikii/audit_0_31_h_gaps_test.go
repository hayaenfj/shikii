package main

import (
	"fmt"
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

// M27b: the result of a trial cannot cite a report this work wrote about itself (the plan's check has its test; the result's
// has none).
func TestAuditHGapTrialResultCannotCiteAReport(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	ref := trialSavedRef(t, plan, "trial")
	saved, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The totals match.")},
		"result": map[string]any{"changed": "Checked the totals.", "checked": "Compared the totals.", "evidence": []string{"new1"}}})
	if err != nil || saved["storage"].(map[string]any)["result_source"] == "" {
		t.Fatalf("precondition: the result is saved: %v %.300s", err, auditJSON(saved))
	}
	got, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "It held.", "evidence": []string{"result"}}}})
	if raw := auditJSON(got) + fmt.Sprint(err); !strings.Contains(raw, "wrote about itself") {
		t.Errorf("the result of a trial cites the result of this work as its evidence and is not refused: %.400s", raw)
	}
}
