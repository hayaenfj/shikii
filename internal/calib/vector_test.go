package calib

import (
	"math"
	"math/rand"
	"testing"
)

// From distributions distorted by a known T and bias, FitVector recovers the distortion.
func TestFitVectorRecoversKnownDistortion(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	keys := []string{"a", "b", "c"}
	// Build the true distribution q; the observation is "q blunted with T=2, with a +1.0 habit added to c".
	// The calibrator should find the inverse (T=0.5 or so, −1.0 on c).
	var samples []MultiSample
	for i := 0; i < 3000; i++ {
		logits := []float64{rng.NormFloat64() * 2, rng.NormFloat64() * 2, rng.NormFloat64() * 2}
		q := softmax(logits)
		truth := keys[sampleIndex(rng, q)]
		obs := softmax([]float64{logits[0] / 2, logits[1] / 2, logits[2]/2 + 1.0})
		samples = append(samples, MultiSample{Raw: map[string]float64{"a": obs[0], "b": obs[1], "c": obs[2]}, Truth: truth, Weight: 1})
	}
	v, err := FitVector(samples)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(v.Temperature-0.5) > 0.08 {
		t.Fatalf("temperature: got %.3f want ≈0.5", v.Temperature)
	}
	// b is projected onto Σb=0, so look at the difference between c and a/b: b_c − b_a ≈ −1.0
	if d := v.Biases["c"] - v.Biases["a"]; math.Abs(d-(-1.0)) > 0.2 {
		t.Fatalf("bias(c)−bias(a): got %.3f want ≈−1.0 (biases=%v)", d, v.Biases)
	}
	// Brier improves after calibration
	var rawB, calB float64
	for _, s := range samples {
		cal := v.Apply(s.Raw)
		for _, k := range keys {
			y := 0.0
			if k == s.Truth {
				y = 1
			}
			rawB += (s.Raw[k] - y) * (s.Raw[k] - y)
			calB += (cal[k] - y) * (cal[k] - y)
		}
	}
	if calB >= rawB {
		t.Fatalf("calibration must reduce multiclass Brier: raw=%.1f cal=%.1f", rawB, calB)
	}
}

func TestVectorApplyIsIdentityAtNeutralParams(t *testing.T) {
	v := Vector{Temperature: 1, Biases: map[string]float64{}}
	raw := map[string]float64{"x": 0.2, "y": 0.5, "z": 0.3}
	out := v.Apply(raw)
	for k, p := range raw {
		if math.Abs(out[k]-p) > 1e-9 {
			t.Fatalf("neutral params must be identity: %v → %v", raw, out)
		}
	}
	// the bias swaps the ranking (pushes back "escaping to other")
	v2 := Vector{Temperature: 1, Biases: map[string]float64{"y": -2}}
	out2 := v2.Apply(raw)
	if out2["y"] >= out2["z"] {
		t.Fatalf("negative bias on y must push it below z: %v", out2)
	}
}

func TestFitVectorRejectsBadInput(t *testing.T) {
	if _, err := FitVector(nil); err == nil {
		t.Fatal("empty must fail")
	}
	if _, err := FitVector([]MultiSample{{Raw: map[string]float64{"a": 1}, Truth: "a"}}); err == nil {
		t.Fatal("single option must fail")
	}
	if _, err := FitVector([]MultiSample{{Raw: map[string]float64{"a": 0.5, "b": 0.5}, Truth: "zzz"}}); err == nil {
		t.Fatal("truth outside options must fail")
	}
}

func sampleIndex(rng *rand.Rand, q []float64) int {
	u := rng.Float64()
	acc := 0.0
	for i, p := range q {
		acc += p
		if u < acc {
			return i
		}
	}
	return len(q) - 1
}
