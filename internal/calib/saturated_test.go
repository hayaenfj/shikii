package calib

import (
	"math"
	"testing"
)

// Measured on 2026-10-17 (shikii-readout + Bonsai 2, 32 yes/no pairs). Positives and negatives are fully separated,
// and the probabilities sit at the edges (|Δ| ≈ 5–6). Fitting Platt on this shape made the in-sample Brier
// worse, raw 0.0205 → 0.107. Maximum likelihood toward the smoothed targets (y+ ≈ 0.93, y− ≈ 0.06)
// should never exceed 0.01. Is Newton's method going wild on a saturated σ?
var saturatedProbs = []struct {
	p float64
	y bool
	w float64
}{
	{0.0030, false, 1}, {0.0030, false, 1}, {0.0036, false, 1}, {0.0037, false, 1}, {0.0037, false, 1}, {0.0037, false, 1},
	{0.0038, false, 1}, {0.0038, false, 1}, {0.0039, false, 1}, {0.0045, false, 1}, {0.0056, false, 1}, {0.0061, false, 1},
	{0.0062, false, 0.5}, {0.0074, false, 0.5}, {0.0133, false, 0.5}, {0.0161, false, 0.5}, {0.0212, false, 0.5},
	{0.1054, false, 0.5}, {0.6235, false, 0.5}, {0.8631, false, 0.5},
	{0.9703, true, 1}, {0.9711, true, 1}, {0.9873, true, 1}, {0.9931, true, 1}, {0.9932, true, 1}, {0.9950, true, 1},
	{0.9954, true, 1}, {0.9960, true, 1}, {0.9962, true, 1}, {0.9963, true, 1}, {0.9964, true, 1}, {0.9969, true, 1},
}

func TestFitDoesNotDivergeOnSaturatedSeparableData(t *testing.T) {
	var samples []Sample
	var rawP []float64
	var truths []bool
	var weights []float64
	for _, r := range saturatedProbs {
		samples = append(samples, Sample{Delta: Logit(r.p), Truth: r.y, Weight: r.w})
		rawP = append(rawP, r.p)
		truths = append(truths, r.y)
		weights = append(weights, r.w)
	}
	pl, err := Fit(samples)
	if err != nil {
		t.Fatal(err)
	}
	if math.IsNaN(pl.Bias) || math.IsInf(pl.Bias, 0) || pl.Temperature <= 0 || math.IsInf(pl.Temperature, 0) {
		t.Fatalf("parameters must be finite with positive temperature: %+v", pl)
	}
	cal := make([]float64, len(samples))
	for i, s := range samples {
		cal[i] = pl.Apply(s.Delta)
	}
	raw, fitted := Brier(rawP, truths, weights), Brier(cal, truths, weights)
	t.Logf("platt=%+v brier raw=%.4f fitted=%.4f", pl, raw, fitted)
	// Pulling toward the smoothed targets (0.93 / 0.06) costs about 0.01. More than that means the optimization is broken.
	if fitted > raw+0.01 {
		t.Fatalf("fitted Brier %.4f is far worse than raw %.4f: the optimiser did not converge (platt=%+v)", fitted, raw, pl)
	}
	// The lowest positive (0.9703) should stay above act (0.70) after calibration. Losing recall on separated data is wrong
	if p := pl.Apply(Logit(0.9703)); p < 0.70 {
		t.Fatalf("weakest positive dropped to %.3f after calibration (platt=%+v)", p, pl)
	}
}
