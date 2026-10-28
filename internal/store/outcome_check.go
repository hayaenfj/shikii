package store

// Checking the truths outcomes give (0.11). A truth from an outcome (source outcome: a file reverted, the person restating the request,
// the working tree changed) stands in for a person's answer, and was never compared with one. A few decisions an outcome answered are
// shown to the person; their answers measure how often the outcome's rule agrees. When it is clearly below what act asks (the upper
// end of the 95% Wilson interval below the act threshold), the outcome's truths of that question are not used to draw lines; when it
// is clearly at or above (the lower end reaches act), no more checks are asked. Fixed rules, no options.

import (
	"hash/fnv"
	"math"
	"strings"
)

// OutcomeCheckEvery: 1 in this many decisions an outcome answered is shown to the person while the agreement is still unsettled.
const OutcomeCheckEvery = 10

// OutcomeCheckAll: a side (the outcome's answer, such as "no") with this many truths or fewer is shown whole, not 1 in 10. The rare
// side is the one in doubt (changes-in-scope's "no" was 14 of 173), and sampling 1 in 10 checked only the common side (0.11).
const OutcomeCheckAll = 50

// OutcomeCheckSample reports whether the decision is one of the 1 in OutcomeCheckEvery shown for checking (fixed by the trace, so the
// same decision is always in or out, also for truths given before 0.11).
func OutcomeCheckSample(traceID string) bool {
	if traceID == "" {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte("outcome-check:" + traceID))
	return h.Sum32()%OutcomeCheckEvery == 0
}

// personChecks are the sources of a person's own answer (the page, a spot check, a hand label). An agent's answer (mcp), a re-asked
// probability (replay) and a file row (file) are not a person checking the outcome.
var personChecks = map[string]bool{"ui": true, "audit": true, "manual": true}

// OutcomeCheck is how often an outcome's truths agreed with the person's later answer on the same decision, with its 95% Wilson interval.
type OutcomeCheck struct {
	Checked, Agree int
	Low, High      float64
}

// OutcomeSide is the check of the outcome's truths with one answer ("yes", "no", "!edit"): how many there are, and how they agreed.
type OutcomeSide struct {
	Truths int
	OutcomeCheck
}

// OutcomeReport is the check by the outcome's answer. Each answer is measured on its own: a rule can be right on the common answer
// and wrong on the rare one.
type OutcomeReport struct {
	Sides map[string]OutcomeSide
}

// OutcomeAgreement compares, for each decision that has an outcome's truth and a later answer of the person, the two (a partial truth
// "!x" agrees with any answer but x), by the outcome's answer. labels are all of a question's labels, in the order they were written.
func OutcomeAgreement(labels []Label) OutcomeReport {
	outcome := map[string]Label{}
	person := map[string]Label{}
	for _, l := range labels {
		switch {
		case l.TraceID == "":
		case l.Source == "outcome":
			outcome[l.TraceID] = l
		case personChecks[l.Source]:
			person[l.TraceID] = l
		}
	}
	r := OutcomeReport{Sides: map[string]OutcomeSide{}}
	for trace, o := range outcome {
		side := r.Sides[o.Truth]
		side.Truths++
		if p, ok := person[trace]; ok && !p.At.Before(o.At) { // the person answered after the outcome came: a check of it
			side.Checked++
			if truthsAgree(o.Truth, p.Truth) {
				side.Agree++
			}
		}
		r.Sides[o.Truth] = side
	}
	for truth, side := range r.Sides {
		side.Low, side.High = wilson(side.Agree, side.Checked)
		r.Sides[truth] = side
	}
	return r
}

// Total is the check over every side, for a one-line summary.
func (r OutcomeReport) Total() OutcomeCheck {
	var c OutcomeCheck
	for _, side := range r.Sides {
		c.Checked += side.Checked
		c.Agree += side.Agree
	}
	c.Low, c.High = wilson(c.Agree, c.Checked)
	return c
}

// Untrusted reports whether the outcome's truths with this answer are clearly less right than act asks: they are not used.
func (r OutcomeReport) Untrusted(truth string, act float64) bool {
	side, ok := r.Sides[truth]
	return ok && side.OutcomeCheck.Untrusted(act)
}

// Settled reports whether every side is settled, so no more checks are asked.
func (r OutcomeReport) Settled(act float64) bool {
	for _, side := range r.Sides {
		if !side.OutcomeCheck.Settled(act) {
			return false
		}
	}
	return true
}

// Shown reports whether the decision the outcome answered with truth is shown to the person to check: its side is unsettled, and it is
// one of the 1 in OutcomeCheckEvery, or its side is small enough to show whole.
func (r OutcomeReport) Shown(traceID, truth string, act float64) bool {
	side, ok := r.Sides[truth]
	return ok && !side.OutcomeCheck.Settled(act) && (side.Truths <= OutcomeCheckAll || OutcomeCheckSample(traceID))
}

func truthsAgree(outcome, person string) bool {
	if not, ok := strings.CutPrefix(outcome, "!"); ok {
		return person != not && person != outcome
	}
	return outcome == person
}

// wilson is the 95% Wilson score interval of k in n (0..1 when n is 0).
func wilson(k, n int) (low, high float64) {
	if n == 0 {
		return 0, 1
	}
	const z = 1.96
	p, nf := float64(k)/float64(n), float64(n)
	d := 1 + z*z/nf
	mid := (p + z*z/(2*nf)) / d
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / d
	return math.Max(0, mid-half), math.Min(1, mid+half)
}

// Untrusted reports whether the outcome's truths are clearly less right than act asks.
func (c OutcomeCheck) Untrusted(act float64) bool { return c.Checked > 0 && c.High < act }

// Settled reports whether more checks would not change the decision either way.
func (c OutcomeCheck) Settled(act float64) bool {
	return c.Untrusted(act) || (c.Checked > 0 && c.Low >= act)
}

// WithoutUntrustedOutcome drops the outcome's truths whose answer's side is untrusted.
func WithoutUntrustedOutcome(labels []Label, r OutcomeReport, act float64) []Label {
	out := make([]Label, 0, len(labels))
	for _, l := range labels {
		if l.Source == "outcome" && r.Untrusted(l.Truth, act) {
			continue
		}
		out = append(out, l)
	}
	return out
}
