package store

import (
	"fmt"
	"testing"
	"time"
)

func TestRejectedOutcomeCannotReturnThroughReplay(t *testing.T) {
	t0 := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	var history []Label
	for i := 0; i < 5; i++ {
		id := fmt.Sprint(i)
		history = append(history, Label{TraceID: id, InputsHash: id, Truth: "no", Source: "outcome", At: t0},
			Label{TraceID: id, InputsHash: id, Truth: "yes", Source: "audit", At: t0.Add(time.Minute)})
	}
	history = append(history, Label{TraceID: "unconfirmed", InputsHash: "unconfirmed", Truth: "no", Source: "outcome", At: t0},
		Label{TraceID: "human", InputsHash: "human", Truth: "no", Source: "manual", At: t0},
		Label{TraceID: "partial", InputsHash: "partial", Truth: "!edit", Source: "manual", At: t0})
	report := OutcomeAgreement(history)
	if !report.Untrusted("no", .75) {
		t.Fatal("control: the outcome's negative rule was rejected")
	}
	for _, tc := range []struct {
		name, input, truth string
		keep               bool
	}{
		{"rejected outcome", "unconfirmed", "no", false},
		{"person's negative answer", "human", "no", true},
		{"person's correction", "0", "yes", true},
		{"stale replay before correction", "0", "no", false},
		{"person's partial answer", "partial", "!edit", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replayed := Label{TraceID: "new", InputsHash: tc.input, Truth: tc.truth, Source: "replay", At: t0.Add(2 * time.Minute)}
			got := WithoutUntrustedOutcome([]Label{replayed}, report, .75)
			if (len(got) == 1) != tc.keep {
				t.Fatalf("replay must retain the original truth's authority: kept=%v want=%v", len(got) == 1, tc.keep)
			}
		})
	}
}
