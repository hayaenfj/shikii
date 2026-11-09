package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func cmdCalibrate(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	labeledPath := fs.String("labeled", "", "labeled data (default: the spec's eval.labeled; without it, only store labels; none skips the spec default too)")
	bins := fs.Int("bins", 10, "number of bins in the reliability diagram")
	write := fs.Bool("write", false, "write the calibration back to the spec")
	force := fs.Bool("force", false, "write back even if inverted or lacking precision")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	return runCalibrate(path, calibrateOpts{ef: ef, storeDir: *storeDir, labeled: *labeledPath, bins: *bins, write: *write, force: *force})
}

// calibrateOpts are the calibrate options. The CLI and serve (automatic calibration) use the same values.
type calibrateOpts struct {
	output   io.Writer
	ef       endpointFlags
	storeDir string
	labeled  string // "" = the spec's eval.labeled, "none" = use no file
	bins     int
	write    bool
	force    bool
	rung     string // "" = both rungs, "minimal" | "context" = draw only that rung (serve's automatic calibration only does rungs with enough truths)
}

// Splits the reasons calibration cannot be saved into too few truths and prediction quality. The page uses the same reasons.
type calibrationRefusal struct {
	humanTruths int
	minN        int
}

func (e *calibrationRefusal) reason() string {
	if e.humanTruths < e.minN {
		return "insufficient"
	}
	return "quality"
}

func (e *calibrationRefusal) Error() string {
	if e.reason() == "insufficient" {
		return fmt.Sprintf("calibration not saved: there are %d truths from people or outcomes, short of the %d required. Add truths with shikii serve, shikii label or shikii import. For a trial only, you can lower the spec's eval.min_n.", e.humanTruths, e.minN)
	}
	return fmt.Sprintf("calibration not saved: there are %d truths, but with the current question and the model's predictions the conditions for acting automatically are not met. Review the question or the model.", e.humanTruths)
}

// runCalibrate is the body of calibrate. It prints its report to stdout.
func runCalibrate(path string, o calibrateOpts) error {
	stdout := reportWriter(o.output)
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	wrote := false
	// The minimal-question rung. Failing to draw a line here is an error (as before).
	if o.rung != "context" {
		cal, ok, err := calibrateRung(sp, o)
		if err != nil {
			return err
		}
		if o.write && !ok {
			if err := noteStopped(o.storeDir, sp, cal); err != nil {
				fmt.Fprintf(stdout, "note: could not keep the stop of %s: %v\n", sp.ID, err)
			}
			return &calibrationRefusal{humanTruths: cal.NHuman, minN: sp.Eval.MinN}
		}
		if o.write {
			sp.PutCalibration(cal)
			wrote = true
			clearStopped(o.storeDir, sp.ID, cal.SpecHash)
		}
	}
	// The context rung (if any). Its truths are only those a person gave to decisions settled with added context. If there are none yet, say so and move on.
	if csp := sp.ContextSpec(); csp != nil && o.rung != "minimal" {
		fmt.Fprintf(stdout, "\ncontext rung: the same question asked with state.context (hash=%s)\n", csp.Hash())
		co := o
		co.labeled = "none" // file rows belong to the minimal question
		ccal, cok, err := calibrateRung(csp, co)
		switch {
		case errors.Is(err, errNoLabels):
			fmt.Fprintln(stdout, "note: no truths on the context rung yet (they come from people answering decisions that went through it)")
		case err != nil:
			return err
		case o.write && cok:
			sp.PutCalibration(ccal)
			wrote = true
		case o.write && o.rung == "context":
			return &calibrationRefusal{humanTruths: ccal.NHuman, minN: csp.Eval.MinN}
		case o.write:
			fmt.Fprintln(stdout, "note: context rung not written (see WARNING above)")
		}
	}
	if wrote {
		if err := sp.Save(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "wrote calibration to", sp.Path())
	}
	return nil
}

var errNoLabels = errors.New("no labels")

