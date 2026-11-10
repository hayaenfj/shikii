package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func cmdHash(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: shikii hash <spec.yaml>\n  prints the spec's hash (question, options, template; not display/variants/calibration)")
	}
	sp, err := spec.Load(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, sp.Hash())
	return nil
}

// cmdCheck checks whether the spec's calibration block is still valid. Meant to run in CI; exits 1 on any violation.
//   - the calibration's spec_hash matches the current hash (the wording was not changed while keeping the constants)
//   - the calibration was not written by hand (method is known and the keys are complete)
//   - with the labels (store + raw rows of eval.labeled), precision at the calibrated act threshold is at least act
//   - for noul, positive and negative are not inverted
func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	storeDir := bindStore(fs)
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("usage: shikii check <spec.yaml>...")
	}
	var violations int
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			fmt.Fprintf(stdout, "FAIL %s: %v\n", path, err)
			violations++
			continue
		}
		for _, v := range checkSpec(sp, store.Store{Dir: *storeDir}) {
			fmt.Fprintf(stdout, "FAIL %s: %s\n", sp.ID, v)
			violations++
		}
		fmt.Fprintf(stdout, "ok   %s hash=%s calibrations=%d\n", sp.ID, sp.Hash(), len(sp.Calibration))
	}
	if violations > 0 {
		return fmt.Errorf("check: %d violation(s)", violations)
	}
	return nil
}

// checkSpec lists the violations of one spec.
func checkSpec(sp *spec.Spec, st store.Store) []string {
	var out []string
	h := sp.Hash()
	base := sp
	for _, c := range sp.Calibration {
		key := fmt.Sprintf("calibration(model=%s)", c.Model)
		sp := base // the spec for this line's rung (a context-rung line is verified with the context rung's hash and truths)
		if c.SpecHash == base.ContextHash() {
			sp = base.ContextSpec()
			key = fmt.Sprintf("calibration(model=%s, context rung)", c.Model)
		} else if c.SpecHash != h {
			out = append(out, fmt.Sprintf("%s: stale spec_hash %s (current %s): the question/template/options changed after calibration; re-run calibrate", key, c.SpecHash, h))
			continue
		}
		switch c.Method {
		case "identity":
		case "platt":
			if sp.Type != spec.Noul || c.Temperature <= 0 {
				out = append(out, key+": platt requires noul and temperature > 0")
				continue
			}
		case "vector":
			if sp.Type == spec.Noul || c.Temperature <= 0 {
				out = append(out, key+": vector requires choice/score and temperature > 0")
				continue
			}
		default:
			out = append(out, fmt.Sprintf("%s: unknown method %q (calibration blocks are generated, not hand-written)", key, c.Method))
			continue
		}
		// Reject hand-written blocks: what calibrate never writes (n below min_n, no date) does not pass (audit S5)
		if c.N < sp.Eval.MinN {
			out = append(out, fmt.Sprintf("%s: n=%d < eval.min_n=%d (calibrate would not have written this block; --force or hand-written)", key, c.N, sp.Eval.MinN))
			continue
		}
		if strings.TrimSpace(c.FittedAt) == "" {
			out = append(out, key+": fitted_at is empty (calibration blocks are generated, not hand-written)")
			continue
		}
		labels, err := collectLabels(sp, st, c.Model, c.FittedOn)
		if err != nil {
			out = append(out, key+": "+err.Error())
			continue
		}
		if len(labels) == 0 {
			out = append(out, key+": no labels to verify against (store or eval.labeled)")
			continue
		}
		// A line drawn before 0.30.7 could lift an option that was never the truth (calibrate now caps it): no label ever checked what
		// the line does to that option, so its precision at act says nothing about it
		if option, bias, mean, lifted := liftedWithoutTruth(c, labels); lifted {
			out = append(out, fmt.Sprintf("%s: option %q is the truth in none of the %d labels, but its bias %.3f is above the mean bias %.3f of the options that are; a few percent on it can become an act decision. Re-run calibrate --write for this model (calibrate no longer lifts such an option)", key, option, len(labels), bias, mean))
		}
		prec, inverted, _ := evaluateCalibration(sp, c, labels)
		if prec < sp.Thresholds.Act {
			out = append(out, fmt.Sprintf("%s: precision at act is %.3f < thresholds.act %.2f on %d labels", key, prec, sp.Thresholds.Act, len(labels)))
		}
		// Also measure with human-decided truths only. Machine truths (context: upper-rung answers) can be wrong in the same direction as the same model, so the act precision guarantee comes from the human ones.
		// Sampling (1 in 20 act decisions shows up in the answer queue) builds the sample. Until min_n is reached only the count is reported; after that, falling below act fails.
		var human []store.Label
		for _, l := range labels {
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		hPrec, _, hAct := evaluateCalibration(sp, c, human)
		fmt.Fprintf(stdout, "note %s: %s: human-checked act decisions: %d (precision %.3f)\n", sp.ID, key, hAct, hPrec)
		if hAct >= sp.Eval.MinN && hPrec < sp.Thresholds.Act {
			out = append(out, fmt.Sprintf("%s: precision at act on human-checked decisions is %.3f < thresholds.act %.2f on %d act decisions (machine truths made it look like %.3f)", key, hPrec, sp.Thresholds.Act, hAct, prec))
		}
		if inverted {
			out = append(out, key+": the probabilities do not tell the answers apart (the AUC's 95% interval reaches 0.5)")
		}
	}
	return out
}

// liftedWithoutTruth reports the option of a vector line with the highest bias among those that no label has as its truth, when that
// bias is above the mean bias of the options that are a truth. The truths are counted like calibrate counts them (the latest truth
// per input; "!option" says what the answer is not and names no truth). A line whose options all have truths, or that has none with
// a truth, is never reported.
func liftedWithoutTruth(c spec.Calibration, labels []store.Label) (option string, bias, mean float64, lifted bool) {
	if c.Method != "vector" || len(c.Biases) == 0 {
		return "", 0, 0, false
	}
	seen := map[string]bool{}
	for _, l := range store.LatestPerInput(labels) {
		if _, ok := c.Biases[l.Truth]; ok {
			seen[l.Truth] = true
		}
	}
	if len(seen) == 0 || len(seen) == len(c.Biases) {
		return "", 0, 0, false
	}
	for k := range seen {
		mean += c.Biases[k]
	}
	mean /= float64(len(seen))
	options := make([]string, 0, len(c.Biases))
	for k := range c.Biases {
		options = append(options, k)
	}
	sort.Strings(options)
	for _, k := range options {
		if !seen[k] && c.Biases[k] > mean+1e-6 && (!lifted || c.Biases[k] > bias) {
			option, bias, lifted = k, c.Biases[k], true
		}
	}
	return option, bias, mean, lifted
}

// collectLabels combines store labels with eval.labeled rows that have raw (noul). The model is not called.
// Raw rows do not say which model's probabilities they are, so use them only when the calibration was fitted on the same file (fitted_on matches).
func collectLabels(sp *spec.Spec, st store.Store, model, fittedOn string) ([]store.Label, error) {
	all, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return nil, err
	}
	labels := store.LatestPerInput(all) // counted like calibrate (the latest truth per input)
	if sp.Eval.Labeled == "" || sp.Eval.Labeled != fittedOn {
		return labels, nil
	}
	b, err := os.ReadFile(sp.Eval.Labeled)
	if err != nil {
		if os.IsNotExist(err) {
			return labels, nil
		}
		return nil, err
	}
	var rows []labeled
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w", sp.Eval.Labeled, err)
	}
	for i, r := range rows {
		given, err := rawFromRow(sp, r.Raw)
		if err != nil {
			return nil, err
		}
		if given == nil {
			continue // rows that need the model are not used in check
		}
		truth, err := parseTruth(sp.Type, r.Truth)
		if err != nil {
			return nil, err
		}
		w, err := fileRowWeight(i, r.Weight) // outside (0, 1]: refused, as calibrate refuses the same row
		if err != nil {
			return nil, err
		}
		labels = append(labels, store.Label{SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: model,
			Raw: given, Truth: truth, Weight: w, Source: "file"})
	}
	return labels, nil
}

