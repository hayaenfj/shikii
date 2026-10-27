package calib

import (
	"math"
	"math/rand"
	"testing"
)

// From data generated with a known bias/T, Fit recovers those values.
func TestFitRecoversKnownPlatt(t *testing.T) {
	truth := Platt{Bias: 2.2, Temperature: 0.3}
	rng := rand.New(rand.NewSource(1))
	var samples []Sample
	for i := 0; i < 4000; i++ {
		delta := rng.NormFloat64()*1.5 + 2.2
		p := truth.Apply(delta)
		samples = append(samples, Sample{Delta: delta, Truth: rng.Float64() < p, Weight: 1})
	}
	got, err := Fit(samples)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.Bias-truth.Bias) > 0.1 || math.Abs(got.Temperature-truth.Temperature) > 0.05 {
		t.Fatalf("fit drifted: got bias=%.3f T=%.3f want bias=%.3f T=%.3f", got.Bias, got.Temperature, truth.Bias, truth.Temperature)
	}
}

func TestFitRejectsDegenerateData(t *testing.T) {
	if _, err := Fit([]Sample{{Delta: 1, Truth: true}, {Delta: 2, Truth: true}}); err == nil {
		t.Fatal("single-class data must not fit")
	}
	// Higher probability means false: the slope is negative, so it is returned as an error
	inv := []Sample{{Delta: 3, Truth: false}, {Delta: 2.5, Truth: false}, {Delta: -2, Truth: true}, {Delta: -3, Truth: true}}
	if _, err := Fit(inv); err == nil {
		t.Fatal("negative slope must be rejected")
	}
}

func TestBrierAndECE(t *testing.T) {
	probs := []float64{1, 0, 1, 0}
	truths := []bool{true, false, true, false}
	if b := Brier(probs, truths, nil); b != 0 {
		t.Fatalf("perfect predictions must have Brier 0, got %f", b)
	}
	if e := ECE(probs, truths, nil, 10); e != 0 {
		t.Fatalf("perfect predictions must have ECE 0, got %f", e)
	}
	// Says 0.9 every time and is right only half the time: overconfident. ECE ≈ 0.4, Brier = 0.5·0.01 + 0.5·0.81 = 0.41
	over := []float64{0.9, 0.9, 0.9, 0.9}
	half := []bool{true, false, true, false}
	if e := ECE(over, half, nil, 10); math.Abs(e-0.4) > 1e-9 {
		t.Fatalf("overconfident ECE want 0.4, got %f", e)
	}
	if b := Brier(over, half, nil); math.Abs(b-0.41) > 1e-9 {
		t.Fatalf("overconfident Brier want 0.41, got %f", b)
	}
	// Weights: making the 2nd item weigh 3 (rather than 0, which falls back to 1) gives Brier (0.01+3·0.81+0.01+0.81)/6
	w := []float64{1, 3, 1, 1}
	if b := Brier(over, half, w); math.Abs(b-(0.01+3*0.81+0.01+0.81)/6) > 1e-9 {
		t.Fatalf("weighted Brier wrong: %f", b)
	}
}

func TestReliabilityBins(t *testing.T) {
	probs := []float64{0.05, 0.95, 0.95, 1.0}
	truths := []bool{false, true, false, true}
	rel := Reliability(probs, truths, nil, 10)
	if rel[0].N != 1 || rel[0].Accuracy != 0 || rel[0].Confidence != 0.05 {
		t.Fatalf("bin 0: %+v", rel[0])
	}
	// p=1.0 goes into the last bin
	last := rel[9]
	if last.N != 3 || math.Abs(last.Accuracy-2.0/3) > 1e-9 || math.Abs(last.Confidence-(0.95+0.95+1.0)/3) > 1e-9 {
		t.Fatalf("bin 9: %+v", last)
	}
}

func TestCheckSeparationDetectsInversion(t *testing.T) {
	clean := []Sample{{Delta: 2.54, Truth: true}, {Delta: 4, Truth: true}, {Delta: 1.96, Truth: false}, {Delta: 0, Truth: false}}
	if s := CheckSeparation(clean); s.Inverted || s.Overlap != 0 || s.PosMin != 2.54 || s.NegMax != 1.96 {
		t.Fatalf("clean data flagged: %+v", s)
	}
	// Inversion measured with had-scorer: lowest related 1.80 < borderline 1.82
	inverted := []Sample{{Delta: 1.80, Truth: true}, {Delta: 4, Truth: true}, {Delta: 1.82, Truth: false}, {Delta: 0, Truth: false}}
	if s := CheckSeparation(inverted); !s.Inverted || s.Overlap != 2 {
		t.Fatalf("inversion not detected: %+v", s)
	}
	// A borderline negative (weight 0.5) above the positives is not called an inversion, because it is known to be ambiguous.
	weak := []Sample{{Delta: 2.54, Truth: true, Weight: 1}, {Delta: 4, Truth: true, Weight: 1},
		{Delta: 3.0, Truth: false, Weight: 0.5}, {Delta: 1.96, Truth: false, Weight: 1}}
	if s := CheckSeparation(weak); s.Inverted || s.Weak != 1 || s.NegMax != 1.96 {
		t.Fatalf("weak labels must not drive the inversion verdict: %+v", s)
	}
	// The same negative with weight 1 is an inversion
	weak[2].Weight = 1
	if s := CheckSeparation(weak); !s.Inverted {
		t.Fatalf("full-weight negative above pos_min must be an inversion: %+v", s)
	}
}

func TestLogitClamps(t *testing.T) {
	if math.IsInf(Logit(0), 0) || math.IsInf(Logit(1), 0) || math.IsNaN(Logit(0)) {
		t.Fatal("logit must clamp 0 and 1")
	}
	if math.Abs(Sigmoid(Logit(0.3))-0.3) > 1e-9 {
		t.Fatal("sigmoid∘logit must be identity in (0,1)")
	}
}

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
