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

// OutcomeAgreement compares, for each decision that has an outcome's truth and a later answer of the person, the two (a partial truth
// "!x" agrees with any answer but x). labels are all of a question's labels, in the order they were written.
func OutcomeAgreement(labels []Label) OutcomeCheck {
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
	var c OutcomeCheck
	for trace, o := range outcome {
		p, ok := person[trace]
		if !ok || p.At.Before(o.At) {
			continue // the person answered before the outcome came (not a check of it)
		}
		c.Checked++
		if truthsAgree(o.Truth, p.Truth) {
			c.Agree++
		}
	}
	c.Low, c.High = wilson(c.Agree, c.Checked)
	return c
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

// Untrusted reports whether the outcome's truths are clearly less right than act asks: they are not used to draw lines.
func (c OutcomeCheck) Untrusted(act float64) bool { return c.Checked > 0 && c.High < act }

// Settled reports whether more checks would not change the decision either way.
func (c OutcomeCheck) Settled(act float64) bool {
	return c.Untrusted(act) || (c.Checked > 0 && c.Low >= act)
}

// WithoutUntrustedOutcome drops the outcome's truths from labels when check says they are untrusted.
func WithoutUntrustedOutcome(labels []Label, check OutcomeCheck, act float64) []Label {
	if !check.Untrusted(act) {
		return labels
	}
	out := make([]Label, 0, len(labels))
	for _, l := range labels {
		if l.Source != "outcome" {
			out = append(out, l)
		}
	}
	return out
}
