package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// cmdJudge is the judge: it asks another endpoint (a larger model) the same spec and gives truths to records that have none.
// It first measures agreement with the human truths (calibrating the judge) and uses only a judge that reaches act. The truths it gives have source=judge and a weight equal to the agreement rate.
// The person's job shrinks from answering everything to spot-checking the judge. The judge's truth is a machine truth (not a HumanSource).
func cmdJudge(args []string) error {
	fs := flag.NewFlagSet("judge", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	eps := ef.endpoints()
	if len(eps) == 0 {
		return errors.New("judge: pass --local URL (or a hosted key) for the judge endpoint")
	}
	st := store.Store{Dir: *storeDir}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	ask := sp.WithoutContext()
	sess := decide.Session{Tainted: ef.tainted}

	// 1. Calibrate the judge: how well it agrees with the human truths (latest per input)
	_, all, err := st.Counts(sp.ID)
	if err != nil {
		return err
	}
	var human []store.Label
	for _, l := range all {
		if store.HumanSource(l.Source) && len(l.Inputs) > 0 {
			human = append(human, l)
		}
	}
	human = store.LatestPerInput(human)
	if len(human) < sp.Eval.MinN {
		return fmt.Errorf("judge: %d human truths with text < eval.min_n=%d; the judge cannot be calibrated yet", len(human), sp.Eval.MinN)
	}
	var agree, total float64
	var agreeWrong, totalWrong float64 // agreement only on inputs the decision model got wrong (a hint whether a judge of the same family can fill the gaps)
	judgeModel := ""
	answered := map[string]bool{} // inputs with a human truth: the judge does not touch them (even if they were re-decided in another trace)
	for _, l := range human {
		answered[l.InputsHash] = true
		d, err := decide.Decide(ctx, ask, l.Inputs, decide.Session{Tainted: sess.Tainted || l.Tainted}, eps)
		if err != nil {
			return fmt.Errorf("judge: %w", err)
		}
		if d.Band == decide.Abstain {
			continue
		}
		judgeModel = d.Model
		_, _, correct := judge(ask, d.Raw, l.Truth)
		total += l.Weight
		if correct {
			agree += l.Weight
		}
		if _, _, modelRight := judge(ask, l.Raw, l.Truth); len(l.Raw) > 0 && !modelRight {
			totalWrong += l.Weight
			if correct {
				agreeWrong += l.Weight
			}
		}
	}
	if total == 0 {
		return errors.New("judge: could not ask the judge about any human truth")
	}
	agreement := agree / total
	fmt.Fprintf(stdout, "judge %s: agreement %.3f with %d human truths (threshold act=%.2f)\n", judgeModel, agreement, len(human), sp.Thresholds.Act)
	if totalWrong > 0 {
		fmt.Fprintf(stdout, "judge: agreement %.3f on inputs the decision model got wrong (%d of them; low means a judge of the same family shares the holes)\n", agreeWrong/totalWrong, int(totalWrong+0.5))
	} else {
		fmt.Fprintln(stdout, "judge: on inputs the decision model got wrong: none in the human truths yet")
	}
	if agreement < sp.Thresholds.Act {
		return fmt.Errorf("judge: agreement %.3f < act %.2f; this judge is not trusted for %s", agreement, sp.Thresholds.Act, sp.ID)
	}

	// 2. Give records without a truth the answer the judge was confident about, with a weight (the record's raw and model belong to the decision model; the judge's answer goes in as truth)
	recs, err := st.Records(sp.ID, 0)
	if err != nil {
		return err
	}
	labeled, err := st.LabeledTraces(sp.ID)
	if err != nil {
		return err
	}
	written, unsure, skippedHuman := 0, 0, 0
	for _, rec := range recs {
		if _, done := labeled[rec.TraceID]; done || len(rec.Inputs) == 0 || !sp.LiveHash(rec.SpecHash) {
			continue
		}
		if answered[rec.InputsHash] || (rec.Minimal != nil && answered[rec.Minimal.InputsHash]) {
			skippedHuman++ // an input a person answered: do not overwrite it with the judge's answer
			continue
		}
		d, err := decide.Decide(ctx, ask, rec.Inputs, decide.Session{Tainted: sess.Tainted || rec.Tainted}, eps)
		if err != nil {
			return fmt.Errorf("judge: %w", err)
		}
		top, p, _ := judge(ask, d.Raw, "")
		if d.Band == decide.Abstain || p < sp.Thresholds.Act {
			unsure++ // the judge was unsure too: leave it to the person
			continue
		}
		r := rec
		ls, err := store.LabelsFor(&r, top, agreement, "judge")
		if err != nil {
			continue
		}
		if err := st.AppendLabels(ls); err != nil {
			return err
		}
		written++
	}
	fmt.Fprintf(stdout, "judge: labeled %d record(s) (weight %.3f, source judge); %d left to a person (judge unsure); %d skipped (already answered by people)\n", written, agreement, unsure, skippedHuman)
	return nil
}
