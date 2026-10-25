package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
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
	human   bool // 人(または結果)が決めた正解か。機械の正解(context / judge)は false。tune と wiki の証拠は人の数を添える
}

// humanTruths は貯金のうち人が決めた正解の数。
func humanTruths(bank []bankRow) int {
	n := 0
	for _, r := range bank {
		if r.human {
			n++
		}
	}
	return n
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
	var o tuneOpts
	o.ef.bind(fs)
	fs.StringVar(&o.storeDir, "store", ".shikii", "記録とラベルの置き場")
	fs.StringVar(&o.labeled, "labeled", "", "正解の貯金に足すラベル付きファイル(省略時は spec の eval.labeled。none なら使わない)")
	fs.BoolVar(&o.write, "write", false, "勝った候補を新しい版として書き、その場で較正する")
	fs.IntVar(&o.folds, "folds", 5, "holdout の分割数")
	fs.IntVar(&o.propose, "propose", 0, "提案役(チャット用 LLM)に候補を N 個作らせて spec の variants に足す")
	fs.StringVar(&o.proposer, "proposer", "", "提案役の URL(OpenAI 互換の /v1/chat/completions。本家 llama-server でも可)。pii: true の spec は手元(127.0.0.1)の提案役だけ")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "提案役のモデル名(空なら口の既定)")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	o.specPath = path
	_, err = runTune(o)
	return err
}

// tuneOpts は tune の指定。CLI と serve / mcp が同じ中身を使う。
type tuneOpts struct {
	output        io.Writer
	specPath      string
	ef            endpointFlags
	storeDir      string
	labeled       string
	write         bool
	folds         int
	propose       int
	proposer      string
	proposerModel string
	wikiDir       string // 空なら <store>/wiki(テストだけが別の場所を指す)
}

// 基本構成: shikii が作るものは全部 store(既定 .shikii/)の下。decisions/(spec)、wiki/(知識)、records/ と labels/(記録と正解)。
// プロジェクトルートに増えるのは .shikii/ だけ。wiki と spec の置き場に別のフラグは無い(store から決まる)。
func wikiDirOf(storeDir string) string  { return filepath.Join(storeDir, "wiki") }
func specsDirOf(storeDir string) string { return filepath.Join(storeDir, "decisions") }

// tuneResult は 1 回の tune の結果。wiki/runs/<name>.json にも残り、画面と MCP が読む。採用の判断もここに積む。
type tuneResult struct {
	SpecID        string            `json:"spec"`
	SpecPath      string            `json:"spec_path"`
	SpecHash      string            `json:"spec_hash"`
	Version       int               `json:"version"`
	Model         string            `json:"model"`
	Truths        int               `json:"truths"`
	At            string            `json:"at"`
	Run           string            `json:"run"`      // runs/ のファイル名(拡張子なし)
	RunPage       string            `json:"run_page"` // markdown の path
	Candidates    []candidateResult `json:"candidates"`
	Holdout       float64           `json:"holdout"`
	HaveHoldout   bool              `json:"have_holdout"`
	HoldoutWins   int               `json:"holdout_better"` // 測る側の fold で、選ばれた候補が当てて現状が外した行
	HoldoutLosses int               `json:"holdout_worse"`
	PValue        float64           `json:"p_value"` // 片側の符号検定。候補が現状と同じ実力なら、これだけ勝つ確率
	Winner        string            `json:"winner"`  // "current" か候補名
	Adoptable     bool              `json:"adoptable"`
	Decision      string            `json:"decision"`
	Lesson        string            `json:"lesson"`
	WinnerVariant *spec.Variant     `json:"winner_variant,omitempty"`
	WinnerWording *wording          `json:"winner_wording,omitempty"`
	Current       *wording          `json:"current_wording,omitempty"`
	Adopted       bool              `json:"adopted"`
	AdoptedPath   string            `json:"adopted_path,omitempty"`
	MeaningChecks []meaningCheck    `json:"meaning_checks,omitempty"`
	AutoAdopted   bool              `json:"auto_adopted,omitempty"`
	Reverted      bool              `json:"reverted,omitempty"`
}

