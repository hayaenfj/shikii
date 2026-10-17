package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// shikii tune: 聞き方(質問文・選択肢の説明・本文の並べ方)の候補を、正解の貯金で全部採点して、勝った候補を新しい版として書く。
// 人が決めるのは正解だけ。候補は spec の variants: に書く(後で、モデルに提案させる --propose を足す)。
//
//   - 貯金 = ラベル付きファイルの行 + store にある本文付きの正解(入力ごとに最新 1 つ、文言やモデルを問わない)
//   - 候補ごとに、貯金の全入力を今のモデルに聞き、top-1 / Brier / precision@act / recall@act を出す
//   - 同じ入力で選んで同じ入力で測る甘さを見るため、k 分割で「k-1 で選び 1 で測る」の正解率(holdout)も出す
//   - 勝ち = top-1 が高い、同点なら Brier が低い。それでも同点なら今の文言(変えない)
//   - --write で勝った候補を <id>-v<version+1>.yaml に書き(元のファイルは触らない)、その場で較正まで引く

type bankRow struct {
	inputs  map[string]string
	truth   string
	weight  float64
	tainted bool
}

type candidate struct {
	name string
	sp   *spec.Spec
}

type candScore struct {
	name                  string
	model                 string
	p                     []float64
	correct               []bool
	weights               []float64
	acc, brier, prec, rec float64
	confusion             map[string]map[string]float64
}

