package main

import (
	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
)

// crossValidatePlatt fits Platt with k folds and sums precision@act, Brier and the share of "no" below ask on the held-out folds.
func crossValidatePlatt(sp *spec.Spec, samples []calib.Sample, k int) (precision, brier, discard float64) {
	if k < 2 || len(samples) < k {
		return 0, 1, 0
	}
	var probs []float64
	var truths []bool
	var weights []float64
	for fold := 0; fold < k; fold++ {
		var train, test []calib.Sample
		for i, s := range samples {
			if i%k == fold {
				test = append(test, s)
			} else {
				train = append(train, s)
			}
		}
		pl, err := calib.Fit(train)
		if err != nil {
			continue
		}
		for _, s := range test {
			probs = append(probs, pl.Apply(s.Delta))
			truths = append(truths, s.Truth)
			weights = append(weights, s.Weight)
		}
	}
	if len(probs) == 0 {
		return 0, 1, 0
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), calib.Brier(probs, truths, weights), precisionBelow(sp.Thresholds.Ask, probs, truths, weights)
}

// precisionBelow is the weighted share of "no" among the probabilities below th (the discard band). 0 when none is below: a band
// never seen is not trusted.
func precisionBelow(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tn, fn float64
	for i, p := range probs {
		if p >= th {
			continue
		}
		if truths[i] {
			fn += weights[i]
		} else {
			tn += weights[i]
		}
	}
	if tn+fn == 0 {
		return 0
	}
	return tn / (tn + fn)
}