type candidateResult struct {
	Name   string  `json:"name"`
	TopOne float64 `json:"top1"`
	Brier  float64 `json:"brier"`
	Prec   float64 `json:"prec_at_act"`
	Rec    float64 `json:"rec_at_act"`
	Change string  `json:"change"`
}

type wording struct {
	Question string            `json:"question"`
	Options  map[string]string `json:"options,omitempty"`
	Levels   []string          `json:"levels,omitempty"`
	Template string            `json:"template"`
	Steps    []spec.Step       `json:"steps,omitempty"`
}

type meaningCheck struct {
	At          string `json:"at"`
	Same        bool   `json:"same"`
	Note        string `json:"note,omitempty"`
	By          string `json:"by"`
	Verdict     string `json:"verdict,omitempty"`
	Lang        string `json:"lang,omitempty"` // 訳の言語(設定の言語。英語なら訳は無い)
	CurrentText string `json:"current_translation,omitempty"`
	Candidate   string `json:"candidate_translation,omitempty"`
	Difference  string `json:"difference,omitempty"`
}

func wordingOf(sp *spec.Spec) *wording {
	w := &wording{Question: sp.Question, Template: sp.State.Template, Levels: sp.Levels, Steps: sp.Steps}
	if len(sp.Options) > 0 {
		w.Options = map[string]string{}
		for _, o := range sp.Options {
			w.Options[o.ID] = o.Description
		}
	}
	return w
}

