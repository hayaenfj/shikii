package calib

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"
)

// The line of a model whose truths cover every option is the line the repository's sample spec carries
// (decisions/prompt-intent-v2.yaml, written by calibrate --write from the same labels): a fix for options without truths must not
// move the temperature or the biases of data that has none.
func TestFitVectorKeepsTheLineWhenEveryOptionWasTheTruth(t *testing.T) {
	f, err := os.Open("../../testdata/store/labels/prompt-intent.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	samples := map[string][]MultiSample{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var l struct {
			Model  string             `json:"model"`
			Raw    map[string]float64 `json:"raw"`
			Truth  string             `json:"truth"`
			Weight float64            `json:"weight"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Raw == nil {
			continue
		}
		samples[l.Model] = append(samples[l.Model], MultiSample{Raw: l.Raw, Truth: l.Truth, Weight: l.Weight})
	}
	for model, want := range map[string]Vector{
		"ggml-org/Kev-4B-GGUF:Q4_K_M": {Temperature: 0.44894918617400287, Biases: map[string]float64{
			"change_request": -0.36401015048582736, "operation": 0.587543414864858, "other": -0.5348494885571329, "question": -0.06151837617222262, "review": 0.37283460035032495}},
		"ternary-bonsai-2-27b": {Temperature: 0.6476247912548807, Biases: map[string]float64{
			"change_request": -0.9561972502792933, "operation": -0.5266943867505005, "other": 0.6100832685021256, "question": -0.3029058663577843, "review": 1.1757142348854523}},
	} {
		if len(samples[model]) != 62 {
			t.Fatalf("%s: the sample has 62 truths, got %d", model, len(samples[model]))
		}
		seen := map[string]bool{}
		for _, s := range samples[model] {
			seen[s.Truth] = true
		}
		if len(seen) != len(want.Biases) {
			t.Fatalf("%s: every option is the truth at least once in the sample: %v", model, seen)
		}
		got, err := FitVector(samples[model])
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(got.Temperature-want.Temperature) > 1e-6 {
			t.Errorf("%s: temperature %.6f, the sample line has %.6f", model, got.Temperature, want.Temperature)
		}
		for option, bias := range want.Biases {
			if math.Abs(got.Biases[option]-bias) > 1e-6 {
				t.Errorf("%s: bias of %s %.6f, the sample line has %.6f", model, option, got.Biases[option], bias)
			}
		}
	}
}
