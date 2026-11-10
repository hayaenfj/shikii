package store

import (
	"math"
	"testing"
)

// A weight says how firm a truth is: (0, 1]. 0 means "not given" and becomes 1. A negative or non-number weight
// is a mistake and must be refused, not saved as a firm truth of weight 1.
func TestLabelForRefusesNegativeAndNonNumberWeights(t *testing.T) {
	rec := &Record{TraceID: "aaaaaaaaaaaaaaaa", SpecID: "s", SpecHash: "h", Type: "noul", Model: "m",
		Raw: map[string]float64{"yes": 0.6, "no": 0.4}}
	for _, w := range []float64{-0.5, -1, math.Inf(-1), math.NaN()} {
		if l, err := LabelFor(rec, "yes", w, "manual"); err == nil {
			t.Errorf("weight %v was accepted and saved as weight %v", w, l.Weight)
		}
		if ls, err := LabelsFor(rec, "yes", w, "manual"); err == nil {
			t.Errorf("LabelsFor: weight %v was accepted: %+v", w, ls)
		}
	}
	// guard: what was accepted before stays accepted, and too large stays refused
	for w, want := range map[float64]float64{0: 1, 0.7: 0.7, 1: 1} {
		l, err := LabelFor(rec, "yes", w, "manual")
		if err != nil || l.Weight != want {
			t.Errorf("weight %v: got %v, %v; want %v", w, l.Weight, err, want)
		}
	}
	for _, w := range []float64{1.5, math.Inf(1)} {
		if _, err := LabelFor(rec, "yes", w, "manual"); err == nil {
			t.Errorf("weight %v must be refused", w)
		}
	}
}
