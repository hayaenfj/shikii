package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
)

func printConfusion(confusion map[string]map[string]float64, reports ...io.Writer) {
	stdout := reportWriter(reports...)
	fmt.Fprintln(stdout, "confusion (truth → predicted):")
	truths := make([]string, 0, len(confusion))
	for t := range confusion {
		truths = append(truths, t)
	}
	sort.Strings(truths)
	for _, t := range truths {
		fmt.Fprintf(stdout, "  %s: %s\n", t, fmtDist(confusion[t]))
	}
}

// printThresholdReport prints precision / recall at the act / ask thresholds and returns precision at act.
func printThresholdReport(t spec.Thresholds, probs []float64, truths []bool, weights []float64, reports ...io.Writer) float64 {
	stdout := reportWriter(reports...)
	report := func(name string, th float64) float64 {
		var tp, fp, fn float64
		for i, p := range probs {
			w := weights[i]
			switch {
			case p >= th && truths[i]:
				tp += w
			case p >= th && !truths[i]:
				fp += w
			case p < th && truths[i]:
				fn += w
			}
		}
		prec, rec := 0.0, 0.0
		if tp+fp > 0 {
			prec = tp / (tp + fp)
		}
		if tp+fn > 0 {
			rec = tp / (tp + fn)
		}
		fmt.Fprintf(stdout, "at %s>=%.2f: precision=%.3f recall=%.3f (tp=%.0f fp=%.0f fn=%.0f)\n", name, th, prec, rec, tp, fp, fn)
		return prec
	}
	precAct := report("act", t.Act)
	report("ask", t.Ask)
	return precAct
}

func printReliability(name string, probs []float64, truths []bool, weights []float64, bins int, reports ...io.Writer) {
	stdout := reportWriter(reports...)
	fmt.Fprintf(stdout, "reliability (%s):\n", name)
	for _, b := range calib.Reliability(probs, truths, weights, bins) {
		if b.N > 0 {
			fmt.Fprintf(stdout, "  [%.1f,%.1f) n=%4.0f conf=%.3f acc=%.3f\n", b.Lo, b.Hi, b.N, b.Confidence, b.Accuracy)
		}
	}
}

func argmax(m map[string]float64) (string, float64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bp := "", -1.0
	for _, k := range keys {
		if m[k] > bp {
			best, bp = k, m[k]
		}
	}
	return best, bp
}
