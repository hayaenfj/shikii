package store

import (
	"strings"
	"testing"
)

// Behaviour check 0.9.2 re-check (claude-en): the agent assessed a delivered plain lesson (not a correction) with
// correction_result "not_applicable", then "unknown". Both were refused with "behaviour observations can only be attached
// to corrections delivered to this request", which does not name the field, so the agent gave up and the assessment stayed pending.
func TestCorrectionResultOnPlainLessonNamesTheField(t *testing.T) {
	s, d, a := memoryStoreFixture(t)
	if err := s.AppendFeedback(d.Scope, Feedback{ID: d.ID, Request: "synthetic request", Summary: "synthetic reply", Reply: "next request"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	for _, cr := range []string{"not_applicable", "unknown"} {
		a.CorrectionResult, a.Decision = cr, "ran the check script the lesson describes"
		err := s.SaveMemoryAssessment(d.Scope, a)
		if err == nil {
			continue // accepting it (and dropping the field) also clears the assessment
		}
		if !strings.Contains(err.Error(), "correction_result") {
			t.Fatalf("correction_result=%q on a plain lesson was refused without naming the field: %v", cr, err)
		}
	}
}
