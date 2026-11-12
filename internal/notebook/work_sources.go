package notebook

import (
	"fmt"
	"strconv"
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
//
// The kind is a struct whose field only this package sets: outside it a WorkSource is one of the values below or the zero value (no
// work source), never a number turned into one (audit N of 0.31.0: WorkSource(5) compiled and saved a source of that kind).
type WorkSource struct{ k uint8 }

var (
	WorkResult          = WorkSource{1}  // the result of a work and its remaining conditions
	WorkReview          = WorkSource{2}  // the ending review of a work's judgment
	WorkRecovery        = WorkSource{3}  // the source that binds a recovered result to its evidence
	WorkProcedure       = WorkSource{4}  // the record of a procedure a result closed
	WorkNoteIntent      = WorkSource{5}  // archive: a required wiki note and the bindings it was prepared with
	WorkCapacity        = WorkSource{6}  // archive: the required work at the session capacity boundary
	WorkPending         = WorkSource{7}  // archive: pending wiki work before a preparation replaces it
	WorkTrialPlan       = WorkSource{8}  // record: the plan of a trial
	WorkTrialExecution  = WorkSource{9}  // record: the execution of a trial
	WorkTrialComparison = WorkSource{10} // record: a comparison of trials
	WorkReading         = WorkSource{11} // record: a measured reading
	WorkOutcome         = WorkSource{12} // record: a procedure outcome with the evidence it cites
)

const workSourceEnd = 13

// WorkSources is every kind, in order.
func WorkSources() []WorkSource {
	all := make([]WorkSource, 0, workSourceEnd-1)
	for k := uint8(1); k < workSourceEnd; k++ {
		all = append(all, WorkSource{k})
	}
	return all
}

// IsZero reports whether the value is no work source (an observation).
func (k WorkSource) IsZero() bool { return k.k == 0 }

// MarshalJSON writes the kind as its number (the capture memo of a work keeps it).
func (k WorkSource) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(int(k.k))), nil }

// UnmarshalJSON reads the number back; a number of no kind is refused.
func (k *WorkSource) UnmarshalJSON(b []byte) error {
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 0 || n >= workSourceEnd {
		return fmt.Errorf("unknown work source %s", b)
	}
	k.k = uint8(n)
	return nil
}

// The titles are the ones shikii has always written: sources saved by every version read the same.
var workSourceTitles = [workSourceEnd]string{
	1:  "Work result and remaining conditions",
	2:  "Evidence-linked work judgment and ending review",
	3:  "Recovered work result evidence",
	4:  "Procedure execution",
	5:  "Required wiki note and original prepared bindings",
	6:  "Required work and bindings at the session capacity boundary",
	7:  "Pending wiki work before replacing preparation",
	8:  "Work trial plan",
	9:  "Work trial execution",
	10: "Work trial comparison",
	11: "Measured reading execution",
	12: "Procedure execution with cited evidence",
}

// Title is the title shikii gives the source.
func (k WorkSource) Title() string {
	if k.k == 0 || k.k >= workSourceEnd {
		return ""
	}
	return workSourceTitles[k.k]
}

// Citable reports whether evidence may cite the source: a record of what the agent did is cited like the observation it records;
// a report or an archive a work wrote about itself is no observation.
func (k WorkSource) Citable() bool {
	return k.k >= WorkTrialPlan.k && k.k < workSourceEnd
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
		return WorkSource{}, false
	}
	title := strings.TrimSpace(o.Title)
	for k := uint8(1); k < workSourceEnd; k++ {
		if workSourceTitles[k] == title {
			return WorkSource{k}, true
		}
	}
	return WorkSource{}, false
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
		return Observation{}, fmt.Errorf("wiki capture: unknown work source %d", k.k)
	}
	return b.capture(Observation{Kind: "agent", Title: k.Title(), Content: content, Reference: reference, Version: version, ObservedAt: observedAt})
}
