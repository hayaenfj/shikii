package main

import (
	"strings"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// unsureChoiceNothingOpen says whether a needs-plan choice is unsure (no answer reached the threshold: discard) and the agent found nothing
// open. Such an item waits for no grade and is shown to no one: only a concrete open decision is asked (CHANGELOG 0.35.0).
func unsureChoiceNothingOpen(rec store.Record) bool {
	return rec.Type == "choice" && rec.Band == string(decide.Discard) && strings.TrimSpace(rec.Inputs["open"]) == planningOpenUnknown
}

// planningWaits says whether a needs-plan decision waits for the person: a disagreement with the agent's organizing, or the spot check
// of a calibrated act.
func planningWaits(d *decide.Decision, rec store.Record) bool {
	if unsureChoiceNothingOpen(rec) {
		return false
	}
	waits := waitsForPersonFor("needs-plan", d)
	if pair, ok := pairOf("needs-plan", rec, nil); ok {
		waits = !pair.agree() || (d.Band == decide.Act && d.IsCalibrated && store.AuditSample(d.TraceID))
	}
	return waits
}
