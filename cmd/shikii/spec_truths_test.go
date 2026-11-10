package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// truthsFixture gives prompt-kind a wording variant, decisions by two models on the current wording, and truths for both models, an
// older wording and another model, with one line that is not JSON in each file. No line is drawn, and there are fewer truths than
// min_n: the checks read everything and start nothing.
func truthsFixture(t *testing.T) (agent string, st store.Store, sp *spec.Spec) {
	t.Helper()
	repo := gitRepo(t)
	agent = hookEnv(t, nil, repo)
	st = store.Store{Dir: agent}
	path := hookSpecPath(agent, "prompt-kind")
	sp = mustLoad(t, path)
	sp.Variants = append(sp.Variants, spec.Variant{Name: "plain-layout", Layout: "plain"})
	data, err := yaml.Marshal(sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sp = mustLoad(t, path)
	if len(sp.Variants) == 0 {
		t.Fatal("the fixture needs a wording variant, or the scoring check reads nothing")
	}
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i, key := range []struct{ hash, model string }{{sp.Hash(), "model-a"}, {sp.Hash(), "model-b"}, {"older-wording", "model-a"}} {
		for j := range 3 {
			inputs := map[string]string{"request": fmt.Sprintf("request %d-%d", i, j)}
			r := store.Record{TraceID: fmt.Sprintf("trace-%d-%d", i, j), SpecID: sp.ID, SpecHash: key.hash, Type: string(sp.Type), Model: key.model,
				InputsHash: store.HashInputs(inputs), Raw: map[string]float64{"change_request": 0.7, "question": 0.3}, Top: "change_request", P: 0.7, Band: []string{"ask", "act", "act"}[j], Project: []string{"alpha", "beta", ""}[i], At: at.Add(time.Duration(i*10+j) * time.Minute)}
			if err := st.AppendRecord(r); err != nil {
				t.Fatal(err)
			}
			source := []string{"ui", "outcome", "ask"}[j]
			if err := st.AppendLabel(store.Label{TraceID: r.TraceID, SpecID: sp.ID, SpecHash: key.hash, Type: r.Type, Model: key.model, InputsHash: r.InputsHash,
				Raw: r.Raw, Truth: []string{"change_request", "question"}[j%2], Weight: 1, Source: source, At: r.At.Add(time.Second)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, sub := range []string{"records", "labels"} {
		f, err := os.OpenFile(filepath.Join(agent, sub, sp.ID+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("{not json\n"); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	return agent, st, sp
}

// What the shared read answers is what the store answers: the same records, the same labels, and for every wording and model the
// same lines in the same order, including a key with no labels and a spec with no files at all.
func TestSpecTruthsAnswerLikeTheStore(t *testing.T) {
	_, st, sp := truthsFixture(t)
	truths := newSpecTruths(st, sp.ID)
	wantRecords, err := st.Records(sp.ID, 0)
	if err != nil || len(wantRecords) != 9 {
		t.Fatalf("the fixture's records: %d %v", len(wantRecords), err)
	}
	if got, err := truths.records(); err != nil || !reflect.DeepEqual(got, wantRecords) {
		t.Fatalf("records() is Records(spec, 0): %v", err)
	}
	_, wantAll, err := st.Counts(sp.ID)
	if err != nil || len(wantAll) != 9 {
		t.Fatalf("the fixture's labels: %d %v", len(wantAll), err)
	}
	if got, err := truths.allLabels(); err != nil || !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("allLabels() is the labels of Counts: %v", err)
	}
	for _, key := range []struct{ hash, model string }{{sp.Hash(), "model-a"}, {sp.Hash(), "model-b"}, {"older-wording", "model-a"}, {sp.Hash(), "model-unknown"}, {"", ""}} {
		want, err := st.Labels(sp.ID, key.hash, key.model)
		if err != nil {
			t.Fatal(err)
		}
		got, err := truths.labelsFor(key.hash, key.model)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("labelsFor(%q, %q) keeps the lines Labels keeps:\n got %d %v\nwant %d", key.hash, key.model, len(got), err, len(want))
		}
	}
	empty := newSpecTruths(st, "no-such-spec")
	if recs, err := empty.records(); err != nil || len(recs) != 0 {
		t.Fatalf("a spec without files has no records and no error: %v %v", recs, err)
	}
	if labels, err := empty.labelsFor("h", "m"); err != nil || labels != nil {
		t.Fatalf("a spec without files has no labels and no error: %v %v", labels, err)
	}
}

// At Stop the two checks of one spec scan its records file twice (once decoded, once counted by Counts) and its labels file once,
// however many models decided on the current wording. They used to scan 12 times for this fixture.
func TestStopChecksScanASpecsFilesOnce(t *testing.T) {
	agent, st, sp := truthsFixture(t)
	wantTruths, err := humanTruthsFor(st, sp)
	if err != nil || wantTruths == 0 {
		t.Fatalf("the fixture has truths by a person on the current wording: %d %v", wantTruths, err)
	}
	before := store.JSONLScans()
	if cerr, terr := stopSpecChecks(agent, sp.ID); cerr != nil || terr != nil {
		t.Fatalf("the checks run: %v %v", cerr, terr)
	}
	if scans := store.JSONLScans() - before; scans != 3 {
		t.Fatalf("one spec's records and labels are scanned 3 times in all at Stop, got %d", scans)
	}
}

// Once a calibration is started the earlier reads are dropped: a truth it writes is counted by the check that follows.
func TestSpecTruthsForgetAfterACalibrationStarts(t *testing.T) {
	agent, st, sp := truthsFixture(t)
	path := lowerMinN(t, agent, sp.ID, 1)
	sp = mustLoad(t, path)
	old := hookCalibrate
	t.Cleanup(func() { hookCalibrate = old })
	started := 0
	hookCalibrate = func(agent, path, storeDir, model string) {
		started++
		inputs := map[string]string{"request": fmt.Sprintf("written while calibrating %s", model)}
		if err := st.AppendLabel(store.Label{TraceID: "calibration-" + model, SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: model,
			InputsHash: store.HashInputs(inputs), Truth: "question", Weight: 1, Source: "ui", At: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	truths := newSpecTruths(st, sp.ID)
	beforeCalibration, err := humanTruthsWith(truths, sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := hookMaybeCalibrateWith(agent, sp.ID, truths); err != nil {
		t.Fatal(err)
	}
	if started == 0 {
		t.Fatal("the fixture starts a calibration (min_n is 1 and no line is drawn)")
	}
	got, err := humanTruthsWith(truths, sp)
	if err != nil {
		t.Fatal(err)
	}
	want, err := humanTruthsFor(st, sp)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || got <= beforeCalibration {
		t.Fatalf("the truths written by the calibration are counted after it: shared read %d, the store %d, before the calibration %d", got, want, beforeCalibration)
	}
}
