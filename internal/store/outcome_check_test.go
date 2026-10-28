package store

import (
	"fmt"
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
	r := OutcomeAgreement(labels)
	c := r.Total()
	if c.Checked != 4 || c.Agree != 3 {
		t.Fatalf("4 checks, 3 agree: %+v", c)
	}
	if yes, no := r.Sides["yes"], r.Sides["no"]; yes.Truths != 2 || yes.Checked != 1 || no.Truths != 2 || no.Checked != 1 || no.Agree != 0 {
		t.Fatalf("each answer of the outcome is measured on its own: %+v", r.Sides)
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
	untrustedNo := OutcomeReport{Sides: map[string]OutcomeSide{"no": {Truths: 2, OutcomeCheck: OutcomeCheck{Checked: 20, High: 0.5}}, "yes": {Truths: 2, OutcomeCheck: OutcomeCheck{Checked: 20, Low: 0.9, High: 1}}}}
	for _, k := range WithoutUntrustedOutcome(labels, untrustedNo, 0.75) {
		if k.Source == "outcome" && k.Truth == "no" {
			t.Fatal("the outcome's truths on the untrusted side are dropped")
		}
	}
	if len(WithoutUntrustedOutcome(labels, untrustedNo, 0.75)) != len(labels)-2 {
		t.Fatal("only the untrusted side's outcome truths are dropped")
	}
}

// 0.11: the outcome's rare answer is the one in doubt. A side with OutcomeCheckAll truths or fewer is shown whole, a larger one 1 in
// OutcomeCheckEvery; the agreement of the common side does not settle the rare one (12 of 12 "yes" checked settled changes-in-scope
// while none of its 14 "no" had been checked).
func TestOutcomeChecksMeasureTheRareAnswerToo(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	var labels []Label
	for i := 0; i < 200; i++ {
		truth := "yes"
		if i < 14 {
			truth = "no"
		}
		labels = append(labels, Label{TraceID: fmt.Sprintf("%016x", i), Truth: truth, Source: "outcome", At: t0})
	}
	for i := 14; i < 40; i++ { // the person agreed with 26 of the outcome's "yes": that side is settled
		labels = append(labels, Label{TraceID: fmt.Sprintf("%016x", i), Truth: "yes", Source: "ui", At: t0.Add(time.Minute)})
	}
	r := OutcomeAgreement(labels)
	if !r.Sides["yes"].Settled(0.75) || r.Settled(0.75) {
		t.Fatalf("the common side is settled, the rare one is not, so checks go on: %+v", r.Sides)
	}
	for i := 0; i < 14; i++ {
		if !r.Shown(fmt.Sprintf("%016x", i), "no", 0.75) {
			t.Fatalf("every one of the 14 \"no\" is shown: %d", i)
		}
	}
	if r.Shown(fmt.Sprintf("%016x", 100), "yes", 0.75) {
		t.Fatal("the settled side is not shown any more")
	}
}
