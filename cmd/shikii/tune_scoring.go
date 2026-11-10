package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// truthBank is the bank of truths: rows of the file + truths with text in the store (the latest one per input, regardless of wording or model).
func truthBank(sp *spec.Spec, st store.Store, labeledPath string) (rows []bankRow, skipped int, err error) {
	type entry struct {
		row bankRow
		at  time.Time
	}
	latest := map[string]entry{} // the latest truth per input (the same rule as calibrate's LatestPerInput). File rows are the oldest
	var order []string
	put := func(h string, row bankRow, at time.Time) {
		if !truthKnown(sp, row.truth) {
			skipped++ // e.g. a choice not in the current spec. Count it instead of silently dropping it
			return
		}
		if row.weight <= 0 || row.weight > 1 {
			row.weight = 1 // as in the store, the weight is in (0, 1]
		}
		cur, seen := latest[h]
		if !seen {
			order = append(order, h)
		}
		if !seen || !at.Before(cur.at) {
			latest[h] = entry{row, at}
		}
	}
	if labeledPath != "" {
		b, err := os.ReadFile(labeledPath)
		if err != nil {
			return nil, 0, err
		}
		var file []labeled
		if err := json.Unmarshal(b, &file); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", labeledPath, err)
		}
		for i, r := range file {
			// a file row outside (0, 1] is refused, not counted as a firm truth; also a row the bank does not use
			weight, err := fileRowWeight(i, r.Weight)
			if err != nil {
				return nil, 0, err
			}
			if len(r.Inputs) == 0 {
				continue // a row with only raw cannot be asked again
			}
			truth, err := parseTruth(sp.Type, r.Truth)
			if err != nil {
				return nil, 0, fmt.Errorf("row %d: %w", i, err)
			}
			put(store.HashInputs(r.Inputs), bankRow{inputs: r.Inputs, truth: truth, weight: weight, human: true}, time.Time{})
		}
	}
	_, all, err := st.Counts(sp.ID)
	if err != nil {
		return nil, 0, err
	}
	texts, err := st.InputsByHash(sp.ID)
	if err != nil {
		return nil, 0, err
	}
	eligible := store.WithoutUntrustedOutcome(all, store.OutcomeAgreement(all), sp.Thresholds.Act)
	for _, l := range store.LatestPerInput(eligible) {
		t, ok := texts[l.InputsHash]
		if !ok {
			continue
		}
		put(l.InputsHash, bankRow{inputs: t.Inputs, truth: l.Truth, weight: l.Weight, tainted: t.Tainted, human: store.HumanSource(l.Source)}, l.At)
	}
	for _, h := range order {
		rows = append(rows, latest[h].row)
	}
	return rows, skipped, nil
}

// truthKnown reports whether a truth means something in the current spec (a choice id, a partial label !id, a level name, yes/no).
func truthKnown(sp *spec.Spec, truth string) bool {
	truth = strings.TrimSpace(truth)
	switch sp.Type {
	case spec.Noul:
		return truth == "yes" || truth == "no"
	case spec.Score:
		for _, l := range sp.Levels {
			if truth == l || truth == "!"+l {
				return true
			}
		}
		return false
	default:
		for _, o := range sp.Options {
			if truth == o.ID || truth == "!"+o.ID {
				return true
			}
		}
		return false
	}
}

// scorable keeps only rows that can be scored: the required inputs are present and there is somewhere to send them. The rest are counted and dropped (one row does not stop tune).
func scorable(sp *spec.Spec, bank []bankRow, eps []decide.Endpoint, tainted bool) (kept []bankRow, dropped int) {
	for _, r := range bank {
		if _, missing := sp.Render(r.inputs); len(missing) > 0 {
			dropped++
			continue
		}
		if decide.PickEndpoint(sp, decide.Session{Tainted: tainted || r.tainted}, eps) == nil {
			dropped++
			continue
		}
		kept = append(kept, r)
	}
	return kept, dropped
}