// runTune は tune の本体。報告は stdout に出し、結果を返す(wiki があれば runs/ に md と json を書く)。
func runTune(o tuneOpts) (*tuneResult, error) {
	stdout := reportWriter(o.output)
	sp, err := spec.Load(o.specPath)
	if err != nil {
		return nil, err
	}
	if o.folds <= 0 {
		o.folds = 5
	}
	if o.wikiDir == "" {
		o.wikiDir = wikiDirOf(o.storeDir)
	}
	if o.propose > 0 && o.proposer == "" {
		return nil, errors.New("tune: --propose needs --proposer URL (a chat endpoint, e.g. http://127.0.0.1:8087 for a local llama-server with a chat GGUF)")
	}
	if o.propose > 0 && sp.PIISensitive() && !isLoopback(o.proposer) {
		return nil, fmt.Errorf("tune: this spec has pii: true, so example texts may only go to a local proposer (127.0.0.1); %s is not local", o.proposer)
	}
	if len(sp.Variants) == 0 && o.propose == 0 {
		return nil, errors.New("tune: the spec has no variants: (candidate wordings). Add e.g.\n  variants:\n    - name: strict-other\n      options:\n        other: \"Not a request at all: greetings, thanks, acknowledgements\"\nor let a model propose some: --propose 3 --proposer http://127.0.0.1:8087")
	}
	labeledPath := o.labeled
	if labeledPath == "none" {
		labeledPath = ""
	} else if labeledPath == "" && sp.Eval.Labeled != "" {
		if _, err := os.Stat(sp.Eval.Labeled); err == nil {
			labeledPath = sp.Eval.Labeled
		}
	}
	st := store.Store{Dir: o.storeDir}
	bank, unknown, err := truthBank(sp, st, labeledPath)
	if err != nil {
		return nil, err
	}
	if unknown > 0 {
		fmt.Fprintf(stdout, "note: %d truth(s) skipped: their answer no longer exists in the spec\n", unknown)
	}
	eps := o.ef.endpoints()
	if len(eps) == 0 {
		return nil, errors.New("tune: no endpoint (pass --local URL or a key for --hosted)")
	}
	bank, unscorable := scorable(sp, bank, eps, o.ef.tainted)
	if unscorable > 0 {
		fmt.Fprintf(stdout, "note: %d truth(s) could not be scored (missing input, or no permitted endpoint for tainted text) and were left out\n", unscorable)
	}
	if len(bank) < sp.Eval.MinN {
		hint := "collect more truths (shikii serve, shikii label, shikii import), or for a trial lower eval.min_n in the spec to the truths you have"
		if labeledPath == "" {
			hint += "; truths in a file are read with --labeled FILE"
		}
		return nil, fmt.Errorf("tune: %d truths < eval.min_n=%d; choosing a wording on fewer truths is guesswork. %s", len(bank), sp.Eval.MinN, hint)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// 1. 今の聞き方を採点(提案役に混同表と外した実例を渡すため、先に)
	current, err := scoreCandidate(ctx, candidate{"current", sp.WithoutContext()}, bank, eps, o.ef.tainted)
	if err != nil {
		return nil, fmt.Errorf("tune: current: %w", err)
	}
	// 2. 提案(あれば): wiki を読ませて候補を作らせ、spec の variants に残す
	var rationales []string
	if o.propose > 0 {
		system, user := buildProposerPrompt(sp, current, bank, wikiContext(o.wikiDir, sp, current.model), o.propose, isLoopback(o.proposer))
		props, _, err := askProposer(ctx, o.proposer, o.proposerModel, system, user)
		if err != nil {
			return nil, fmt.Errorf("tune: %w", err)
		}
		added, skipped, notes := adoptProposals(sp, props)
		rationales = notes
		fmt.Fprintf(stdout, "proposer: %d proposal(s), %d added to variants, %d skipped\n", len(props), added, skipped)
		for _, n := range notes {
			fmt.Fprintln(stdout, "  "+n)
		}
		if added > 0 {
			// 書き戻すのは variants だけ。長い採点の間に別の端末が calibrate --write した線を、古いメモリの線で潰さないよう、ファイルを読み直して足す
			fresh, err := spec.Load(sp.Path())
			if err != nil {
				return nil, fmt.Errorf("tune: reload %s: %w", sp.Path(), err)
			}
			fresh.Variants = sp.Variants
			if err := fresh.Save(); err != nil {
				return nil, fmt.Errorf("tune: save proposals into %s: %w", sp.Path(), err)
			}
		}
		if len(sp.Variants) == 0 {
			return nil, errors.New("tune: nothing to score (every proposal was invalid)")
		}
	}
	// 候補は最小の質問の段だけで採点する(聞き方の比較。段を上がると文脈の段の答えが混ざる)。採用した版は context を引き継ぐ。
	cands := []candidate{{"current", sp.WithoutContext()}}
	for _, v := range sp.Variants {
		cands = append(cands, candidate{v.Name, sp.WithVariant(v).WithoutContext()})
	}
	fmt.Fprintf(stdout, "tune %s: %d truths (%d human) × %d candidates (model asked %d times)\n", sp.ID, len(bank), humanTruths(bank), len(cands), len(bank)*len(cands))
	scores := make([]candScore, len(cands))
	scores[0] = current
	for i := 1; i < len(cands); i++ {
		s, err := scoreCandidate(ctx, cands[i], bank, eps, o.ef.tainted)
		if err != nil {
			return nil, fmt.Errorf("tune: candidate %s: %w", cands[i].name, err)
		}
		scores[i] = s
	}
	res := &tuneResult{SpecID: sp.ID, SpecPath: sp.Path(), SpecHash: sp.Hash(), Version: sp.Version, Model: current.model, Truths: len(bank),
		At: time.Now().Format(time.RFC3339), Current: wordingOf(sp)}
	// 表
	fmt.Fprintf(stdout, "%-14s %6s %7s %9s %8s  %s\n", "candidate", "top-1", "brier", "prec@act", "rec@act", "what changes")
	for i, s := range scores {
		change := describeVariant(sp, cands[i].sp)
		fmt.Fprintf(stdout, "%-14s %6.3f %7.4f %9.3f %8.3f  %s\n", s.name, s.acc, s.brier, s.prec, s.rec, change)
		res.Candidates = append(res.Candidates, candidateResult{s.name, s.acc, s.brier, s.prec, s.rec, change})
	}
	best := pickBest(scores, nil)
	// holdout: k-1 で選んで 1 で測る(選び方そのものの当たり)。「今の文言のまま」は選ばないので、その holdout は現状の正解率そのもの。
	k := o.folds
	if k > len(bank) {
		k = len(bank)
	}
	if k >= 2 {
		var hit, total float64
		wins, losses := 0, 0 // 測る側の fold で、選ばれた候補と現状の正誤が食い違った行の数
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
				switch {
				case scores[chosen].correct[i] && !scores[0].correct[i]:
					wins++
				case !scores[chosen].correct[i] && scores[0].correct[i]:
					losses++
				}
			}
		}
		if total > 0 {
			res.Holdout, res.HaveHoldout = hit/total, true
			res.HoldoutWins, res.HoldoutLosses = wins, losses
			res.PValue = signTestP(wins, losses)
			fmt.Fprintf(stdout, "holdout (%d-fold: pick on %d/%d, measure on 1/%d): top-1=%.3f  vs keeping current: %.3f  (paired: %d better, %d worse, one-sided sign test p=%.3f)\n",
				k, k-1, k, k, res.Holdout, scores[0].acc, wins, losses, res.PValue)
		}
	}
	// 3. 採否
	res.Winner = "current"
	switch {
	case best == 0:
		res.Decision = "kept current (nothing beats it; ties keep it)"
		res.Lesson = fmt.Sprintf("none of %d candidates beat the current wording (top-1 %.3f) on %d truths", len(cands)-1, scores[0].acc, len(bank))
		fmt.Fprintln(stdout, "winner: current (nothing beats the current wording; ties keep it)")
	case !res.HaveHoldout:
		res.Decision = "kept current (too few truths for a holdout)"
		res.Lesson = fmt.Sprintf("%s looked better on the full bank (%.3f vs %.3f) but %d truths are too few for a holdout", scores[best].name, scores[best].acc, scores[0].acc, len(bank))
		fmt.Fprintf(stdout, "winner: current (too few truths for a holdout; %s looked better on the full bank but that is not enough)\n", scores[best].name)
	case res.Holdout <= scores[0].acc+1e-12 || res.PValue >= signTestAlpha:
		// 貯金全体では勝って見えても、選び方の当たりが現状を上回らない、または上回り方が偶然で説明できる = 変えない。
		// 候補を増やしても誤って採る率は上がらない: 測る側の fold は選ぶ側と独立なので、符号検定の p は名目どおり
		res.Decision = fmt.Sprintf("kept current (holdout %.3f vs %.3f, %d better / %d worse, p=%.3f: not enough evidence)", res.Holdout, scores[0].acc, res.HoldoutWins, res.HoldoutLosses, res.PValue)
		res.Lesson = fmt.Sprintf("%s wins on the full bank (%.3f vs %.3f) but the holdout (%.3f vs %.3f; %d better, %d worse, p=%.3f) does not rule out chance on %d truths; more truths will settle it", scores[best].name, scores[best].acc, scores[0].acc, res.Holdout, scores[0].acc, res.HoldoutWins, res.HoldoutLosses, res.PValue, len(bank))
		fmt.Fprintf(stdout, "winner: current (%s is better on the full bank, %.3f vs %.3f, but the holdout %.3f vs %.3f with %d better / %d worse (p=%.3f) does not rule out chance: not enough evidence to change the wording. More truths will settle it)\n",
			scores[best].name, scores[best].acc, scores[0].acc, res.Holdout, scores[0].acc, res.HoldoutWins, res.HoldoutLosses, res.PValue)
	default:
		res.Adoptable = true
		res.Winner = scores[best].name
		v := sp.Variants[best-1]
		res.WinnerVariant = &v
		res.WinnerWording = wordingOf(cands[best].sp)
		res.Lesson = fmt.Sprintf("%s: %s → top-1 %.3f → %.3f, brier %.4f → %.4f, holdout %.3f > current %.3f (%d better / %d worse, p=%.3f) on %d truths", scores[best].name, describeVariant(sp, cands[best].sp), scores[0].acc, scores[best].acc, scores[0].brier, scores[best].brier, res.Holdout, scores[0].acc, res.HoldoutWins, res.HoldoutLosses, res.PValue, len(bank))
		fmt.Fprintf(stdout, "winner: %s  top-1 %.3f → %.3f, brier %.4f → %.4f\n", scores[best].name, scores[0].acc, scores[best].acc, scores[0].brier, scores[best].brier)
		printConfusion(scores[best].confusion, stdout)
		if o.write && o.propose > 0 {
			res.Decision = fmt.Sprintf("%s was proposed by a model this run, so --write did not adopt it: a person has to read it first (it is saved in variants; adopt on the page, or run tune --write without --propose)", scores[best].name)
		} else if o.write {
			res.Decision = fmt.Sprintf("adopted %s as version %d (by the person at the terminal, --write)", scores[best].name, sp.Version+1)
		} else {
			res.Decision = fmt.Sprintf("%s may be adopted once its meaning is confirmed as unchanged (the agent via hook variants check, a person on the page, or --write)", scores[best].name)
		}
	}
	runPath, err := writeRunPage(o.wikiDir, sp, current.model, cands, scores, res.Holdout, res.HaveHoldout, res.Decision, res.Lesson, rationales)
	if err != nil {
		return res, fmt.Errorf("tune: write the wiki run page: %w", err)
	}
	res.RunPage = runPath
	res.Run = strings.TrimSuffix(filepath.Base(runPath), ".md")
	// 自動で育つ: 有意に勝った候補を主張の下書きに、既存の主張にはこの run を証拠として足す(モデルは呼び直さない)
	if drafted, updated, err := updateClaims(o.wikiDir, sp, cands, scores, res.Run, humanTruths(bank)); err != nil {
		fmt.Fprintf(stdout, "note: wiki claims not updated: %v\n", err)
	} else if drafted+updated > 0 {
		fmt.Fprintf(stdout, "wiki: %d claim(s) drafted, %d claim(s) given new evidence from this run\n", drafted, updated)
	}
	fmt.Fprintf(stdout, "wiki: %s\n", runPath)
	if !res.Adoptable {
		saveTuneResult(res)
		return res, nil
	}
	if !o.write {
		fmt.Fprintln(stdout, "dry run: pass --write to adopt it as a new version and calibrate it, or confirm the meaning on the page")
		saveTuneResult(res)
		return res, nil
	}
	if o.propose > 0 {
		// 機械が作った文言は、人が読む前には採用しない。候補は variants に残っているので、読んでから tune --write か画面で
		fmt.Fprintf(stdout, "not adopted: %s was proposed by a model in this run. Read it (variants in %s), then adopt on the page or with tune --write\n", scores[best].name, sp.Path())
		if res.WinnerWording != nil {
			fmt.Fprintf(stdout, "  question: %s\n", res.WinnerWording.Question)
		}
		saveTuneResult(res)
		return res, nil
	}
	// 端末の --write は「読んだ人がいる」ことの表明。意味の確認として残す
	recordMeaningCheck(res, true, "", "terminal --write")
	newPath, err := adoptWording(res, o.ef, o.storeDir, labeledPath, stdout)
	saveTuneResult(res)
	if err != nil {
		return res, err
	}
	appendRunNote(res.RunPage, fmt.Sprintf("**Adopted** as %s from the terminal (--write).", newPath))
	fmt.Fprintf(stdout, "wrote %s (version %d) and calibrated it\n", newPath, sp.Version+1)
	return res, nil
}

