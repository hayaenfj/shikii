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
