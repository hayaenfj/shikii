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
		// 0.11: a needs-plan with the agent's organizing waits only when the decision model (p .6: yes) and the agent disagree, even in the ask band
		{TraceID: "pa", Band: "ask", P: .6, Inputs: map[string]string{"agreed": "a", "open": "AI's organizing (not permission to act, not the person's truth): the column order is open"}, At: now},
		{TraceID: "pd", Band: "ask", P: .6, Inputs: map[string]string{"agreed": "a", "open": "unknown (cannot be confirmed from the current conversation)"}, At: now},
	}
	labs := []store.Label{{TraceID: "a1", Source: "ui"}, {TraceID: "a2", Source: "outcome"}, {TraceID: "r3", Source: "outcome"}, {TraceID: "d1", Source: "judge"}, {TraceID: "pa", Source: "agreed"}}

	w := countPersonWork("needs-plan", false, recs, labs, nil, now)
	// unsure a1/a2 (no organizing), sample audit, and the disagreement pd; the agreement pa does not wait and is not the person's truth
	if w.Records != 6 || w.Waits != 4 || w.RecentRecords != 5 || w.RecentWaits != 3 || w.ByPerson != 1 || w.ByResult != 2 {
		t.Fatalf("needs-plan: %+v", w)
	}
	w = countPersonWork("prompt-kind", true, recs, labs, nil, now)
	// Unsure decisions whose truth comes from the outcome, and uncertain discards, do not wait for a person
	if w.Waits != 1 {
		t.Fatalf("prompt-kind: asks must not wait for a person: %+v", w)
	}
	// An uncertain discard of a choice goes to a person
	if got := countPersonWork("same-issue", true, recs, labs, nil, now).Waits; got != 6 {
		t.Fatalf("a choice spec sends unsure discards to a person: %d", got)
	}
	var b strings.Builder
	w.print(&b, "  ")
	if !strings.Contains(b.String(), "1 of 6 decisions wait for a person") || !strings.Contains(b.String(), "by results 2") {
		t.Fatalf("print: %s", b.String())
	}
	// The AI's provisional scores (0.11): an agreement with the decision model (p .8: yes) does not wait, a disagreement and an undecided score do,
	// unless the outcome answered first (r3)
	scored := []store.Record{{TraceID: "r1", Band: "discard", P: .8, At: now}, {TraceID: "r2", Band: "discard", P: .8, At: now}, {TraceID: "r3", Band: "discard", P: .8, At: now}, {TraceID: "r4", Band: "act", P: .8, At: now}}
	reviews := map[string]store.Review{"r1": {Truth: "yes"}, "r2": {Truth: "unknown"}, "r3": {Truth: "no"}, "r4": {Truth: "no"}}
	if got := countPersonWork("done-as-asked", false, scored, labs, reviews, now).Waits; got != 2 {
		t.Fatalf("done-as-asked: the undecided r2 and the disagreement r4 wait: %d", got)
	}
}

// 0.9.5 check: shikii calibrate re-asks the model with the stored text of earlier truths and saves those answers with source "replay" (new
// trace ids). They counted as the person's answers, so one calibrate run on a question with 11 truths from results turned "answered by you"
// from 2 into 13. A replay is the machine asking again, not new work by the person or a new result.
func TestReplayedTruthsAreNotPersonWork(t *testing.T) {
	labs := []store.Label{
		{TraceID: "0000000000000001", Truth: "yes", Source: "outcome"},
		{TraceID: "0000000000000002", Truth: "no", Source: "ask"},
		{TraceID: "0000000000000003", Truth: "yes", Source: "replay"},
		{TraceID: "0000000000000004", Truth: "no", Source: "replay"},
	}
	w := countPersonWork("needs-plan", false, nil, labs, nil, time.Now())
	if w.ByPerson != 1 || w.ByResult != 1 {
		t.Fatalf("replayed truths are neither the person's answers nor results: person=%d result=%d", w.ByPerson, w.ByResult)
	}
}

// A truth an agent sent through MCP (source mcp) is not the person's answer. The page's "truths decided by you" must not count it.
func TestCountPersonWorkLeavesOutMCPTruths(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	recs := []store.Record{{TraceID: "a1", Band: "ask", At: now}, {TraceID: "a2", Band: "ask", At: now}}
	labs := []store.Label{{TraceID: "a1", Source: "ask"}, {TraceID: "a2", Source: "mcp"}}
	if w := countPersonWork("needs-plan", false, recs, labs, nil, now); w.ByPerson != 1 {
		t.Fatalf("truths decided by the person: %d, want 1 (the mcp truth is the agent's): %+v", w.ByPerson, w)
	}
}