// signTestAlpha は採用に要る確かさ。候補と現状の実力が同じなら、採用を誤る率はこの値を超えない(候補の数に依らない)。
const signTestAlpha = 0.05

// signTestP は片側の符号検定: 食い違った n = wins+losses 行のうち wins 以上が候補側に出る確率(実力が同じなら 1/2 ずつ)。
func signTestP(wins, losses int) float64 {
	n := wins + losses
	if n == 0 {
		return 1
	}
	p := 0.0
	for x := wins; x <= n; x++ {
		p += binom(n, x)
	}
	return p / math.Pow(2, float64(n))
}

func binom(n, k int) float64 {
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(n-k+i) / float64(i)
	}
	return r
}

// adoptWording は勝った候補を新しい版として書き、その場で較正する。結果に採用の印を付ける(json / md の保存は呼ぶ側)。
func adoptWording(res *tuneResult, ef endpointFlags, storeDir, labeledPath string, reports ...io.Writer) (string, error) {
	base, err := spec.Load(res.SpecPath)
	if err != nil {
		return "", err
	}
	if base.Hash() != res.SpecHash {
		return "", fmt.Errorf("the spec changed since this run (hash %s → %s); run tune again", res.SpecHash, base.Hash())
	}
	if res.WinnerVariant == nil {
		return "", errors.New("nothing to adopt")
	}
	var before, after candidateResult
	for _, c := range res.Candidates {
		if c.Name == "current" {
			before = c
		}
		if c.Name == res.Winner {
			after = c
		}
	}
	newPath, err := writeTunedVersion(base, candidate{res.Winner, base.WithVariant(*res.WinnerVariant)}, before, after, res.Truths)
	if err != nil {
		return "", err
	}
	lab := labeledPath
	if lab == "" {
		if base.Eval.Labeled != "" {
			lab = base.Eval.Labeled
		} else {
			lab = "none"
		}
	}
	// 採用と較正は一体: 線が引けない文言は版にしない(残すと serve が較正無しの新版を使い、act が黙って消える)
	if err := runCalibrate(newPath, calibrateOpts{output: reportWriter(reports...), ef: ef, storeDir: storeDir, labeled: lab, bins: 10, write: true}); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("not adopted: calibration of the new wording failed, so %s was not kept: %w", filepath.Base(newPath), err)
	}
	if n, err := spec.Load(newPath); err != nil || len(n.Calibration) == 0 {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("not adopted: the new wording could not be calibrated (precision at act below the threshold), so %s was not kept", filepath.Base(newPath))
	}
	res.Adopted, res.AdoptedPath = true, newPath
	return newPath, nil
}