// evaluateCalibration applies the calibration and gives precision at the act threshold. For noul it also checks inversion.
func evaluateCalibration(sp *spec.Spec, c spec.Calibration, labels []store.Label) (precision float64, inverted bool, nAct int) {
	probs, truths, weights := make([]float64, len(labels)), make([]bool, len(labels)), make([]float64, len(labels))
	switch sp.Type {
	case spec.Noul:
		pl := calib.Platt{Bias: c.Bias, Temperature: c.Temperature}
		samples := make([]calib.Sample, len(labels))
		for i, l := range labels {
			d := calib.Logit(l.Raw["yes"])
			samples[i] = calib.Sample{Delta: d, Truth: l.Truth == "yes", Weight: l.Weight}
			p := l.Raw["yes"]
			if c.Method == "platt" {
				p = pl.Apply(d)
			}
			probs[i], truths[i], weights[i] = p, l.Truth == "yes", l.Weight
		}
		disc, ok := calib.AUC(probs, truths, weights)
		inverted = ok && disc.Low <= 0.5 // no clear signal (0.10: an overlap alone no longer fails the check)
	default:
		rawP, rawRight := make([]float64, len(labels)), make([]bool, len(labels))
		for i, l := range labels {
			dist := l.Raw
			if c.Method == "vector" {
				dist = calib.Vector{Temperature: c.Temperature, Biases: c.Biases}.Apply(l.Raw)
			}
			_, p, correct := judge(sp, dist, l.Truth)
			probs[i], truths[i], weights[i] = p, correct, l.Weight
			_, rawP[i], rawRight[i] = judge(sp, l.Raw, l.Truth)
		}
		// measured on the raw probabilities, as calibrate does (verifyTop): vector scaling can reorder the answers, and the check must not
		// fail a line calibrate drew, or pass one it refused, by measuring something else (0.10)
		disc, ok := calib.AUC(rawP, rawRight, weights)
		inverted = ok && disc.Low <= 0.5 // the top answer's probability does not tell right from wrong (0.10, as for noul)
	}
	for _, p := range probs {
		if p >= sp.Thresholds.Act {
			nAct++ // number of decisions in the act band after applying the line (the sample size when measuring precision on human truths only)
		}
	}
	return precisionAt(sp.Thresholds.Act, probs, truths, weights), inverted, nAct
}

// judge is one label's "probability used for the band" and "whether that answer was right".
//   - choice: the top probability; correct if top == truth ("!x" means anything but x)
//   - score with thresholds.level: P(level ≥ k); correct if the truth's level ≥ k (the event happened)
//   - score without level: same as choice
func judge(sp *spec.Spec, dist map[string]float64, truth string) (top string, p float64, correct bool) {
	top, p = argmax(dist)
	if sp.Type == spec.Score && sp.Thresholds.Level > 0 && !store.Partial(truth) {
		p = decide.MassAtOrAbove(dist, sp.Thresholds.Level)
		lvl, err := strconv.Atoi(truth)
		return top, p, err == nil && lvl >= sp.Thresholds.Level
	}
	return top, p, store.Correct(top, truth)
}
