package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

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