// saveTuneResult は結果を run ページの隣の json に残す(画面と MCP が読む)。wiki が無ければ何もしない。
func saveTuneResult(res *tuneResult) {
	if res.RunPage == "" {
		return
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(strings.TrimSuffix(res.RunPage, ".md")+".json", b, 0o644)
}

// appendRunNote は run の md に 1 行足す。
func appendRunNote(runPage, line string) {
	if runPage == "" {
		return
	}
	if f, err := os.OpenFile(runPage, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString("\n" + line + "\n")
		f.Close()
	}
}

// loadTuneResult は run 名から json を読む。
func loadTuneResult(wikiDir, run string) (*tuneResult, error) {
	run = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(run), ".json"), ".md")
	b, err := os.ReadFile(filepath.Join(wikiDir, "runs", run+".json"))
	if err != nil {
		return nil, err
	}
	var res tuneResult
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// latestTuneResult は spec の一番新しい run の json。
func latestTuneResult(wikiDir, specID string) (*tuneResult, error) {
	files, _ := filepath.Glob(filepath.Join(wikiDir, "runs", "*.json"))
	var best *tuneResult
	for _, f := range files {
		res, err := loadTuneResult(wikiDir, filepath.Base(f))
		if err != nil || res.SpecID != specID {
			continue
		}
		if best == nil || parsedAt(res.At).After(parsedAt(best.At)) {
			best = res
		}
	}
	if best == nil {
		return nil, errNotFound
	}
	return best, nil
}

func parsedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// recordMeaningCheck は人の「意味は同じ / 違う」を run の md と json に残す。却下は spec のページにも残り、次の提案役が読む。
func recordMeaningCheck(res *tuneResult, same bool, note, by string) {
	recordCheck(res, meaningCheck{Same: same, Note: note, By: by})
}

// recordCheck は意味の確かめ(人か、0.9.2 からはエージェント)を run の md と json に残す。エージェントが決めきれなかった時(unsure)は却下にしない。
func recordCheck(res *tuneResult, mc meaningCheck) {
	mc.At = time.Now().Format("2006-01-02 15:04")
	res.MeaningChecks = append(res.MeaningChecks, mc)
	same, note, by := mc.Same, mc.Note, mc.By
	if mc.Difference != "" {
		note = strings.TrimSpace(note + " " + mc.Difference)
	}
	verdict := "meaning confirmed as the same"
	switch {
	case mc.Verdict == "unsure":
		verdict = "the agent could not decide whether the meaning is the same: waiting for a person"
	case !same:
		verdict = "meaning judged to have CHANGED: not adopted"
	}
	line := fmt.Sprintf("\n**Meaning check (%s, %s):** %s", mc.At, by, verdict)
	if note != "" {
		line += " — " + note
	}
	if res.RunPage != "" {
		if f, err := os.OpenFile(res.RunPage, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(line + "\n")
			f.Close()
		}
		if !same && mc.Verdict != "unsure" {
			q := ""
			if res.WinnerWording != nil {
				q = trim(res.WinnerWording.Question, 80)
			}
			specPage := filepath.Join(filepath.Dir(filepath.Dir(res.RunPage)), "specs", res.SpecID+".md")
			who := "a person"
			if by == "agent" {
				who = "the agent"
			}
			appendRunNote(specPage, fmt.Sprintf("- %s: %s rejected %s (%q): the meaning changed. %s", mc.At, who, res.Winner, q, note))
		}
	}
	saveTuneResult(res)
}

