package calib

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/probability"
)

// floorLabels is a fixed set of truths shaped like the hook's prompt-kind question: five options, the truths are
// edit or explain, the model is right most of the time with 0.70..0.85 on its top answer, and two options (run,
// decide) never get a probability above the floor a readout gives a label outside its top list (1e-6).
func floorLabels() []MultiSample {
	var out []MultiSample
	for i := 0; i < 60; i++ {
		truth, second := "edit", "explain"
		if i%3 == 0 {
			truth, second = "explain", "edit"
		}
		top := 0.70 + 0.05*float64(i%4)
		raw := map[string]float64{truth: top, second: 1 - top - 0.01 - 2e-6, "investigate": 0.01, "run": 1e-6, "decide": 1e-6}
		if i%8 == 0 { // the model is wrong now and then, on its least sure answers (top 0.70)
			raw[truth], raw[second] = raw[second], raw[truth]
		}
		out = append(out, MultiSample{Raw: raw, Truth: truth, Weight: 1})
	}
	return out
}

// An option the truths never showed above the floor must not win after calibration because the model gives it a
// few percent on a new input. The smoothing of the targets is there to keep such an option's bias from flying to
// minus infinity, not to lift it above the model's top answer.
func TestFitVectorDoesNotLiftAnOptionTheTruthsOnlySawAtTheFloor(t *testing.T) {
	v, err := FitVector(floorLabels())
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]float64{"edit": 0.80, "explain": 0.15, "investigate": 0.01, "run": 0.04, "decide": 1e-6}
	got := v.Apply(raw)
	top, p := probability.Argmax(got)
	if top != "edit" {
		t.Fatalf("the model answered edit at 0.80 and run at 0.04; calibrated top = %s at %.3f\n temperature=%.3f biases=%v\n calibrated=%v", top, p, v.Temperature, v.Biases, got)
	}
	if got["run"] >= 0.5 {
		t.Fatalf("run was 0.04 raw and is %.3f calibrated (biases=%v)", got["run"], v.Biases)
	}
}

// Guard: the smoothing still does what it is for. An option that is never the truth but that the model keeps
// giving a real probability is pushed down, and its bias stays bounded (it does not fly to minus infinity), so a
// later input the model is sure about can still reach it.
func TestFitVectorKeepsTheBiasOfAnOptionWithoutTruthsBounded(t *testing.T) {
	var samples []MultiSample
	for i := 0; i < 60; i++ {
		truth, second := "a", "b"
		if i%2 == 1 {
			truth, second = "b", "a"
		}
		samples = append(samples, MultiSample{Raw: map[string]float64{truth: 0.60, second: 0.10, "other": 0.30}, Truth: truth, Weight: 1})
	}
	v, err := FitVector(samples)
	if err != nil {
		t.Fatal(err)
	}
	if b := v.Biases["other"]; b >= 0 || b < -12 {
		t.Fatalf("an option the model over-uses must get a negative, bounded bias: %v (temperature %.3f)", v.Biases, v.Temperature)
	}
	sure := v.Apply(map[string]float64{"a": 0.0005, "b": 0.0005, "other": 0.999})
	if top, _ := probability.Argmax(sure); top != "other" {
		t.Fatalf("an input the model is sure is other must still come out as other: %v (biases %v)", sure, v.Biases)
	}
}

// alwaysRightLabels is a fixed set where the model's top answer (0.60..0.95) is always the truth, a or b, and a
// third option is never the truth and always gets 0.02.
func alwaysRightLabels(n int) []MultiSample {
	var out []MultiSample
	for i := 0; i < n; i++ {
		truth, second := "a", "b"
		if i%2 == 1 {
			truth, second = "b", "a"
		}
		top := 0.60 + 0.05*float64(i%8)
		out = append(out, MultiSample{Raw: map[string]float64{truth: top, second: 1 - top - 0.02, "c": 0.02}, Truth: truth, Weight: 1})
	}
	return out
}

// The same lift without the floor: when the fit sharpens (temperature below 1), an option that was never the truth
// and always got 0.02 must not become the answer for an input where the model gives it 0.15 and another 0.45.
func TestFitVectorDoesNotLiftAnOptionWithoutTruthsWhenItSharpens(t *testing.T) {
	v, err := FitVector(alwaysRightLabels(50))
	if err != nil {
		t.Fatal(err)
	}
	got := v.Apply(map[string]float64{"a": 0.45, "b": 0.40, "c": 0.15})
	if top, p := probability.Argmax(got); top != "a" {
		t.Fatalf("the model answered a at 0.45 and c at 0.15; calibrated top = %s at %.4f (temperature=%.3f biases=%v)", top, p, v.Temperature, v.Biases)
	}
}

// With more truths of the same kind the first step used to land where the loss is flat (probabilities floored at
// 1e-12) and the temperature ended next to zero: every later answer came out as probability 1.
func TestFitVectorTemperatureDoesNotCollapseOnLabelsTheModelGotAllRight(t *testing.T) {
	v, err := FitVector(alwaysRightLabels(300))
	if err != nil {
		t.Fatal(err)
	}
	if v.Temperature < 1e-3 {
		t.Fatalf("temperature collapsed to %g (biases=%v)", v.Temperature, v.Biases)
	}
	if got := v.Apply(map[string]float64{"a": 0.45, "b": 0.40, "c": 0.15}); got["a"] > 0.999 {
		t.Fatalf("a at 0.45 against b at 0.40 came out as %v (temperature=%g)", got, v.Temperature)
	}
}
