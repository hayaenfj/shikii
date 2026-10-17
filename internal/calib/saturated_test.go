package calib

import (
	"math"
	"testing"
)

// 2026-10-17 の実測(shikii-readout + Bonsai 2、はい/いいえ 32 組)。正負は完全に分かれ、
// 確率は端に寄っている(|Δ| ≈ 5〜6)。この形で Platt を当てると in-sample の Brier が
// 生 0.0205 → 0.107 に悪化した。平滑化した目標値(y+ ≈ 0.93, y− ≈ 0.06)への最尤なら
// 0.01 を超えるはずがない。飽和した σ で Newton 法が暴れていないか。
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
	// 平滑化目標(0.93 / 0.06)に寄せる分の悪化は 0.01 程度。それ以上なら最適化が壊れている。
	if fitted > raw+0.01 {
		t.Fatalf("fitted Brier %.4f is far worse than raw %.4f: the optimiser did not converge (platt=%+v)", fitted, raw, pl)
	}
	// 正例の最低(0.9703)は較正後も act(0.70)を超えているべき。分離したデータで recall を落とすのは誤り
	if p := pl.Apply(Logit(0.9703)); p < 0.70 {
		t.Fatalf("weakest positive dropped to %.3f after calibration (platt=%+v)", p, pl)
	}
}
