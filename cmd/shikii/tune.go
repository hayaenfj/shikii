package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
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
	ctx, cancel := context.WithTimeout(context.Background(), tuneRunLimit)
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
