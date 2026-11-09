package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// labelsFromFile converts a labeled file into store.Label form. If any rows were asked of the model, it also returns that actual model name.
// For rows without Raw, reuse a store label with the same input (inputs_hash) under the --model key (so recalibration works without the model).
func labelsFromFile(sp *spec.Spec, path string, ef endpointFlags, st store.Store, reports ...io.Writer) ([]store.Label, string, error) {
	stdout := reportWriter(reports...)
	if path == "" {
		return nil, "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var rows []labeled
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	eps := ef.endpoints()
	sess := decide.Session{Tainted: ef.tainted}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Earlier labels for the same input (to reuse raw and time). If several, take the oldest = the first time it was asked.
	// Borrowing the time of a correction a person added later would give the file's truth the same time and make it win in LatestPerInput (audit C2).
	stored := map[string]store.Label{}
	if prior, err := st.Labels(sp.ID, sp.Hash(), ef.model); err == nil {
		for _, l := range prior {
			if cur, ok := stored[l.InputsHash]; !ok || l.At.Before(cur.At) {
				stored[l.InputsHash] = l
			}
		}
	}
	// A file row's time is "when that input was first brought in" (the oldest label under the current wording, any model).
	// Using "now" every time would make it newer than a person's later correction, so it would win.
	firstSeen := map[string]time.Time{}
	if _, all, err := st.Counts(sp.ID); err == nil {
		for _, l := range all {
			if l.SpecHash != sp.Hash() || l.InputsHash == "" {
				continue
			}
			if cur, ok := firstSeen[l.InputsHash]; !ok || l.At.Before(cur) {
				firstSeen[l.InputsHash] = l.At
			}
		}
	}
	reused := 0

	model := ""
	out := make([]store.Label, 0, len(rows))
	for i, r := range rows {
		truth, err := parseTruth(sp.Type, r.Truth)
		if err != nil {
			return nil, "", fmt.Errorf("row %d: %w", i, err)
		}
		l := store.Label{SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Truth: truth, Weight: r.Weight, Source: "file", At: time.Now()}
		given, err := rawFromRow(sp, r.Raw)
		if err != nil {
			return nil, "", fmt.Errorf("row %d: %w", i, err)
		}
		switch {
		case given != nil:
			l.Raw = given // probability brought in: the model is not called
		case func() bool { _, ok := stored[store.HashInputs(r.Inputs)]; return ok }():
			prior := stored[store.HashInputs(r.Inputs)]
			l.Raw, l.Model, l.TraceID, l.InputsHash = prior.Raw, prior.Model, prior.TraceID, prior.InputsHash
			l.Inputs = copyInputs(r.Inputs)
			if !prior.At.IsZero() {
				l.At = prior.At
			}
			if model == "" {
				model = prior.Model
			}
			reused++
		default:
			d, err := decide.Decide(ctx, sp.WithoutContext(), r.Inputs, sess, eps) // the file's truths belong to the minimal-question rung
			if err != nil {
				return nil, "", fmt.Errorf("row %d: %w", i, err)
			}
			if d.Band == decide.Abstain {
				return nil, "", fmt.Errorf("row %d: abstained: %s", i, d.Reason)
			}
			l.Raw, l.Model, l.TraceID, l.InputsHash = d.Raw, d.Model, d.TraceID, d.InputsHash
			l.Inputs = copyInputs(r.Inputs) // keep the text along with the truth (material for re-asking)
			if model == "" {
				model = d.Model
			} else if model != d.Model {
				return nil, "", fmt.Errorf("row %d: model changed mid-run (%s → %s)", i, model, d.Model)
			}
		}
		if l.Weight <= 0 {
			l.Weight = 1
		}
		if first, ok := firstSeen[l.InputsHash]; ok && l.InputsHash != "" {
			l.At = first
		}
		out = append(out, l)
	}
	if reused > 0 {
		fmt.Fprintf(stdout, "reused %d stored labels for file rows (model=%s); queried %d\n", reused, ef.model, len(out)-reused)
	}
	return out, model, nil
}

// alreadyStored reports whether a label with the same (hash, model, inputs_hash) is in the store.
func storedInputs(st store.Store, sp *spec.Spec, model string) (map[string]bool, error) {
	existing, err := st.Labels(sp.ID, sp.Hash(), model)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(existing))
	for _, e := range existing {
		out[e.InputsHash] = true
	}
	return out, nil
}

