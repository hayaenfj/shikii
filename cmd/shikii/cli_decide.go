package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func cmdDecide(args []string) error {
	fs := flag.NewFlagSet("decide", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	noRecord := fs.Bool("no-record", false, "do not record")
	inputs := kvFlag{}
	fs.Var(inputs, "i", "input key=value (repeatable)")
	asJSON := fs.Bool("json", false, "print the Decision as JSON")
	path, err := parseSpecPath(fs, args)
	if err != nil {
		return err
	}
	sp, err := spec.Load(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, inputs, decide.Session{Tainted: ef.tainted}, ef.endpoints())
	if err != nil {
		return err
	}
	attachReceipt(sp, store.Store{Dir: *storeDir}, d)
	if !*noRecord && d.Band != decide.Abstain {
		if err := recordDecision(store.Store{Dir: *storeDir}, sp, d, inputs); err != nil {
			return err
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}
	fmt.Fprintf(stdout, "spec=%s hash=%s model=%s endpoint=%s trace=%s\n", d.SpecID, d.SpecHash, d.Model, d.Endpoint, d.TraceID)
	fmt.Fprintf(stdout, "band=%s top=%s p=%.3f calibrated=%v", d.Band, d.Top, d.P, d.IsCalibrated)
	if sp.Type == spec.Score {
		fmt.Fprintf(stdout, " score=%.2f", d.Score)
	}
	fmt.Fprintln(stdout)
	if d.Reason != "" {
		fmt.Fprintln(stdout, "reason:", d.Reason)
	}
	if d.Minimal != nil {
		fmt.Fprintf(stdout, "rung=context (the minimal question was unsure: top=%s p=%.3f band=%s; asked again with state.context)\n", d.Minimal.Top, d.Minimal.P, d.Minimal.Band)
	}
	if r := d.Receipt; r != nil {
		fmt.Fprintf(stdout, "receipt: line %s fitted %s on %d truths (%d human); human-checked act decisions %d, precision %.3f\n", r.Method, r.FittedAt, r.N, r.NHuman, r.HumanCheckedAct, r.HumanPrecision)
	}
	if len(d.Raw) > 0 {
		fmt.Fprintln(stdout, "raw:", fmtDist(d.Raw))
		if d.IsCalibrated {
			fmt.Fprintln(stdout, "calibrated:", fmtDist(d.Calibrated))
		}
	}
	return nil
}

// recordOf turns a Decision into a record. The text is always kept.
// copyInputs copies the text (kept in records and labels).
func copyInputs(inputs map[string]string) map[string]string {
	if len(inputs) == 0 {
		return nil
	}
	out := make(map[string]string, len(inputs))
	for k, v := range inputs {
		out[k] = v
	}
	return out
}

func recordOf(sp *spec.Spec, d *decide.Decision, inputs map[string]string) store.Record {
	return store.Record{
		TraceID: d.TraceID, SpecID: d.SpecID, SpecHash: d.SpecHash, Type: d.Type,
		Model: d.Model, Endpoint: d.Endpoint,
		Inputs: copyInputs(inputs), InputsHash: d.InputsHash,
		Raw: d.Raw, Calibrated: d.Calibrated, IsCalibrated: d.IsCalibrated,
		Top: d.Top, P: d.P, Band: string(d.Band), Tainted: d.Tainted, Rung: d.Rung, Minimal: d.Minimal, At: d.At,
	}
}

// recordDecision records a decision. When the context rung reaches confidence (act), that answer is kept as a truth for the minimal question:
// the upper rung's answer becomes the lower rung's truth (weight is the calibrated probability, source is context). If a person answers later, theirs is newer and wins.
func recordDecision(st store.Store, sp *spec.Spec, d *decide.Decision, inputs map[string]string) error {
	return recordDecisionRec(st, sp, d, recordOf(sp, d, inputs))
}

// recordDecisionRec writes rec (recordOf plus marks; RedoOf for a redo). This is the one place that defines how decisions are recorded (audit 6 item 5).
func recordDecisionRec(st store.Store, sp *spec.Spec, d *decide.Decision, rec store.Record) error {
	if err := st.AppendRecord(rec); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	if d.Rung != "context" || d.Band != decide.Act || d.Minimal == nil {
		return nil
	}
	if sp.Type == spec.Score && sp.Thresholds.Level > 0 {
		return nil // P is P(level ≥ k) and Top is the highest level. They are different events, so they cannot become one truth (audit round 3 item 8)
	}
	inputsHash := d.Minimal.InputsHash
	if inputsHash == "" {
		inputsHash = rec.InputsHash
	}
	l := store.Label{
		TraceID: rec.TraceID, SpecID: rec.SpecID, SpecHash: d.Minimal.SpecHash, Type: rec.Type, Model: rec.Model,
		InputsHash: inputsHash, Inputs: rec.Inputs, Raw: d.Minimal.Raw,
		Truth: d.Top, Weight: d.P, Source: "context", Tainted: rec.Tainted, Rung: "minimal", At: rec.At,
	}
	if err := st.AppendLabel(l); err != nil {
		return fmt.Errorf("record context truth: %w", err)
	}
	return nil
}

// attachReceipt attaches a receipt to an act decision: that rung's line (method, date, number of truths), and the number and precision of act decisions a person checked (human truths in the store only).
func attachReceipt(sp *spec.Spec, st store.Store, d *decide.Decision) {
	if d.Band != decide.Act {
		return
	}
	rs := sp
	if d.Rung == "context" && sp.ContextSpec() != nil {
		rs = sp.ContextSpec()
	}
	c := rs.CalibrationFor(d.Model)
	if c == nil {
		return
	}
	r := &decide.Receipt{SpecHash: c.SpecHash, Model: c.Model, Method: c.Method, FittedAt: c.FittedAt, N: c.N, NHuman: c.NHuman, PrecisionAtFit: c.PrecisionAt}
	if labels, err := st.Labels(sp.ID, c.SpecHash, d.Model); err == nil {
		var human []store.Label
		for _, l := range store.LatestPerInput(labels) {
			if store.HumanSource(l.Source) {
				human = append(human, l)
			}
		}
		if len(human) > 0 {
			r.HumanPrecision, _, r.HumanCheckedAct = evaluateCalibration(rs, *c, human)
		}
	}
	d.Receipt = r
}
