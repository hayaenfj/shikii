package notebook

import (
	"fmt"
	"strings"
	"time"
)

// WorkSource is a source shikii writes about a work itself: a report (a result, an ending review, the binding of a result's
// evidence, the record of a procedure a result closed), an archive of the work's state, or a record of what the agent did (a
// trial, a measured reading, a procedure outcome). Its title and whether evidence may cite it are fixed by the constant.
//
// A source is saved through CaptureObserved (what an agent or a person observed) or CaptureWork (one of these): there is no other
// way out of this package, so a source shikii writes always takes a title of this list, and an observation never takes one. The
// audits of 0.31.0 found, round after round, another way a report passed for an observation while reports were told apart by a
// table of titles that each writer had to keep up with.
type WorkSource uint8

const (
	WorkResult          WorkSource = iota + 1 // the result of a work and its remaining conditions
	WorkReview                                // the ending review of a work's judgment
	WorkRecovery                              // the source that binds a recovered result to its evidence
	WorkProcedure                             // the record of a procedure a result closed
	WorkNoteIntent                            // archive: a required wiki note and the bindings it was prepared with
	WorkCapacity                              // archive: the required work at the session capacity boundary
	WorkPending                               // archive: pending wiki work before a preparation replaces it
	WorkTrialPlan                             // record: the plan of a trial
	WorkTrialExecution                        // record: the execution of a trial
	WorkTrialComparison                       // record: a comparison of trials
	WorkReading                               // record: a measured reading
	WorkOutcome                               // record: a procedure outcome with the evidence it cites
	workSourceEnd
)

// The titles are the ones shikii has always written: sources saved by every version read the same.
var workSourceTitles = [workSourceEnd]string{
	WorkResult:          "Work result and remaining conditions",
	WorkReview:          "Evidence-linked work judgment and ending review",
	WorkRecovery:        "Recovered work result evidence",
	WorkProcedure:       "Procedure execution",
	WorkNoteIntent:      "Required wiki note and original prepared bindings",
	WorkCapacity:        "Required work and bindings at the session capacity boundary",
	WorkPending:         "Pending wiki work before replacing preparation",
	WorkTrialPlan:       "Work trial plan",
	WorkTrialExecution:  "Work trial execution",
	WorkTrialComparison: "Work trial comparison",
	WorkReading:         "Measured reading execution",
	WorkOutcome:         "Procedure execution with cited evidence",
}

// Title is the title shikii gives the source.
func (k WorkSource) Title() string {
	if k == 0 || k >= workSourceEnd {
		return ""
	}
	return workSourceTitles[k]
}

// Citable reports whether evidence may cite the source: a record of what the agent did is cited like the observation it records;
// a report or an archive a work wrote about itself is no observation.
func (k WorkSource) Citable() bool {
	return k >= WorkTrialPlan && k < workSourceEnd
}

// Describe says what the source is, in a refusal that names it.
func (k WorkSource) Describe() string {
	switch k {
	case WorkResult:
		return "an earlier result"
	case WorkReview:
		return "an ending review"
	case WorkRecovery:
		return "the source that binds a result to its evidence"
	case WorkProcedure:
		return "the record of a procedure a result closed"
	case WorkNoteIntent, WorkCapacity, WorkPending:
		return fmt.Sprintf("%q, an archive of the work's state", k.Title())
	}
	return "a record of what the agent did"
}

// WorkSourceOf is the work source a saved observation is: kind agent (or none) and one of the titles, the spaces around it left out.
func WorkSourceOf(o Observation) (WorkSource, bool) {
	if o.Kind != "agent" && o.Kind != "" {
		return 0, false
	}
	title := strings.TrimSpace(o.Title)
	for k := WorkResult; k < workSourceEnd; k++ {
		if workSourceTitles[k] == title {
			return k, true
		}
	}
	return 0, false
}

// ValidateObserved checks an observation an agent or a person sends, without writing it: Validate, and a title shikii gives its own
// sources is refused (saved, the observation would be taken for one of them).
func (o Observation) ValidateObserved() error {
	if err := o.Validate(); err != nil {
		return err
	}
	if _, ok := WorkSourceOf(o); ok {
		return fmt.Errorf("the title %q is the one shikii gives its own reports and records; give the observation a title of its own (what you checked)", o.Title)
	}
	return nil
}

// CaptureObserved saves what an agent or a person observed (wiki capture, the observations of a work record, a quoted request).
func (b Book) CaptureObserved(o Observation) (Observation, error) {
	if o.ID == "" {
		if err := o.ValidateObserved(); err != nil {
			return Observation{}, fmt.Errorf("wiki capture: %w", err)
		}
	}
	return b.capture(o)
}

// CaptureWork saves a source shikii writes about a work, under the title of its kind.
func (b Book) CaptureWork(k WorkSource, content, reference, version string, observedAt time.Time) (Observation, error) {
	if k.Title() == "" {
		return Observation{}, fmt.Errorf("wiki capture: unknown work source %d", k)
	}
	return b.capture(Observation{Kind: "agent", Title: k.Title(), Content: content, Reference: reference, Version: version, ObservedAt: observedAt})
}
