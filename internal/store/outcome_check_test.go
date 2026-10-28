package store

import (
	"math"
	"testing"
	"time"
)

// The agreement counts only decisions where the person answered after the outcome, compares a partial truth "!x" as "not x", and gives
// the Wilson interval worked out by hand (3 of 4: about 0.301 to 0.954).
func TestOutcomeAgreementComparesThePersonsLaterAnswer(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	l := func(trace, truth, source string, minute int) Label {
		return Label{TraceID: trace, Truth: truth, Source: source, At: t0.Add(time.Duration(minute) * time.Minute)}
	}
	labels := []Label{
		l("a", "yes", "outcome", 0), l("a", "yes", "ui", 1), // agree
		l("b", "no", "outcome", 0), l("b", "yes", "audit", 1), // disagree
		l("c", "!edit", "outcome", 0), l("c", "explain", "ui", 1), // a partial truth agrees with another answer
		l("d", "edit", "outcome", 0), l("d", "edit", "manual", 1), // agree
		l("e", "yes", "ui", 0), l("e", "no", "outcome", 1), // the person answered first: not a check
		l("f", "yes", "outcome", 0), l("f", "no", "mcp", 1), // an agent is not a person
	}
	c := OutcomeAgreement(labels)
	if c.Checked != 4 || c.Agree != 3 {
		t.Fatalf("4 checks, 3 agree: %+v", c)
	}
	if math.Abs(c.Low-0.3006) > 1e-3 || math.Abs(c.High-0.9544) > 1e-3 {
		t.Fatalf("Wilson interval of 3 in 4: %+v", c)
	}
	if !(OutcomeCheck{Checked: 20, Agree: 8, Low: 0.22, High: 0.61}).Untrusted(0.75) || (OutcomeCheck{Checked: 4, Agree: 3, Low: 0.30, High: 0.95}).Untrusted(0.75) {
		t.Fatal("untrusted only when the upper end is below act")
	}
	if !(OutcomeCheck{Checked: 40, Agree: 39, Low: 0.87, High: 0.99}).Settled(0.75) || (OutcomeCheck{Checked: 4, Agree: 3, Low: 0.30, High: 0.95}).Settled(0.75) {
		t.Fatal("settled when the interval is past act on either side")
	}
	kept := WithoutUntrustedOutcome(labels, OutcomeCheck{Checked: 20, High: 0.5}, 0.75)
	for _, k := range kept {
		if k.Source == "outcome" {
			t.Fatal("the outcome's truths are dropped when untrusted")
		}
	}
	if len(WithoutUntrustedOutcome(labels, c, 0.75)) != len(labels) {
		t.Fatal("kept while not untrusted")
	}
}
