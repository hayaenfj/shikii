package main

import (
	"fmt"
	"io"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// verifyTop verifies and calibrates choice / score (design §7.1).
//  1. With the raw distribution, measure accuracy against the top probability and precision at the act threshold
//  2. If precision is at least act, identity (= the mark that allows act on raw values) is ok
//  3. If not, fit vector scaling and measure again. If at least act after calibration, vector is ok;
//     if still not, return vector with ok=false (writing back needs --force)
func verifyTop(sp *spec.Spec, labels []store.Label, bins int, reports ...io.Writer) (spec.Calibration, bool, error) {
	stdout := reportWriter(reports...)
	// The report uses all labels; fitting (FitVector / CV) uses only labels whose correct option is known.
	// Partial labels ("!edit" = it was not edit) count for evaluating precision but cannot be used to learn the distribution.
	ms := make([]calib.MultiSample, len(labels))
	var fit []calib.MultiSample
	partial := 0
	for i, l := range labels {
		ms[i] = calib.MultiSample{Raw: l.Raw, Truth: l.Truth, Weight: l.Weight}
		if store.Partial(l.Truth) {
			partial++
		} else {
			fit = append(fit, ms[i])
		}
	}
	if partial > 0 {
		fmt.Fprintf(stdout, "labels: %d full, %d partial (\"!option\"; used for precision, not for fitting)\n", len(fit), partial)
	}
	rawRep := topReport(sp, ms, nil)
	fmt.Fprintf(stdout, "raw:        top-1 accuracy=%.3f brier(top)=%.4f ece(top)=%.4f\n", rawRep.acc, rawRep.brier, rawRep.ece)
	printThresholdReport(sp.Thresholds, rawRep.pTop, rawRep.correct, rawRep.weights, stdout)
	printReliability("top, raw", rawRep.pTop, rawRep.correct, rawRep.weights, bins, stdout)
	printConfusion(rawRep.confusion, stdout)
	identityOK := rawRep.precAct >= sp.Thresholds.Act
	identity := spec.Calibration{Method: "identity", PrecisionAt: rawRep.precAct, Brier: rawRep.brier, ECE: rawRep.ece}
	// the same rule as yes/no questions (0.10): when the top answer's probability does not tell right from wrong (the AUC's 95% interval
	// reaches 0.5), no line, even when the share of right answers alone passes the precision at act
	noSignal := false
	if disc, ok := calib.AUC(rawRep.pTop, rawRep.correct, rawRep.weights); ok {
		fmt.Fprintf(stdout, "discrimination (top answer right or wrong): AUC=%.3f (95%%: %.3f-%.3f) on %d right / %d wrong\n", disc.AUC, disc.Low, disc.High, disc.Pos, disc.Neg)
		noSignal = disc.Low <= 0.5
	}
	refuseNoSignal := func(c spec.Calibration) (spec.Calibration, bool, error) { // after the whole report, so the numbers are still shown
		fmt.Fprintln(stdout, "WARNING: the top answer's probability does not tell right from wrong (the AUC's 95% interval reaches 0.5); a calibration cannot fix this. Change the question or the model.")
		return c, false, nil
	}

	// vector looks too good in-sample, so adopt or reject it by the k-fold CV metrics.
	v, err := calib.FitVector(fit)
	if err != nil {
		if noSignal {
			return refuseNoSignal(identity)
		}
		if identityOK {
			fmt.Fprintf(stdout, "identity: raw precision at act %.3f >= act (vector scaling could not be fitted: %v)\n", rawRep.precAct, err)
			return identity, true, nil
		}
		return spec.Calibration{}, false, fmt.Errorf("vector scaling: %w", err)
	}
	calRep := topReport(sp, ms, &v)
	cvPrec, cvRec := crossValidateVector(sp, ms, 5) // score on all labels (partial ones included); fit only on complete labels (split inside the function)
	fmt.Fprintf(stdout, "vector:     temperature=%.4f biases=%s\n", v.Temperature, fmtDist(v.Biases))
	fmt.Fprintf(stdout, "calibrated: top-1 accuracy=%.3f brier(top)=%.4f ece(top)=%.4f (in-sample)\n", calRep.acc, calRep.brier, calRep.ece)
	printThresholdReport(sp.Thresholds, calRep.pTop, calRep.correct, calRep.weights, stdout)
	printReliability("top, calibrated", calRep.pTop, calRep.correct, calRep.weights, bins, stdout)
	printConfusion(calRep.confusion, stdout)
	fmt.Fprintf(stdout, "vector 5-fold CV at act: precision=%.3f recall=%.3f\n", cvPrec, cvRec)
	vectorOK := cvPrec >= sp.Thresholds.Act
	vector := spec.Calibration{
		Method: "vector", Temperature: v.Temperature, Biases: v.Biases,
		PrecisionAt: cvPrec, Brier: calRep.brier, ECE: calRep.ece,
	}

	// Choose: among candidates meeting precision@act, the one with higher recall@act. On a tie, the simpler identity.
	switch {
	case noSignal:
		return refuseNoSignal(identity)
	case identityOK && vectorOK && cvRec > rawRep.recAct:
		fmt.Fprintf(stdout, "choose vector: both satisfy act; vector recall %.3f > identity recall %.3f\n", cvRec, rawRep.recAct)
		return vector, true, nil
	case identityOK:
		fmt.Fprintf(stdout, "choose identity: raw precision at act %.3f >= act (recall %.3f)\n", rawRep.precAct, rawRep.recAct)
		return identity, true, nil
	case vectorOK:
		fmt.Fprintf(stdout, "choose vector: CV precision at act %.3f >= act (identity was %.3f)\n", cvPrec, rawRep.precAct)
		return vector, true, nil
	default:
		fmt.Fprintf(stdout, "WARNING: neither raw (%.3f) nor vector-calibrated (CV %.3f) precision at act reaches thresholds.act (%.2f): act must not be allowed for this model.\n", rawRep.precAct, cvPrec, sp.Thresholds.Act)
		return vector, false, nil
	}
}
