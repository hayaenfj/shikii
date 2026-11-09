package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// cmdBias reorders a choice's options, asks the same state again, and measures how much the answer moves with the order.
//   - flip rate: share of cases where the top changed when the order changed
//   - mean |Δp|: how much each option's probability moved
//   - position mass: average probability per position (shows a bias such as "leans to the first")
//
// Records nothing and calibrates nothing. The model does not generate text, so running permutations is cheap.
func cmdBias(args []string) error {
	fs := flag.NewFlagSet("bias", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	labeledPath := fs.String("labeled", "", "where to take states from (default: the spec's eval.labeled; without it, the text of truths in the store)")
	storeDir := bindStore(fs)
	rows := fs.Int("rows", 20, "maximum number of rows to use")
	perms := fs.Int("permutations", 5, "number of permutations per row")
	seed := fs.Int64("seed", 1, "random seed for the permutations")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	if sp.Type != spec.Choice {
		return fmt.Errorf("bias: only choice has an option order (spec type is %s)", sp.Type)
	}
	if *labeledPath == "" {
		*labeledPath = sp.Eval.Labeled
	}
	// States come from the same saved truths as calibrate / tune: the --labeled file and the text of truths in the store (latest per input)
	bank, _, err := truthBank(sp, store.Store{Dir: *storeDir}, *labeledPath)
	if err != nil {
		return err
	}
	if len(bank) == 0 {
		return fmt.Errorf("bias: no states to ask: no truths in %s and no --labeled file (shikii label / shikii import first)", *storeDir)
	}
	lab := make([]labeled, 0, len(bank))
	for _, row := range bank {
		truth, _ := json.Marshal(row.truth)
		lab = append(lab, labeled{Inputs: row.inputs, Truth: truth, Weight: row.weight})
	}
	if len(lab) > *rows {
		lab = lab[:*rows]
	}
	ep := decide.PickEndpoint(sp, decide.Session{Tainted: ef.tainted}, ef.endpoints())
	if ep == nil {
		return fmt.Errorf("bias: no permitted endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	rep, model, err := measureBias(ctx, sp, lab, ep.Client, *perms, *seed)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s rows=%d permutations=%d queries=%d\n", sp.ID, sp.Hash(), model, rep.rows, *perms, rep.queries)
	fmt.Fprintf(stdout, "flip rate (top changes with order): %.3f (%d/%d)\n", rep.flipRate, rep.flips, rep.compared)
	fmt.Fprintln(stdout, "mean |Δp| by option:")
	for _, o := range sp.Options {
		fmt.Fprintf(stdout, "  %-16s %.3f\n", o.ID, rep.meanAbsDelta[o.ID])
	}
	fmt.Fprintln(stdout, "mean Δp by position vs spec order (1 = listed first; + means that slot gets extra probability):")
	for i, m := range rep.positionMass {
		fmt.Fprintf(stdout, "  pos %d: %+.3f\n", i+1, m)
	}
	fmt.Fprintf(stdout, "position shift spread (max−min): %.3f\n", rep.positionSpread)
	if rep.flipRate > 0.10 || rep.positionSpread > 0.10 {
		fmt.Fprintln(stdout, "WARNING: the answer depends on option order. Shuffle options at query time or average over permutations before trusting act.")
		return fmt.Errorf("bias: order sensitivity above tolerance")
	}
	fmt.Fprintln(stdout, "ok")
	return nil
}

type biasReport struct {
	rows, queries, flips, compared int
	flipRate                       float64
	meanAbsDelta                   map[string]float64
	positionMass                   []float64
	positionSpread                 float64
}

// measureBias asks each row in the spec's order plus N permutations and tallies how much the order shakes the answer.
func measureBias(ctx context.Context, sp *spec.Spec, lab []labeled, c *systemone.Client, perms int, seed int64) (biasReport, string, error) {
	K := len(sp.Options)
	rep := biasReport{meanAbsDelta: map[string]float64{}, positionMass: make([]float64, K)}
	posN := make([]float64, K)
	sumAbs := map[string]float64{}
	var nAbs float64
	rng := rand.New(rand.NewSource(seed))
	model := ""
	canonical := make([]int, K)
	for i := range canonical {
		canonical[i] = i
	}
	// Position bias is measured by "how much the probability of the option placed at that position moved from when it was asked in the spec's order".
	// The raw average probability per position would be skewed by which option happened to land there.
	addPos := func(order []int, dist, base map[string]float64) {
		for pos, idx := range order {
			id := sp.Options[idx].ID
			rep.positionMass[pos] += dist[id] - base[id]
			posN[pos]++
		}
	}
	for _, r := range lab {
		state, missing := sp.Render(r.Inputs)
		if len(missing) > 0 {
			continue
		}
		base, m, err := decide.AskOrdered(ctx, sp, state, canonical, c)
		if err != nil {
			return rep, "", err
		}
		model = m
		rep.rows++
		rep.queries++
		baseTop, _ := argmax(base)
		for p := 0; p < perms; p++ {
			order := rng.Perm(K)
			dist, _, err := decide.AskOrdered(ctx, sp, state, order, c)
			if err != nil {
				return rep, "", err
			}
			rep.queries++
			rep.compared++
			addPos(order, dist, base)
			if top, _ := argmax(dist); top != baseTop {
				rep.flips++
			}
			for _, o := range sp.Options {
				sumAbs[o.ID] += math.Abs(dist[o.ID] - base[o.ID])
			}
			nAbs++
		}
	}
	if rep.compared > 0 {
		rep.flipRate = float64(rep.flips) / float64(rep.compared)
	}
	for _, o := range sp.Options {
		if nAbs > 0 {
			rep.meanAbsDelta[o.ID] = sumAbs[o.ID] / nAbs
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range rep.positionMass {
		if posN[i] > 0 {
			rep.positionMass[i] /= posN[i]
		}
		lo, hi = math.Min(lo, rep.positionMass[i]), math.Max(hi, rep.positionMass[i])
	}
	if K > 0 {
		rep.positionSpread = hi - lo
	}
	return rep, model, nil
}
