package notebook

import (
	"errors"
)

// RetainExecutionSources settles already checked machine diagnostics, keeping
// their originals addressable. It creates no lesson, effect grade or truth.
// Human intent and general agent observations still need their ordinary review.
func (b Book) RetainExecutionSources(diagnostics, evidence []Source) error {
	if len(diagnostics) < 1 || len(diagnostics) > 2 || len(evidence) > 8 {
		return errors.New("invalid bounded execution diagnostic sources")
	}
	ids := []string{}
	for _, ref := range diagnostics {
		o, e := b.checkedOutcomeSource(ref)
		if e != nil {
			return e
		}
		if kind, ok := WorkSourceOf(o); !ok || (kind != WorkProcedure && kind != WorkResult && kind != WorkReading) {
			return errors.New("only checked machine execution diagnostics are settled automatically")
		}
		ids = append(ids, o.ID)
	}
	for _, ref := range evidence {
		o, e := b.checkedOutcomeSource(ref)
		if e != nil {
			return e
		}
		if o.Kind == "test" || o.Kind == "code" {
			ids = append(ids, o.ID)
		}
	}
	_, e := b.Ingest(Draft{Sources: ids})
	return e
}