func scoreCandidate(ctx context.Context, c candidate, bank []bankRow, eps []decide.Endpoint, tainted bool) (candScore, error) {
	s := candScore{name: c.name, p: make([]float64, len(bank)), correct: make([]bool, len(bank)), weights: make([]float64, len(bank)), confusion: map[string]map[string]float64{}}
	var hit, total float64
	for i, r := range bank {
		d, err := decide.Decide(ctx, c.sp, r.inputs, decide.Session{Tainted: tainted || r.tainted}, eps)
		if err != nil {
			return s, err
		}
		if d.Band == decide.Abstain {
			return s, fmt.Errorf("abstained: %s", d.Reason)
		}
		if s.model == "" {
			s.model = d.Model
		}
		top, p, correct := judge(c.sp, d.Raw, r.truth)
		s.p[i], s.correct[i], s.weights[i] = p, correct, r.weight
		if s.confusion[r.truth] == nil {
			s.confusion[r.truth] = map[string]float64{}
		}
		s.confusion[r.truth][top] += r.weight
		total += r.weight
		if correct {
			hit += r.weight
		}
	}
	if total > 0 {
		s.acc = hit / total
	}
	s.brier = calib.Brier(s.p, s.correct, s.weights)
	s.prec = precisionAt(c.sp.Thresholds.Act, s.p, s.correct, s.weights)
	s.rec = recallAt(c.sp.Thresholds.Act, s.p, s.correct, s.weights)
	return s, nil
}

// pickBest is the candidate with the higher top-1; on a tie, lower Brier. Still tied, the current wording (index 0).
// If include is given, compare only on those rows (for holdout).
func pickBest(scores []candScore, include func(i int) bool) int {
	acc := func(s candScore) (float64, float64) {
		var hit, total, bs, bw float64
		for i := range s.p {
			if include != nil && !include(i) {
				continue
			}
			w := s.weights[i]
			total += w
			if s.correct[i] {
				hit += w
			}
			y := 0.0
			if s.correct[i] {
				y = 1
			}
			bs += w * (s.p[i] - y) * (s.p[i] - y)
			bw += w
		}
		if total == 0 {
			return 0, 1
		}
		return hit / total, bs / bw
	}
	best := 0
	bestAcc, bestBrier := acc(scores[0])
	for i := 1; i < len(scores); i++ {
		a, b := acc(scores[i])
		if a > bestAcc+1e-12 || (abs(a-bestAcc) <= 1e-12 && b < bestBrier-1e-12) {
			best, bestAcc, bestBrier = i, a, b
		}
	}
	return best
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func describeVariant(base, c *spec.Spec) string {
	if c == base {
		return "(as is)"
	}
	var parts []string
	if c.Question != base.Question {
		parts = append(parts, "question: "+trim(c.Question, 60))
	}
	if c.State.Template != base.State.Template {
		parts = append(parts, "template changed")
	}
	for i := range c.Options {
		if i < len(base.Options) && c.Options[i].Description != base.Options[i].Description {
			parts = append(parts, c.Options[i].ID+": "+trim(c.Options[i].Description, 40))
		}
	}
	for i := range c.Levels {
		if i < len(base.Levels) && c.Levels[i] != base.Levels[i] {
			parts = append(parts, fmt.Sprintf("level %d: %s", i, trim(c.Levels[i], 40)))
		}
	}
	if c.Layout != base.Layout {
		l := c.Layout
		if l == "" {
			l = "default"
		}
		parts = append(parts, "layout: "+l)
	}
	if !reflect.DeepEqual(c.Steps, base.Steps) {
		if len(c.Steps) == 0 {
			parts = append(parts, "asked as one choice again (no steps)")
		} else {
			parts = append(parts, fmt.Sprintf("decomposed into %d yes/no steps: %s", len(c.Steps), trim(c.Steps[0].Ask, 50)))
		}
	}
	return strings.Join(parts, "; ")
}

func trim(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// writeTunedVersion writes the winner to <dir>/<id>-v<N+1>.yaml. The original file is untouched. The list of candidates carries over (for the next tune).
func writeTunedVersion(base *spec.Spec, win candidate, before, after candidateResult, truths int) (string, error) {
	n := *win.sp
	n.Version = base.Version + 1
	n.Variants = base.Variants
	n.Calibration = nil
	dir := filepath.Dir(base.Path())
	path := filepath.Join(dir, fmt.Sprintf("%s-v%d.yaml", base.ID, n.Version))
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists; bump the version of %s first or remove the file", path, base.Path())
	}
	b, err := yaml.Marshal(&n)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("# %s v%d: adopted by `shikii tune` on %s from %s (variant %q).\n# top-1 %.3f → %.3f, brier %.4f → %.4f on %d truths. The calibration block below is written by calibrate; never by hand.\n",
		base.ID, n.Version, time.Now().Format("2006-01-02"), filepath.Base(base.Path()), win.name, before.TopOne, after.TopOne, before.Brier, after.Brier, truths)
	if err := os.WriteFile(path, []byte(header+string(b)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
