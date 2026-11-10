package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func cmdLabel(args []string) error {
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	storeDir := bindStore(fs)
	specID := fs.String("spec-id", "", "spec to search records in (all when omitted)")
	weight := fs.Float64("weight", 1, "label weight (below 1 for weak labels)")
	source := fs.String("source", "manual", "label source: ask | outcome | manual | ui | mcp etc. (calibrate sets replay and file)")
	last := fs.Bool("last", false, "instead of a trace_id, label the latest record of --spec-id")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	var rec *store.Record
	var truth string
	switch {
	case *last:
		if len(pos) != 1 || *specID == "" {
			return fmt.Errorf("usage: shikii label --last --spec-id ID <truth>")
		}
		r, err := st.LastRecord(*specID)
		if err != nil {
			return err
		}
		rec, truth = r, pos[0]
	default:
		if len(pos) != 2 {
			return fmt.Errorf("usage: shikii label <trace_id> <truth>  |  shikii label --last --spec-id ID <truth>")
		}
		r, err := st.FindRecord(*specID, pos[0])
		if err != nil {
			return err
		}
		rec, truth = r, pos[1]
	}
	labels, err := store.LabelsFor(rec, truth, *weight, *source)
	if err != nil {
		return err
	}
	if err := st.AppendLabels(labels); err != nil { // write both rungs in one go (never just one of them)
		return err
	}
	for _, l := range labels {
		fmt.Fprintf(stdout, "labeled %s (%s, %s, %s rung) truth=%s weight=%.2f source=%s\n", l.TraceID, l.SpecID, l.Model, l.Rung, l.Truth, l.Weight, l.Source)
	}
	return nil
}

// labeled is one row of labeled data. Truth is true/false or "yes"/"no" for noul, and an id/number for choice/score.
// If Raw (noul's raw probability) is present, the model is not called.
type labeled struct {
	Inputs map[string]string `json:"inputs"`
	Truth  json.RawMessage   `json:"truth"`
	Weight float64           `json:"weight,omitempty"`
	// Raw brings in a probability (a row that does not call the model). For noul it is one P(yes) number; for choice / score an {id: p} object.
	// Used to measure probabilities from another tool on the same scale, like the LoRA arm of the comparison bench.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// fileRowWeight is the weight of row i of a labeled file, by the rule of every other way a truth comes in (store.LabelFor): 0 or
// absent is a firm 1, a value in (0, 1] is kept, and anything else is an error naming the row. Every reader of such a file uses it
// (calibrate, check, and the truth bank of tune, bias and the wiki questions).
func fileRowWeight(i int, weight float64) (float64, error) {
	if weight == 0 {
		return 1, nil
	}
	if !(weight > 0 && weight <= 1) {
		return 0, fmt.Errorf("row %d: weight must be in (0, 1], got %g (1 = a firm truth; less = a weak one)", i, weight)
	}
	return weight, nil
}

// rawFromRow turns a row's raw into a distribution, or nil if absent. A key not among the spec's options is an error (never silently dropped).
func rawFromRow(sp *spec.Spec, raw json.RawMessage) (map[string]float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var p float64
	if err := json.Unmarshal(raw, &p); err == nil {
		if sp.Type != spec.Noul {
			return nil, fmt.Errorf("raw: a single number is only for noul; give {id: probability} for %s", sp.Type)
		}
		if p < 0 || p > 1 {
			return nil, fmt.Errorf("raw: P(yes) must be in 0..1, got %g", p)
		}
		return map[string]float64{"yes": p, "no": 1 - p}, nil
	}
	var dist map[string]float64
	if err := json.Unmarshal(raw, &dist); err != nil {
		return nil, fmt.Errorf("raw: want a number (noul) or {id: probability}: %w", err)
	}
	// All keys must be present (noul needs yes; no is filled in), each value in 0..1, summing close to 1. Missing or out-of-range values are never silently set to 0 (audit round 4 item 9)
	if sp.Type == spec.Noul {
		if _, ok := dist["yes"]; !ok {
			return nil, errors.New("raw: noul needs \"yes\"")
		}
		if _, ok := dist["no"]; !ok {
			dist["no"] = 1 - dist["yes"]
		}
	}
	sum := 0.0
	for k, v := range dist {
		if !sp.HasTruthKey(k) {
			return nil, fmt.Errorf("raw: %q is not an option of this spec", k)
		}
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("raw: %q must be in 0..1, got %g", k, v)
		}
		sum += v
	}
	for _, id := range sp.TruthKeys() {
		if _, ok := dist[id]; !ok {
			return nil, fmt.Errorf("raw: missing %q (give every option's probability)", id)
		}
	}
	if sum < 0.98 || sum > 1.02 {
		return nil, fmt.Errorf("raw: probabilities sum to %.3f, want 1", sum)
	}
	return dist, nil
}

func parseTruth(t spec.Type, raw json.RawMessage) (string, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if t != spec.Noul {
			return "", fmt.Errorf("%s truth must be a string, got %s", t, raw)
		}
		if b {
			return "yes", nil
		}
		return "no", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("truth must be bool or string, got %s", raw)
	}
	// a noul truth is yes or no, as in store.LabelFor. Any other string ("true", "Yes") used to count as "no" (0.9.4 audit)
	if t == spec.Noul && s != "yes" && s != "no" {
		return "", fmt.Errorf("noul truth must be yes or no (or true / false), got %q", s)
	}
	return s, nil
}
