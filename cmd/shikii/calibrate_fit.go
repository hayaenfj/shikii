package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// discriminationOf is how well the raw probabilities rank the truths: p(yes) against the truth for noul, and the top answer's
// probability against whether it was right for the other types (the line acts on the top answer). ok=false with one side only.
func discriminationOf(sp *spec.Spec, labels []store.Label) (calib.Discrimination, bool) {
	if sp.Type == spec.Noul {
		probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
		for i, l := range labels {
			probs[i], truths[i], weights[i] = l.Raw["yes"], l.Truth == "yes", l.Weight
		}
		return calib.AUC(probs, truths, weights)
	}
	ms := make([]calib.MultiSample, len(labels))
	for i, l := range labels {
		ms[i] = calib.MultiSample{Raw: l.Raw, Truth: l.Truth, Weight: l.Weight}
	}
	rep := topReport(sp, ms, nil)
	return calib.AUC(rep.pTop, rep.correct, rep.weights)
}

// calibrateNoul fits Platt and prints the metrics (design §7). If positive and negative are inverted, ok=false.
func calibrateNoul(sp *spec.Spec, labels []store.Label, bins int, reports ...io.Writer) (spec.Calibration, bool, error) {
	stdout := reportWriter(reports...)
	samples := make([]calib.Sample, len(labels))
	rawProbs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	for i, l := range labels {
		p := l.Raw["yes"]
		samples[i] = calib.Sample{Delta: calib.Logit(p), Truth: l.Truth == "yes", Weight: l.Weight}
		rawProbs[i], truths[i], weights[i] = p, l.Truth == "yes", l.Weight
	}
	sep := calib.CheckSeparation(samples)
	pl, err := calib.Fit(samples)
	if errors.Is(err, calib.ErrOneSided) {
		fmt.Fprintln(stdout, "WARNING: every truth is on one side; a line needs both answers. Not written (tried again as truths grow).")
		return spec.Calibration{}, false, nil
	}
	if errors.Is(err, calib.ErrSlopeNotPositive) {
		// a higher probability goes with "no": the answers cannot be told apart, the same as an overlap (not written; the caller says why)
		fmt.Fprintln(stdout, "WARNING: a higher probability goes with \"no\" (fitted slope is not positive); this question and model cannot tell the answers apart. Change the question or the model.")
		return spec.Calibration{}, false, nil
	}
	if err != nil {
		return spec.Calibration{}, false, fmt.Errorf("calibrate: %w", err)
	}
	calProbs := make([]float64, len(samples))
	for i, s := range samples {
		calProbs[i] = pl.Apply(s.Delta)
	}
	fmt.Fprintf(stdout, "platt: bias=%.4f temperature=%.4f\n", pl.Bias, pl.Temperature)
	fmt.Fprintf(stdout, "brier: raw=%.4f calibrated=%.4f\n", calib.Brier(rawProbs, truths, weights), calib.Brier(calProbs, truths, weights))
	fmt.Fprintf(stdout, "ece:   raw=%.4f calibrated=%.4f (bins=%d)\n", calib.ECE(rawProbs, truths, weights, bins), calib.ECE(calProbs, truths, weights, bins), bins)
	fmt.Fprintf(stdout, "separation(Δ, weight>=1): pos_min=%.3f neg_max=%.3f overlap=%d (weak labels excluded: %d; an overlap alone does not refuse the line)\n", sep.PosMin, sep.NegMax, sep.Overlap, sep.Weak)
	if sep.Overlap > 0 {
		// Show which examples overlap. The text is not printed; the label can be traced by inputs_hash.
		fmt.Fprintln(stdout, "overlapping (truth raw weight inputs_hash):")
		for i, l := range labels {
			if l.Weight > 0 && l.Weight < 1 {
				continue
			}
			d := samples[i].Delta
			if (l.Truth == "yes" && d < sep.NegMax) || (l.Truth == "no" && d > sep.PosMin) {
				fmt.Fprintf(stdout, "  %-3s %.3f %.1f %s\n", l.Truth, l.Raw["yes"], l.Weight, l.InputsHash)
			}
		}
	}
	fmt.Fprintf(stdout, "boundaries after calibration: neg_max→%.3f (ask=%.2f) pos_min→%.3f (act=%.2f)\n",
		pl.Apply(sep.NegMax), sp.Thresholds.Ask, pl.Apply(sep.PosMin), sp.Thresholds.Act)
	printThresholdReport(sp.Thresholds, calProbs, truths, weights, stdout)
	printReliability("calibrated", calProbs, truths, weights, bins, stdout)
	// Overlapping answers are normal for a real model; the calibration and the precision at act below handle them. What no line can fix
	// is a ranking with no clear signal: the AUC's 95% interval reaches 0.5 (before 0.10 a single overlapping pair refused the line)
	disc, _ := discriminationOf(sp, labels)
	fmt.Fprintf(stdout, "discrimination: AUC=%.3f (95%%: %.3f-%.3f) on %d yes / %d no\n", disc.AUC, disc.Low, disc.High, disc.Pos, disc.Neg)
	if disc.Low <= 0.5 {
		fmt.Fprintln(stdout, "WARNING: the probabilities do not tell the answers apart (the AUC's 95% interval reaches 0.5); a calibration cannot fix this. Change the question or the model.")
		return spec.Calibration{Method: "platt", Bias: pl.Bias, Temperature: pl.Temperature,
			Brier: calib.Brier(calProbs, truths, weights), ECE: calib.ECE(calProbs, truths, weights, bins)}, false, nil
	}

	// Compare identity and platt. platt looks too good in-sample, so judge by the 5-fold CV Brier and precision@act.
	// For models whose raw probabilities are already calibrated (Jev, Kev's noul, etc.), platt does not improve, so choose identity.
	rawPrec := precisionAt(sp.Thresholds.Act, rawProbs, truths, weights)
	rawBrier := calib.Brier(rawProbs, truths, weights)
	cvPrec, cvBrier, cvDiscard := crossValidatePlatt(sp, samples, 5)
	fmt.Fprintf(stdout, "platt 5-fold CV: precision@act=%.3f brier=%.4f (raw: precision@act=%.3f brier=%.4f)\n", cvPrec, cvBrier, rawPrec, rawBrier)
	// the confident "no" side (p < ask): the share of real "no" among those. A question answered "yes" nine times in ten can rank well and
	// still put many "yes" below ask, so a "this looks wrong" notice is only given when this share reaches act too (hookDecide, 0.10)
	rawDiscard := precisionBelow(sp.Thresholds.Ask, rawProbs, truths, weights)
	fmt.Fprintf(stdout, "discard (p < ask): share of \"no\" raw=%.3f platt CV=%.3f\n", rawDiscard, cvDiscard)
	identityOK, plattOK := rawPrec >= sp.Thresholds.Act, cvPrec >= sp.Thresholds.Act
	identity := spec.Calibration{Method: "identity", PrecisionAt: rawPrec, PrecisionAtDiscard: rawDiscard, Brier: rawBrier, ECE: calib.ECE(rawProbs, truths, weights, bins)}
	// The Brier written to the spec is the CV value (in-sample is optimistic and too lenient as a baseline for drift; audit C6)
	platt := spec.Calibration{Method: "platt", Bias: pl.Bias, Temperature: pl.Temperature, PrecisionAt: cvPrec, PrecisionAtDiscard: cvDiscard,
		Brier: cvBrier, ECE: calib.ECE(calProbs, truths, weights, bins)}
	switch {
	case identityOK && plattOK && cvBrier < rawBrier:
		fmt.Fprintf(stdout, "choose platt: both satisfy act; CV brier %.4f < raw %.4f\n", cvBrier, rawBrier)
		return platt, true, nil
	case identityOK:
		fmt.Fprintf(stdout, "choose identity: raw precision at act %.3f >= act and platt does not improve brier (CV %.4f vs raw %.4f)\n", rawPrec, cvBrier, rawBrier)
		return identity, true, nil
	case plattOK:
		fmt.Fprintf(stdout, "choose platt: CV precision at act %.3f >= act (raw was %.3f)\n", cvPrec, rawPrec)
		return platt, true, nil
	default:
		fmt.Fprintf(stdout, "WARNING: neither raw (%.3f) nor platt-calibrated (CV %.3f) precision at act reaches thresholds.act (%.2f): act must not be allowed for this model.\n", rawPrec, cvPrec, sp.Thresholds.Act)
		return platt, false, nil
	}
}
