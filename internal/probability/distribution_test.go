package probability

import (
	"math"
	"testing"
)

func TestArgmaxKeepsDeterministicBoundaryCases(t *testing.T) {
	for _, tc := range []struct {
		values map[string]float64
		key    string
		p      float64
	}{
		{nil, "", -1}, {map[string]float64{"z": .5, "a": .5}, "a", .5},
		{map[string]float64{"z": .7, "a": .2}, "z", .7},
		{map[string]float64{"a": math.NaN(), "b": .3}, "b", .3},
		{map[string]float64{"a": -2}, "", -1},
	} {
		k, p := Argmax(tc.values)
		if k != tc.key || p != tc.p {
			t.Fatalf("argmax=%q %v, want %q %v", k, p, tc.key, tc.p)
		}
	}
}

func TestSoftmaxKeepsTemperatureAndExtremeValues(t *testing.T) {
	for _, tc := range []struct {
		z           []float64
		temperature float64
		want        []float64
	}{
		{nil, 1, []float64{}}, {[]float64{0, 0}, 1, []float64{.5, .5}},
		{[]float64{1000, 1001}, 1, []float64{.2689414213699951, .7310585786300049}},
		{[]float64{1000, 1001}, .5, []float64{.11920292202211755, .8807970779778823}},
		{[]float64{-1000, 1000}, 1, []float64{0, 1}},
	} {
		got := Softmax(tc.z, tc.temperature)
		if len(got) != len(tc.want) {
			t.Fatal(got)
		}
		for i, v := range got {
			if math.IsNaN(v) || math.Abs(v-tc.want[i]) > 1e-15 {
				t.Fatalf("softmax=%v want %v", got, tc.want)
			}
		}
	}
	for _, z := range [][]float64{{math.Inf(1), 0}, {math.NaN(), 0}} {
		for _, v := range Softmax(z, 1) {
			if !math.IsNaN(v) {
				t.Fatalf("non-finite logits changed: %v", v)
			}
		}
	}
}
