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
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// shikii tune: scores every candidate wording (question text, choice descriptions, how the text is laid out) against the bank of truths, and writes the winner as a new version.
// People decide only the truths. Candidates go in the spec's variants: (later, --propose was added to have a model propose them).
//
//   - bank = rows of the labeled file + truths with text in the store (the latest one per input, regardless of wording or model)
//   - for each candidate, ask the current model about every input in the bank and report top-1 / Brier / precision@act / recall@act
//   - to see the optimism of choosing and measuring on the same inputs, also report the accuracy of "choose on k-1, measure on 1" over k folds (holdout)
//   - win = higher top-1; on a tie, lower Brier. Still tied, the current wording (no change)
//   - --write writes the winner to <id>-v<version+1>.yaml (the original file is untouched) and calibrates on the spot

type bankRow struct {
	inputs  map[string]string
	truth   string
	weight  float64
	tainted bool
	human   bool // whether the truth was decided by a person (or an outcome). Machine truths (context / judge) are false. tune and the wiki evidence show the human count
}

// humanTruths is the number of truths in the bank decided by a person.
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
	fs.StringVar(&o.storeDir, "store", ".shikii", "where records and labels are kept")
	fs.StringVar(&o.labeled, "labeled", "", "labeled file to add to the bank of truths (default: the spec's eval.labeled; none means do not use one)")
	fs.BoolVar(&o.write, "write", false, "write the winner as a new version and calibrate on the spot")
	fs.IntVar(&o.folds, "folds", 5, "number of holdout folds")
	fs.IntVar(&o.propose, "propose", 0, "have the proposer (a chat LLM) make N candidates and add them to the spec's variants")
	fs.StringVar(&o.proposer, "proposer", "", "URL of the proposer (OpenAI-compatible /v1/chat/completions; upstream llama-server works too). Specs with pii: true use only a local (127.0.0.1) proposer")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "model name of the proposer (empty means the endpoint's default)")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	o.specPath = path
	_, err = runTune(o)
	return err
}

// tuneOpts are tune's settings. The CLI and serve / mcp use the same content.
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
	wikiDir       string // empty means <store>/wiki (only tests point elsewhere)
}

// Basic layout: everything shikii makes is under the store (default .shikii/): decisions/ (specs), wiki/ (knowledge), records/ and labels/ (records and truths).
// The only thing added to the project root is .shikii/. There are no separate flags for where the wiki and specs go (they follow from the store).
func wikiDirOf(storeDir string) string  { return filepath.Join(storeDir, "wiki") }
func specsDirOf(storeDir string) string { return filepath.Join(storeDir, "decisions") }