func cmdTune(args []string) error {
	fs := flag.NewFlagSet("tune", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	labeledPath := fs.String("labeled", "", "正解の貯金に足すラベル付きファイル(省略時は spec の eval.labeled。none なら使わない)")
	write := fs.Bool("write", false, "勝った候補を新しい版として書き、その場で較正する")
	folds := fs.Int("folds", 5, "holdout の分割数")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	if len(sp.Variants) == 0 {
		return errors.New("tune: the spec has no variants: (candidate wordings). Add e.g.\n  variants:\n    - name: strict-other\n      options:\n        other: \"Not a request at all: greetings, thanks, acknowledgements\"")
	}
	if *labeledPath == "none" {
		*labeledPath = ""
	} else if *labeledPath == "" && sp.Eval.Labeled != "" {
		if _, err := os.Stat(sp.Eval.Labeled); err == nil {
			*labeledPath = sp.Eval.Labeled
		}
	}
	st := store.Store{Dir: *storeDir}
	bank, err := truthBank(sp, st, *labeledPath)
	if err != nil {
		return err
	}
	if len(bank) < sp.Eval.MinN {
		return fmt.Errorf("tune: %d truths < eval.min_n=%d; choosing a wording on fewer truths is guesswork. Collect more (shikii serve) or pass --labeled", len(bank), sp.Eval.MinN)
	}
	eps := ef.endpoints()
	if len(eps) == 0 {
		return errors.New("tune: no endpoint (pass --local URL or a key for --hosted)")
	}
	cands := []candidate{{"current", sp}}
	for _, v := range sp.Variants {
		cands = append(cands, candidate{v.Name, sp.WithVariant(v)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	fmt.Fprintf(stdout, "tune %s: %d truths × %d candidates (model asked %d times)\n", sp.ID, len(bank), len(cands), len(bank)*len(cands))
	scores := make([]candScore, len(cands))
	for i, c := range cands {
		s, err := scoreCandidate(ctx, c, bank, eps, ef.tainted)
		if err != nil {
			return fmt.Errorf("tune: candidate %s: %w", c.name, err)
		}
		scores[i] = s
	}
	// 表
	fmt.Fprintf(stdout, "%-14s %6s %7s %9s %8s  %s\n", "candidate", "top-1", "brier", "prec@act", "rec@act", "what changes")
	for i, s := range scores {
		fmt.Fprintf(stdout, "%-14s %6.3f %7.4f %9.3f %8.3f  %s\n", s.name, s.acc, s.brier, s.prec, s.rec, describeVariant(sp, cands[i].sp))
	}
	best := pickBest(scores, nil)
	// holdout: k-1 で選んで 1 で測る(選び方そのものの当たり)。「今の文言のまま」は選ばないので、その holdout は現状の正解率そのもの。
	k := *folds
	if k > len(bank) {
		k = len(bank)
	}
	holdout, haveHoldout := 0.0, false
	if k >= 2 {
		var hit, total float64
		for fold := 0; fold < k; fold++ {
			train := func(i int) bool { return i%k != fold }
			chosen := pickBest(scores, train)
			for i := range bank {
				if train(i) {
					continue
				}
				w := scores[chosen].weights[i]
				total += w
				if scores[chosen].correct[i] {
					hit += w
				}
			}
		}
		if total > 0 {
			holdout, haveHoldout = hit/total, true
			fmt.Fprintf(stdout, "holdout (%d-fold: pick on %d/%d, measure on 1/%d): top-1=%.3f  vs keeping current: %.3f\n", k, k-1, k, k, holdout, scores[0].acc)
		}
	}
	switch {
	case best == 0:
		fmt.Fprintln(stdout, "winner: current (nothing beats the current wording; ties keep it)")
		return nil
	case !haveHoldout:
		fmt.Fprintf(stdout, "winner: current (too few truths for a holdout; %s looked better on the full bank but that is not enough)\n", scores[best].name)
		return nil
	case holdout <= scores[0].acc+1e-12:
		// 貯金全体では勝って見えても、選び方の当たりが現状を上回らない = まぐれの範囲。変えない
		fmt.Fprintf(stdout, "winner: current (%s is better on the full bank, %.3f vs %.3f, but the holdout %.3f does not beat keeping current %.3f: not enough evidence to change the wording. More truths will settle it)\n",
			scores[best].name, scores[best].acc, scores[0].acc, holdout, scores[0].acc)
		return nil
	}
	fmt.Fprintf(stdout, "winner: %s  top-1 %.3f → %.3f, brier %.4f → %.4f\n", scores[best].name, scores[0].acc, scores[best].acc, scores[0].brier, scores[best].brier)
	printConfusion(scores[best].confusion)
	if !*write {
		fmt.Fprintln(stdout, "dry run: pass --write to adopt it as a new version and calibrate it")
		return nil
	}
	newPath, err := writeTunedVersion(sp, cands[best], scores[0], scores[best])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (version %d). calibrating it now …\n", newPath, sp.Version+1)
	lab := *labeledPath
	if lab == "" {
		lab = "none"
	}
	return runCalibrate(newPath, calibrateOpts{ef: ef, storeDir: *storeDir, labeled: lab, bins: 10, write: true})
}

// truthBank は正解の貯金。ファイルの行 + store の本文付きの正解(入力ごとに最新 1 つ。文言・モデルは問わない)。
func truthBank(sp *spec.Spec, st store.Store, labeledPath string) ([]bankRow, error) {
	var rows []bankRow
	seen := map[string]bool{}
	if labeledPath != "" {
		b, err := os.ReadFile(labeledPath)
		if err != nil {
			return nil, err
		}
		var file []labeled
		if err := json.Unmarshal(b, &file); err != nil {
			return nil, fmt.Errorf("%s: %w", labeledPath, err)
		}
		for i, r := range file {
			if len(r.Inputs) == 0 {
				continue // raw だけの行は聞き直せない
			}
			truth, err := parseTruth(sp.Type, r.Truth)
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i, err)
			}
			h := store.HashInputs(r.Inputs)
			if seen[h] {
				continue
			}
			seen[h] = true
			w := r.Weight
			if w <= 0 {
				w = 1
			}
			rows = append(rows, bankRow{inputs: r.Inputs, truth: truth, weight: w})
		}
	}
	_, all, err := st.Counts(sp.ID)
	if err != nil {
		return nil, err
	}
	texts, err := st.InputsByHash(sp.ID)
	if err != nil {
		return nil, err
	}
	for _, l := range store.LatestPerInput(all) {
		if seen[l.InputsHash] {
			continue
		}
		t, ok := texts[l.InputsHash]
		if !ok {
			continue
		}
		seen[l.InputsHash] = true
		rows = append(rows, bankRow{inputs: t.Inputs, truth: l.Truth, weight: l.Weight, tainted: t.Tainted})
	}
	return rows, nil
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

// pickBest は top-1 が高い候補、同点なら Brier が低い候補。それでも同点なら今の文言(添字 0)。
// include が与えられればその行だけで比べる(holdout 用)。
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
	return strings.Join(parts, "; ")
}

func trim(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// writeTunedVersion は勝った候補を <dir>/<id>-v<N+1>.yaml に書く。元のファイルは触らない。候補の一覧は引き継ぐ(次の tune のため)。
func writeTunedVersion(base *spec.Spec, win candidate, before, after candScore) (string, error) {
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
		base.ID, n.Version, time.Now().Format("2006-01-02"), filepath.Base(base.Path()), win.name, before.acc, after.acc, before.brier, after.brier, len(before.p))
	if err := os.WriteFile(path, []byte(header+string(b)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
