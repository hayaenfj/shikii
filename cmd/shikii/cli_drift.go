package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// cmdDrift checks, over the latest N labels, how much the Brier and the precision at act got worse than at calibration time.
// It warns and exits 1 if Brier is worse by +0.05 or more than at calibration, or precision falls below act.
func cmdDrift(args []string) error {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	storeDir := bindStore(fs)
	model := fs.String("model", "", "model name used as the calibration key (required)")
	window := fs.Int("window", 50, "how many recent labels to look at")
	tol := fs.Float64("tolerance", 0.05, "how much Brier degradation to allow")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	if *model == "" {
		return fmt.Errorf("drift: --model is required")
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	c := sp.CalibrationFor(*model)
	if c == nil {
		return fmt.Errorf("drift: no current calibration for model %s (hash %s); nothing to drift from", *model, sp.Hash())
	}
	if (c.Method == "platt" || c.Method == "vector") && !(c.Temperature > 0) {
		return fmt.Errorf("drift: calibration for %s has temperature %g (not > 0); the block is broken or hand-written, run check", *model, c.Temperature)
	}
	all, err := (store.Store{Dir: *storeDir}).Labels(sp.ID, sp.Hash(), *model)
	if err != nil {
		return err
	}
	labels := store.LatestPerInput(all)
	if len(labels) == 0 {
		return fmt.Errorf("drift: no labels in store for (hash=%s, model=%s)", sp.Hash(), *model)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].At.Before(labels[j].At) })
	if len(labels) > *window {
		labels = labels[len(labels)-*window:]
	}
	prec, brier, agree, askN := driftMetrics(sp, *c, labels)
	fmt.Fprintf(stdout, "spec=%s model=%s window=%d (calibrated %s on n=%d)\n", sp.ID, *model, len(labels), c.FittedAt, c.N)
	fmt.Fprintf(stdout, "brier:            at calibration=%.4f recent=%.4f\n", c.Brier, brier)
	fmt.Fprintf(stdout, "precision at act: recent=%.3f (act=%.2f)\n", prec, sp.Thresholds.Act)
	if askN > 0 {
		fmt.Fprintf(stdout, "ask agreement:    %.3f (share of ask-sourced labels where the user picked the model's top)\n", agree)
	} else {
		fmt.Fprintln(stdout, "ask agreement:    n/a (no ask-sourced labels in this window)")
	}
	var warn []string
	if brier > c.Brier+*tol {
		warn = append(warn, fmt.Sprintf("brier worsened by %.4f (> %.2f)", brier-c.Brier, *tol))
	}
	if prec < sp.Thresholds.Act {
		warn = append(warn, fmt.Sprintf("precision at act %.3f < %.2f", prec, sp.Thresholds.Act))
	}
	if len(warn) > 0 {
		fmt.Fprintln(stdout, "WARNING: "+strings.Join(warn, "; ")+". Re-run calibrate, or drop act to ask for this model.")
		return fmt.Errorf("drift: %d warning(s)", len(warn))
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

// driftMetrics gives precision@act, Brier and agreement at ask for the latest labels with calibration applied.
func driftMetrics(sp *spec.Spec, c spec.Calibration, labels []store.Label) (precision, brier, askAgreement, askN float64) {
	probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	var askHit float64
	for i, l := range labels {
		var p float64
		var correct bool
		if sp.Type == spec.Noul {
			p = l.Raw["yes"]
			if c.Method == "platt" {
				p = calib.Platt{Bias: c.Bias, Temperature: c.Temperature}.Apply(calib.Logit(p))
			}
			correct = l.Truth == "yes"
		} else {
			dist := l.Raw
			if c.Method == "vector" {
				dist = calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(l.Raw)
			}
			_, p, correct = judge(sp, dist, l.Truth)
		}
		probs[i], truths[i], weights[i] = p, correct, l.Weight
		if l.Source == "ask" {
			askN += l.Weight
			if correct {
				askHit += l.Weight
			}
		}
	}
	if askN > 0 {
		askAgreement = askHit / askN
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), calib.Brier(probs, truths, weights), askAgreement, askN
}