// replayTruths brings the "saved truths" to the current (spec_hash, model).
// From all of the spec's labels in the store (including old hashes and other models), take the latest truth per input,
// and for those whose record still has its text, ask the current model again with the current wording and turn them into labels.
// Inputs in seen (handled on the file side) are skipped. If model is empty, return the model name of the first answer.
// Truths that cannot be re-asked (no text) are counted and the reason printed. Never silently 0.
func replayTruths(sp *spec.Spec, st store.Store, ef endpointFlags, model string, seen map[string]bool, reports ...io.Writer) ([]store.Label, string, error) {
	stdout := reportWriter(reports...)
	_, all, err := st.Counts(sp.ID)
	if err != nil {
		return nil, model, err
	}
	// The latest truth per input (if one input has several, the newer one)
	type truth struct {
		truth  string
		weight float64
		at     time.Time
	}
	bank := map[string]truth{}
	var human []store.Label
	eligible := store.WithoutUntrustedOutcome(all, store.OutcomeAgreement(all), sp.Thresholds.Act)
	for _, l := range eligible {
		if store.HumanSource(l.Source) { // do not re-ask machine truths (upper-rung answers): re-asking at another rung gives the same model's answer and it would pass as a human truth (audit round 3 item 1)
			human = append(human, l)
		}
	}
	for _, l := range store.LatestPerInput(human) {
		if l.InputsHash == "" || seen[l.InputsHash] {
			continue
		}
		bank[l.InputsHash] = truth{l.Truth, l.Weight, l.At}
	}
	if len(bank) == 0 {
		return nil, model, nil
	}
	// Do not re-ask what already exists for the current (hash, model). If model is not set yet, settle it with the first answer and then check.
	current := map[string]bool{}
	loadCurrent := func(m string) error {
		have, err := st.Labels(sp.ID, sp.Hash(), m)
		if err != nil {
			return err
		}
		for _, l := range have {
			current[l.InputsHash] = true
		}
		return nil
	}
	// Even with no model set, check once with the --model value (it has a default). If the actual model name differs, check again after the first answer.
	guess := model
	if guess == "" {
		guess = ef.model
	}
	if err := loadCurrent(guess); err != nil {
		return nil, model, err
	}
	texts, err := st.InputsByHash(sp.ID)
	if err != nil {
		return nil, model, err
	}
	eps := ef.endpoints()
	ctx, cancel := context.WithTimeout(context.Background(), replayLimit)
	defer cancel()

	var out []store.Label
	noText, noEndpoint, noContext, invalid := 0, 0, 0, 0
	rs := sp.WithoutContext() // re-ask at this rung (this hash) only. Moving up a rung would give an answer for another hash
	hashes := make([]string, 0, len(bank))
	for h := range bank {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	for _, h := range hashes {
		if current[h] {
			continue
		}
		stored, ok := texts[h]
		if !ok {
			noText++
			continue
		}
		inputs := stored.Inputs
		if len(eps) == 0 {
			noEndpoint++
			continue
		}
		// Text recorded as tainted is never sent to hosted, even if the current session is not tainted (audit S1)
		sess := decide.Session{Tainted: ef.tainted || stored.Tainted}
		d, err := decide.Decide(ctx, rs, inputs, sess, eps)
		if err != nil {
			return nil, model, fmt.Errorf("replay %s: %w", h, err)
		}
		if d.Band == decide.Abstain {
			if strings.HasPrefix(d.Reason, "missing required input") {
				noContext++ // re-asking at the context rung: no context to add was recorded with this text
			} else {
				noEndpoint++
			}
			continue
		}
		if model == "" {
			model = d.Model
			if err := loadCurrent(model); err != nil {
				return nil, model, err
			}
			if current[h] {
				continue
			}
		} else if d.Model != model {
			return nil, model, fmt.Errorf("replay: model changed mid-run (%s → %s)", model, d.Model)
		}
		rec := recordOf(sp, d, inputs)
		t := bank[h]
		l, err := store.LabelFor(&rec, t.truth, t.weight, "replay")
		if err != nil {
			invalid++ // e.g. the truth is an option not in the current spec. Count it instead of silently dropping it
			continue
		}
		out = append(out, l)
		current[h] = true
	}
	if len(out) > 0 {
		fmt.Fprintf(stdout, "replayed %d earlier truths against the current wording and model (hash=%s, model=%s); no human labels needed\n", len(out), sp.Hash(), model)
	}
	if noText > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) cannot be replayed: no text stored for them (recorded before text was kept)\n", noText)
	}
	if noEndpoint > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) not replayed: no endpoint to ask (pass --local or a key for --hosted)\n", noEndpoint)
	}
	if noContext > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) not replayed on this rung: their text has no context inputs (%s)\n", noContext, strings.Join(sp.State.Required, ", "))
	}
	if invalid > 0 {
		fmt.Fprintf(stdout, "note: %d earlier truth(s) skipped: their answer no longer exists in the spec\n", invalid)
	}
	return out, model, nil
}
