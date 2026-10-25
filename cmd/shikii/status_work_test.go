package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The person's effort counts "decisions a person answers under the current rules": unsure decisions whose truth comes from the outcome are not counted; samples and held items of the AI's provisional scores are.
func TestCountPersonWork(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour)
	audit := ""
	for audit == "" {
		if id := store.NewTraceID(); store.AuditSample(id) {
			audit = id
		}
	}
	recs := []store.Record{
		{TraceID: "a1", Band: "ask", At: now},
		{TraceID: "a2", Band: "ask", At: old},
		{TraceID: "d1", Band: "discard", At: now},
		{TraceID: audit, Band: "act", IsCalibrated: true, At: now},
		{TraceID: "r1", Band: "discard", At: now},
		{TraceID: "r2", Band: "discard", At: now},
		{TraceID: "r3", Band: "discard", At: now},
	}
	reviews := map[string]store.Review{"r1": {Sampled: true}, "r2": {Truth: "unknown"}, "r3": {Sampled: true}}
	labs := []store.Label{{TraceID: "a1", Source: "ui"}, {TraceID: "a2", Source: "outcome"}, {TraceID: "r3", Source: "outcome"}, {TraceID: "d1", Source: "judge"}}

	w := countPersonWork("needs-plan", false, recs, labs, reviews, now)
	// unsure a1/a2, sample audit, provisional-score sample r1 and held r2 (the outcome answered r3 first)
	if w.Records != 7 || w.Waits != 5 || w.RecentRecords != 6 || w.RecentWaits != 4 || w.ByPerson != 1 || w.ByResult != 2 {
		t.Fatalf("needs-plan: %+v", w)
	}
	w = countPersonWork("prompt-kind", true, recs, labs, reviews, now)
	// Unsure decisions whose truth comes from the outcome, and uncertain discards, do not wait for a person
	if w.Waits != 3 {
		t.Fatalf("prompt-kind: asks must not wait for a person: %+v", w)
	}
	// An uncertain discard of a choice goes to a person (r3 also goes to a person by its band)
	if got := countPersonWork("same-issue", true, recs, labs, reviews, now).Waits; got != 7 {
		t.Fatalf("a choice spec sends unsure discards to a person: %d", got)
	}
	var b strings.Builder
	w.print(&b, "  ")
	if !strings.Contains(b.String(), "3 of 7 decisions wait for a person") || !strings.Contains(b.String(), "by results 2") {
		t.Fatalf("print: %s", b.String())
	}
}
