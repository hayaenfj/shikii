package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// hookMaybeCalibrate follows the same rule as serve's auto-calibration: for a model with no line for the current wording (hash) yet, draw the line once the person's truths (one per input) reach eval.min_n.
// Models with a line are left alone (redrawing happens when drift reports a decline). The work is hookCalibrate (by default a detached process). The same spec runs at most once per hookCalibrateGap.
func hookMaybeCalibrate(agent, specID string) error {
	return hookMaybeCalibrateWith(agent, specID, newSpecTruths(store.Store{Dir: agent}, specID))
}

// hookMaybeCalibrateWith is hookMaybeCalibrate reading the spec's records and labels through truths, so the checks that run back to
// back at Stop decode them once. Once a calibration is started the reads are forgotten: it may write new records and labels.
func hookMaybeCalibrateWith(agent, specID string, truths *specTruths) error {
	path := hookSpecPath(agent, specID)
	sp, err := spec.Load(path)
	if err != nil {
		return nil // no spec (the person removed it): do nothing
	}
	st := truths.st
	recs, err := truths.records()
	if err != nil {
		return err
	}
	models := map[string]bool{}
	for _, r := range recs {
		if r.Model != "" && sp.LiveHash(r.SpecHash) {
			models[r.Model] = true
		}
	}
	for model := range models {
		if c := sp.CalibrationFor(model); c != nil && c.SpecHash == sp.Hash() {
			continue
		}
		labels, err := truths.labelsFor(sp.Hash(), model)
		if err != nil {
			continue
		}
		if all, err := truths.allLabels(); err == nil { // counted like calibrate: an untrusted outcome's truths are not counted (0.11)
			labels = store.WithoutUntrustedOutcome(labels, store.OutcomeAgreement(all), sp.Thresholds.Act)
		}
		var human []store.Label
		for _, l := range store.LatestPerInput(labels) { // counted like calibrate: one per input, the person's truths only
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		if len(human) < sp.Eval.MinN {
			continue
		}
		guard := filepath.Join(st.Dir, "calibrating-"+specID)
		if info, err := os.Stat(guard); err == nil && time.Since(info.ModTime()) < hookCalibrateGap {
			continue // the previous calibration is still running (or just failed). Wait a while
		}
		if last, ok := lastCalibrateAttempt(guard, sp.Hash(), model); ok && len(human) < last+hookRecalibrateEvery {
			continue // no line could be drawn for the same wording and model: no replay until truths grow by hookRecalibrateEvery
		}
		_ = os.WriteFile(guard, []byte(fmt.Sprintf("%s %s %d %s\n", time.Now().Format(time.RFC3339), model, len(human), sp.Hash())), 0o600)
		hookCalibrate(agent, path, st.Dir, model)
		truths.forget()
	}
	return nil
}

// stopSpecChecks runs, for one of the hook's own specs at Stop, the check that may draw its line and then the check that may score
// its wordings. Both read the spec's records and labels through the same specTruths, so each file is decoded once.
func stopSpecChecks(agent, specID string) (calibrate, tune error) {
	truths := newSpecTruths(store.Store{Dir: agent}, specID)
	calibrate = hookMaybeCalibrateWith(agent, specID, truths)
	tune = hookMaybeTuneWith(agent, specID, truths)
	return calibrate, tune
}

// lastCalibrateAttempt returns, from the record of the last auto-calibration (time, model, number of truths, wording hash), the number of truths tried with the same wording and model.
// If a line is drawn it is never called, so a record means "tried but no line" or "still running". Records in an old format count as missing.
func lastCalibrateAttempt(guard, hash, model string) (int, bool) {
	b, err := os.ReadFile(guard)
	if err != nil {
		return 0, false
	}
	var at, m, h string
	var n int
	if _, err := fmt.Sscanf(string(b), "%s %s %d %s", &at, &m, &n, &h); err != nil || m != model || h != hash {
		return 0, false
	}
	return n, true
}

// humanTruths is the number of the person's truths on the current wording (hash) (one per input, counted across models). Counted like calibrate / tune.
func humanTruthsFor(st store.Store, sp *spec.Spec) (int, error) {
	return humanTruthsWith(newSpecTruths(st, sp.ID), sp)
}

// humanTruthsWith is humanTruthsFor reading through truths.
func humanTruthsWith(truths *specTruths, sp *spec.Spec) (int, error) {
	recs, err := truths.records()
	if err != nil {
		return 0, err
	}
	models := map[string]bool{}
	for _, r := range recs {
		if r.Model != "" && sp.LiveHash(r.SpecHash) {
			models[r.Model] = true
		}
	}
	var all []store.Label
	for model := range models {
		labels, err := truths.labelsFor(sp.Hash(), model)
		if err != nil {
			return 0, err
		}
		all = append(all, labels...)
	}
	n := 0
	originals, err := truths.allLabels()
	if err != nil {
		return 0, err
	}
	all = store.WithoutUntrustedOutcome(all, store.OutcomeAgreement(originals), sp.Thresholds.Act)
	for _, l := range store.LatestPerInput(all) {
		if store.HumanSource(l.Source) {
			n++
		}
	}
	return n, nil
}

// hookMaybeTune lets the machine try the wording variants (on a slow cycle, like the wiki's lint): when the spec has variants (wordings the person wrote),
// the person's truths are at least eval.min_n, and they grew by hookTuneEvery since the last auto-tune, it starts `shikii tune` (without --write) detached.
// Without variants there is nothing to try. A variant that scored higher gets its meaning checked by the agent before the next request (wordingCheckContext). The last count is kept in tuned-<spec>.
func hookMaybeTune(agent, specID string) error {
	return hookMaybeTuneWith(agent, specID, newSpecTruths(store.Store{Dir: agent}, specID))
}

// hookMaybeTuneWith is hookMaybeTune reading the spec's records and labels through truths (see hookMaybeCalibrateWith).
func hookMaybeTuneWith(agent, specID string, truths *specTruths) error {
	path := hookSpecPath(agent, specID)
	sp, err := spec.Load(path)
	if err != nil || len(sp.Variants) == 0 {
		return nil
	}
	st := truths.st
	n, err := humanTruthsWith(truths, sp)
	if err != nil {
		return err
	}
	if n < sp.Eval.MinN {
		return nil
	}
	if !contrastingTuneTruthsWith(truths, sp) {
		return nil
	}
	guard := filepath.Join(agent, "tuned-"+specID)
	last := -1
	if b, err := os.ReadFile(guard); err == nil {
		var cnt int
		var hash string
		if _, err := fmt.Sscanf(string(b), "%d %s", &cnt, &hash); err == nil && hash == sp.Hash() {
			last = cnt // carry the last count only for the same wording (a changed wording is counted again as a new spec)
		}
	}
	if last >= 0 && n-last < hookTuneEvery {
		return nil
	}
	if err := writeTuneGuard(agent, sp, n); err != nil {
		return err
	}
	hookTune(agent, path, st.Dir)
	truths.forget()
	return nil
}

// spawnTune starts `shikii tune <spec> --store … --local …` in a detached process (the hook does not wait; no --write). Output is appended to hook.log.
func spawnTune(agent, path, storeDir string) {
	spawnMaintenance(agent, "auto-tune", fmt.Sprintf("%s: started in the background (adoption is a person's call: the page, or tune --write)", strings.TrimSuffix(filepath.Base(path), ".yaml")), "tune", path, "--store", storeDir, "--local", hookLocalURL)
}

// spawnCalibrate keeps the model and --write arguments specific to calibration.
func spawnCalibrate(agent, path, storeDir, model string) {
	spawnMaintenance(agent, "auto-calibrate", fmt.Sprintf("%s (%s): started in the background", filepath.Base(path), model), "calibrate", path, "--store", storeDir, "--local", hookLocalURL, "--model", model, "--write")
}

func spawnMaintenance(agent, operation, started string, args ...string) {
	exe, err := os.Executable()
	if err != nil {
		hookLog(operation + ": cannot find shikii: " + err.Error())
		return
	}
	logf, err := os.OpenFile(filepath.Join(agent, "hook.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s %s %s\n", time.Now().Format(time.RFC3339), operation, started)
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := startMaintenance(cmd); err != nil {
		fmt.Fprintf(logf, "%s %s: %v\n", time.Now().Format(time.RFC3339), operation, err)
	}
}

// Tests inspect the command without starting the test executable in the background.
var startMaintenance = func(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}
