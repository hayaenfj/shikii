package main

import (
	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// crossValidateVector fits vector with k folds and sums precision / recall at the act threshold on the held-out folds.
func crossValidateVector(sp *spec.Spec, ms []calib.MultiSample, k int) (precision, recall float64) {
	// Scoring uses the same set as identity / check (all labels, including partial "!x") and the same event (judge).
	// Fitting (FitVector) uses only complete labels whose correct option is known. Audit C1 / C4.
	if k < 2 || len(ms) < k {
		return 0, 0
	}
	var tp, fp, fn float64
	for fold := 0; fold < k; fold++ {
		var train, test []calib.MultiSample
		for i, s := range ms {
			switch {
			case i%k == fold:
				test = append(test, s)
			case !store.Partial(s.Truth):
				train = append(train, s)
			}
		}
		v, err := calib.FitVector(train)
		if err != nil {
			continue
		}
		for _, s := range test {
			_, p, correct := judge(sp, v.Apply(s.Raw), s.Truth)
			w := s.Weight
			if w <= 0 {
				w = 1
			}
			switch {
			case p >= sp.Thresholds.Act && correct:
				tp += w
			case p >= sp.Thresholds.Act && !correct:
				fp += w
			case correct:
				fn += w
			}
		}
	}
	if tp+fp > 0 {
		precision = tp / (tp + fp)
	}
	if tp+fn > 0 {
		recall = tp / (tp + fn)
	}
	return precision, recall
}

// report is a bundle of metrics against the top probability.
type report struct {
	pTop      []float64
	correct   []bool
	weights   []float64
	acc       float64
	brier     float64
	ece       float64
	precAct   float64
	recAct    float64
	confusion map[string]map[string]float64
}

// topReport collects the top probability, correctness and the confusion table (after applying calibration if any).
func topReport(sp *spec.Spec, ms []calib.MultiSample, v *calib.Vector) report {
	r := report{
		pTop: make([]float64, len(ms)), correct: make([]bool, len(ms)), weights: make([]float64, len(ms)),
		confusion: map[string]map[string]float64{},
	}
	var hit, total float64
	for i, s := range ms {
		dist := s.Raw
		if v != nil {
			dist = v.Apply(s.Raw)
		}
		top, p, correct := judge(sp, dist, s.Truth)
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		r.pTop[i], r.correct[i], r.weights[i] = p, correct, w
		if r.confusion[s.Truth] == nil {
			r.confusion[s.Truth] = map[string]float64{}
		}
		r.confusion[s.Truth][top] += w
		total += w
		if r.correct[i] {
			hit += w
		}
	}
	if total > 0 {
		r.acc = hit / total
	}
	r.brier = calib.Brier(r.pTop, r.correct, r.weights)
	r.ece = calib.ECE(r.pTop, r.correct, r.weights, 10)
	r.precAct = precisionAt(sp.Thresholds.Act, r.pTop, r.correct, r.weights)
	r.recAct = recallAt(sp.Thresholds.Act, r.pTop, r.correct, r.weights)
	return r
}

func recallAt(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tp, fn float64
	for i, p := range probs {
		if !truths[i] {
			continue
		}
		if p >= th {
			tp += weights[i]
		} else {
			fn += weights[i]
		}
	}
	if tp+fn == 0 {
		return 0
	}
	return tp / (tp + fn)
}

func precisionAt(th float64, probs []float64, truths []bool, weights []float64) float64 {
	var tp, fp float64
	for i, p := range probs {
		if p < th {
			continue
		}
		if truths[i] {
			tp += weights[i]
		} else {
			fp += weights[i]
		}
	}
	if tp+fp == 0 {
		return 0
	}
	return tp / (tp + fp)
}