// calibrateRung draws the line for one rung (one spec hash). It always prints the report and returns whether it may be written (ok). It does not write the line itself (the caller writes them together).
func calibrateRung(sp *spec.Spec, o calibrateOpts) (spec.Calibration, bool, error) {
	stdout := reportWriter(o.output)
	ef, storeDir, labeledPath, bins, force := o.ef, &o.storeDir, &o.labeled, &o.bins, &o.force
	var cal spec.Calibration
	if *bins <= 0 {
		*bins = 10
	}
	if *labeledPath == "none" {
		*labeledPath = "" // proceed with store labels and re-asking only
	} else if *labeledPath == "" {
		// The spec's default file may be missing (proceed with store labels only). If given explicitly with --labeled, a missing file is an error.
		if _, err := os.Stat(sp.Eval.Labeled); sp.Eval.Labeled != "" && err == nil {
			*labeledPath = sp.Eval.Labeled
		} else if sp.Eval.Labeled != "" {
			fmt.Fprintf(stdout, "note: eval.labeled %s not found; using store labels only\n", sp.Eval.Labeled)
		}
	}

	// 1. The labeled file (if any). For rows without Raw, use a store label with the same inputs if there is one; otherwise ask the model.
	st := store.Store{Dir: *storeDir}
	labels, model, err := labelsFromFile(sp, *labeledPath, ef, st, stdout)
	if err != nil {
		return cal, false, err
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if l.InputsHash != "" {
			seen[l.InputsHash] = true
		}
	}
	// 2. Re-ask: for truths given under old wording or another model, ask again with the current (hash, model) from records that still have their text.
	//    A truth is decided by a person and does not depend on wording or model. Only the probability is fetched again.
	replayed, replayModel, err := replayTruths(sp, st, ef, model, seen, stdout)
	if err != nil {
		return cal, false, err
	}
	if model == "" {
		model = replayModel
	}
	if model == "" {
		model = ef.model
	}
	// 3. Labels collected in the store: all that match (hash, model). Keep them even if the file has the same input:
	//    if a correction a person added later is newer, it wins in LatestPerInput below (audit C2).
	stored, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return cal, false, err
	}
	replayedSet, err := storedInputs(st, sp, model)
	if err != nil {
		return cal, false, err
	}
	for _, l := range replayed {
		if replayedSet[l.InputsHash] {
			continue
		}
		replayedSet[l.InputsHash] = true
		if err := st.AppendLabel(l); err != nil {
			return cal, false, fmt.Errorf("persist replayed label: %w", err)
		}
		stored = append(stored, l)
	}
	// 4. Keep in the store the labels for file rows we got by asking the model, so check / drift can verify without the model later.
	persisted := 0
	storedSet, err := storedInputs(st, sp, model)
	if err != nil {
		return cal, false, err
	}
	for _, l := range labels {
		if l.Model == "" || l.InputsHash == "" {
			continue // rows with raw (model not called) are not kept
		}
		if storedSet[l.InputsHash] {
			continue
		}
		storedSet[l.InputsHash] = true
		if err := st.AppendLabel(l); err != nil {
			return cal, false, fmt.Errorf("persist label: %w", err)
		}
		persisted++
	}
	if persisted > 0 {
		fmt.Fprintf(stdout, "persisted %d queried labels to %s\n", persisted, *storeDir)
	}
	fileN, storedN := len(labels), len(stored)
	labels = append(labels, stored...)
	// Reject stale or untrusted replays before selecting the latest label, so
	// they cannot hide the person's valid earlier correction for this input.
	if _, all, err := st.Counts(sp.ID); err == nil {
		report := store.OutcomeAgreement(all)
		if report.Total().Checked > 0 {
			fmt.Fprintln(stdout, strings.TrimSpace(outcomeCheckLine(report, sp.Thresholds.Act)))
		}
		labels = store.WithoutUntrustedOutcome(labels, report, sp.Thresholds.Act)
	}
	// The latest truth per input (counted the same way as check / drift). A correction replaces; duplicates of one input count once
	if before := len(labels); before > 0 {
		labels = store.LatestPerInput(labels)
		if len(labels) < before {
			fmt.Fprintf(stdout, "deduplicated: %d labels on %d distinct inputs (latest truth per input wins)\n", before, len(labels))
		}
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s n=%d (file=%d store=%d replay=%d)\n", sp.ID, sp.Hash(), model, len(labels), fileN, storedN, len(replayed))
	if len(labels) == 0 {
		return cal, false, fmt.Errorf("calibrate: %w for (hash=%s, model=%s)", errNoLabels, sp.Hash(), model)
	}
	// Print the report even with too few. Only writing back is stopped. Count only truths decided by people (or outcomes): never draw a line from machine truths (upper-rung answers) alone (audit round 3 item 2)
	nHuman := 0
	for _, l := range labels {
		if store.HumanSource(l.Source) {
			nHuman++
		}
	}
	underMin := nHuman < sp.Eval.MinN
	if underMin {
		fmt.Fprintf(stdout, "WARNING: %d human truths (%d samples in all) < eval.min_n=%d; report only, will not write (collect more, or lower eval.min_n in the spec for a trial)\n", nHuman, len(labels), sp.Eval.MinN)
	}

	// Always print the report. Whether it may be written back (ok) is returned separately and only matters with --write.
	var ok bool
	switch sp.Type {
	case spec.Noul:
		cal, ok, err = calibrateNoul(sp, labels, *bins, stdout)
	default:
		cal, ok, err = verifyTop(sp, labels, *bins, stdout)
	}
	if err != nil {
		return cal, false, err
	}
	if d, ok := discriminationOf(sp, labels); ok {
		cal.Discrimination = &d
	}
	cal.Model, cal.SpecHash, cal.N, cal.NHuman = model, sp.Hash(), len(labels), nHuman
	cal.FittedOn, cal.FittedAt = *labeledPath, time.Now().Format("2006-01-02")
	// --force overrides a refusal of a fitted line, never the lack of one: calibrateNoul returns no method when no line fits (one side only, or a
	// higher probability goes with "no"), and writing that empty block would replace this model's line with one nothing can use (0.9.4 audit)
	return cal, ((ok && !underMin) || *force) && cal.Method != "", nil
}
