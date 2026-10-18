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

// cmdJudge は審判: 別の端点(より大きいモデル)に同じ spec を聞き、正解の無い記録に正解を付ける。
// 先に人の正解との一致率を測り(審判の較正)、act に届く審判だけを使う。付ける正解は source=judge、重みは一致率。
// 人の仕事は、全部に答えることから、審判を抜き取りで検査することに縮む。審判の正解は機械の正解(HumanSource ではない)。
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

	// 1. 審判の較正: 人の正解(入力ごとに最新)に審判がどれだけ一致するか
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
	judgeModel := ""
	for _, l := range human {
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
	}
	if total == 0 {
		return errors.New("judge: could not ask the judge about any human truth")
	}
	agreement := agree / total
	fmt.Fprintf(stdout, "judge %s: agreement %.3f with %d human truths (threshold act=%.2f)\n", judgeModel, agreement, len(human), sp.Thresholds.Act)
	if agreement < sp.Thresholds.Act {
		return fmt.Errorf("judge: agreement %.3f < act %.2f; this judge is not trusted for %s", agreement, sp.Thresholds.Act, sp.ID)
	}

	// 2. 正解の無い記録に、審判が確信した答えを重み付きで付ける(記録の raw と model は判定モデルのもの。審判の答えは truth として入る)
	recs, err := st.Records(sp.ID, 0)
	if err != nil {
		return err
	}
	labeled, err := st.LabeledTraces(sp.ID)
	if err != nil {
		return err
	}
	written, unsure := 0, 0
	for _, rec := range recs {
		if _, done := labeled[rec.TraceID]; done || len(rec.Inputs) == 0 || !sp.LiveHash(rec.SpecHash) {
			continue
		}
		d, err := decide.Decide(ctx, ask, rec.Inputs, decide.Session{Tainted: sess.Tainted || rec.Tainted}, eps)
		if err != nil {
			return fmt.Errorf("judge: %w", err)
		}
		top, p, _ := judge(ask, d.Raw, "")
		if d.Band == decide.Abstain || p < sp.Thresholds.Act {
			unsure++ // 審判も迷った: 人へ
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
	fmt.Fprintf(stdout, "judge: labeled %d record(s) (weight %.3f, source judge); %d left to a person (judge unsure)\n", written, agreement, unsure)
	return nil
}