// tuneResult is the result of one tune. It is also kept in wiki/runs/<name>.json and read by the page and MCP. Adoption decisions are added here too.
type tuneResult struct {
	SpecID        string            `json:"spec"`
	SpecPath      string            `json:"spec_path"`
	SpecHash      string            `json:"spec_hash"`
	Version       int               `json:"version"`
	Model         string            `json:"model"`
	Truths        int               `json:"truths"`
	At            string            `json:"at"`
	Run           string            `json:"run"`      // file name in runs/ (no extension)
	RunPage       string            `json:"run_page"` // path of the markdown
	Candidates    []candidateResult `json:"candidates"`
	Holdout       float64           `json:"holdout"`
	HaveHoldout   bool              `json:"have_holdout"`
	HoldoutWins   int               `json:"holdout_better"` // rows in the measuring fold where the chosen candidate was right and the current one wrong
	HoldoutLosses int               `json:"holdout_worse"`
	PValue        float64           `json:"p_value"` // one-sided sign test. If the candidate were as good as the current one, the chance of winning this much
	Winner        string            `json:"winner"`  // "current" or a candidate name
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
	AdoptError    string            `json:"adopt_error,omitempty"` // the agent said the meaning is the same, but adoption failed (e.g. calibration); told to the person once
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
	Lang        string `json:"lang,omitempty"` // language of the translation (the configured language; for English there is no translation)
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

// runTune is the body of tune. It prints the report to stdout and returns the result (if there is a wiki, it writes md and json to runs/).
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

	// 1. Score the current wording (first, so the confusion table and wrong examples can go to the proposer)
	current, err := scoreCandidate(ctx, candidate{"current", sp.WithoutContext()}, bank, eps, o.ef.tainted)
	if err != nil {
		return nil, fmt.Errorf("tune: current: %w", err)
	}
	// 2. Propose (if asked): have it read the wiki and make candidates, and keep them in the spec's variants
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
			// Write back only variants. Reread the file and add, so a line another terminal wrote with calibrate --write during the long scoring is not overwritten with an old in-memory line
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
	// Candidates are scored only on the minimal question rung (comparing wordings; going up the rungs mixes in context rung answers). The adopted version keeps context.
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
	// spec_path is saved absolute: serve and the hook adopt the run from another folder, where a path relative to this tune's folder names nothing (re-audit 0.9.2).
	// rebase still rebuilds it from the wiki folder after the project folder is moved.
	specPath := sp.Path()
	if abs, err := filepath.Abs(specPath); err == nil {
		specPath = abs
	}
	res := &tuneResult{SpecID: sp.ID, SpecPath: specPath, SpecHash: sp.Hash(), Version: sp.Version, Model: current.model, Truths: len(bank),
		At: time.Now().Format(time.RFC3339), Current: wordingOf(sp)}
	// table
	fmt.Fprintf(stdout, "%-14s %6s %7s %9s %8s  %s\n", "candidate", "top-1", "brier", "prec@act", "rec@act", "what changes")
	for i, s := range scores {
		change := describeVariant(sp, cands[i].sp)
		fmt.Fprintf(stdout, "%-14s %6.3f %7.4f %9.3f %8.3f  %s\n", s.name, s.acc, s.brier, s.prec, s.rec, change)
		res.Candidates = append(res.Candidates, candidateResult{s.name, s.acc, s.brier, s.prec, s.rec, change})
	}
	best := pickBest(scores, nil)
	// holdout: choose on k-1 and measure on 1 (how good the choosing itself is). "Keep the current wording" is never chosen, so its holdout is just the current accuracy.
	k := o.folds
	if k > len(bank) {
		k = len(bank)
	}
	if k >= 2 {
		var hit, total float64
		wins, losses := 0, 0 // number of rows in the measuring fold where the chosen candidate and the current one disagree on right/wrong
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
	// 3. Adopt or not
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
		// Even if it looks like a win over the whole bank, if the choosing does not beat the current one, or the margin can be explained by chance, do not change.
		// Adding candidates does not raise the rate of adopting by mistake: the measuring fold is independent of the choosing side, so the sign test's p is as nominal
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
	// Grows by itself: a candidate that won significantly becomes a claim draft, and this run is added as evidence to existing claims (the model is not called again)
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
		// Wording the machine made is not adopted before a person reads it. The candidates stay in variants, so after reading use tune --write or the page
		fmt.Fprintf(stdout, "not adopted: %s was proposed by a model in this run. Read it (variants in %s), then adopt on the page or with tune --write\n", scores[best].name, sp.Path())
		if res.WinnerWording != nil {
			fmt.Fprintf(stdout, "  question: %s\n", res.WinnerWording.Question)
		}
		saveTuneResult(res)
		return res, nil
	}
	// --write in the terminal is a statement that someone has read it. Keep it as the meaning check
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

// signTestAlpha is the certainty needed to adopt. If the candidate and the current one are equally good, the rate of adopting by mistake does not exceed this value (regardless of the number of candidates).
const signTestAlpha = 0.05

// signTestP is the one-sided sign test: among the n = wins+losses rows that disagree, the chance that wins or more fall on the candidate's side (1/2 each if equally good).
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

// adoptWording writes the winner as a new version and calibrates on the spot. It marks the result as adopted (saving json / md is the caller's job).
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
	// Adopting and calibrating go together: wording that cannot get a line does not become a version (if kept, serve would use a new version with no calibration and act would silently disappear)
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

// saveTuneResult keeps the result in a json next to the run page (read by the page and MCP). Does nothing without a wiki.
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

// appendRunNote adds one line to the run's md.
func appendRunNote(runPage, line string) {
	if runPage == "" {
		return
	}
	if f, err := os.OpenFile(runPage, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString("\n" + line + "\n")
		f.Close()
	}
}

// loadTuneResult reads the json for a run name.
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
	res.rebase(wikiDir, run)
	return &res, nil
}

// rebase rebuilds the absolute paths kept in the json from the wiki it was read from (audit 0.9.2). After the project folder was moved, the recorded
// paths point at a place that is gone; in a copied project they point at the original project, which still exists. Either way every reader and writer
// must stay inside this project: run_page is the md next to the json, and a spec or adopted version that was inside the store the run was written in
// (the store of the recorded run_page, normally <store>/decisions/<file>) is the same file in this store. An undone version keeps its recorded name
// (only the file on disk gets ".reverted"), so it is found the same way. A spec outside that store (serve --specs, or tune on a file elsewhere) keeps its path.
func (res *tuneResult) rebase(wikiDir, run string) {
	if abs, err := filepath.Abs(wikiDir); err == nil {
		wikiDir = abs
	}
	recorded := res.RunPage
	res.RunPage = filepath.Join(wikiDir, "runs", run+".md")
	if recorded == "" {
		return
	}
	oldStore, store := filepath.Dir(filepath.Dir(filepath.Dir(recorded))), filepath.Dir(wikiDir)
	for _, p := range []*string{&res.SpecPath, &res.AdoptedPath} {
		if *p == "" {
			continue
		}
		rel, err := filepath.Rel(oldStore, *p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		*p = filepath.Join(store, rel)
	}
}

// lockTuneRun takes the lock of one run (a file next to its json) while it is adopted or undone. The agent's adoption (hook variants check, a separate
// process) and the page's (serve) share it, so the same run is not adopted twice at the same time (audit 0.9.2, low 2). It does not wait: when the run
// is held, it returns errTuneRunBusy and the caller refuses.
func lockTuneRun(wikiDir, run string) (func(), error) {
	run = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(run), ".json"), ".md")
	f, err := os.OpenFile(filepath.Join(wikiDir, "runs", run+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errTuneRunBusy
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

var errTuneRunBusy = errors.New("this run is being adopted or undone right now (by the agent or on the page); try again after it finishes")

// latestTuneResult is the json of the spec's newest run.
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

// recordMeaningCheck keeps the person's "same meaning / different" in the run's md and json. A rejection also stays on the spec's page, and the next proposer reads it.
func recordMeaningCheck(res *tuneResult, same bool, note, by string) {
	recordCheck(res, meaningCheck{Same: same, Note: note, By: by})
}

// recordCheck keeps the meaning check (by a person, or from 0.9.2 by an agent) in the run's md and json. When the agent could not decide (unsure), it is not a rejection.
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
			if len(r.Inputs) == 0 {
				continue // a row with only raw cannot be asked again
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