// truthBank は正解の貯金。ファイルの行 + store の本文付きの正解(入力ごとに最新 1 つ。文言・モデルは問わない)。
func truthBank(sp *spec.Spec, st store.Store, labeledPath string) (rows []bankRow, skipped int, err error) {
	type entry struct {
		row bankRow
		at  time.Time
	}
	latest := map[string]entry{} // 入力ごとに最新の正解(calibrate の LatestPerInput と同じ決まり)。ファイルの行は最古
	var order []string
	put := func(h string, row bankRow, at time.Time) {
		if !truthKnown(sp, row.truth) {
			skipped++ // 今の spec に無い選択肢など。黙って捨てずに数える
			return
		}
		if row.weight <= 0 || row.weight > 1 {
			row.weight = 1 // store と同じく重みは (0, 1]
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
			if len(r.Inputs) == 0 {
				continue // raw だけの行は聞き直せない
			}
			truth, err := parseTruth(sp.Type, r.Truth)
			if err != nil {
				return nil, 0, fmt.Errorf("row %d: %w", i, err)
			}
			put(store.HashInputs(r.Inputs), bankRow{inputs: r.Inputs, truth: truth, weight: r.Weight, human: true}, time.Time{})
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
	for _, l := range store.LatestPerInput(all) {
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

// truthKnown は正解が今の spec で意味を持つか(選択肢 id、!id の部分ラベル、段階名、yes/no)。
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

// scorable は採点できる行だけを残す: 必要な入力が揃い、送れる先がある。残りは数えて除く(1 行で tune を止めない)。
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

// writeTunedVersion は勝った候補を <dir>/<id>-v<N+1>.yaml に書く。元のファイルは触らない。候補の一覧は引き継ぐ(次の tune のため)。
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
