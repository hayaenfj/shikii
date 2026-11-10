package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// liftedLineFixture is a prompt-kind-like question whose 60 truths are edit or explain only (run, decide and investigate are never
// the truth), saved for one model on the current wording.
func liftedLineFixture(t *testing.T) (*spec.Spec, store.Store, []store.Label) {
	t.Helper()
	pii := false
	sp := &spec.Spec{
		ID: "prompt-kind", Type: spec.Choice, Question: "What will the agent end up doing?",
		Options: []spec.Option{{ID: "edit", Description: "edits files"}, {ID: "explain", Description: "explains"},
			{ID: "investigate", Description: "investigates"}, {ID: "run", Description: "runs commands"}, {ID: "decide", Description: "a reply to a proposal"}},
		State:      spec.State{Template: "request: {{ request }}", Required: []string{"request"}},
		Policy:     spec.Policy{SendTo: []string{"local"}, PII: &pii},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.60},
		Eval:       spec.Eval{MinN: 50},
	}
	if err := sp.Validate(); err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: t.TempDir()}
	var labels []store.Label
	for i := 0; i < 60; i++ {
		truth, second := "edit", "explain"
		if i%3 == 0 {
			truth, second = "explain", "edit"
		}
		top := 0.70 + 0.05*float64(i%4)
		raw := map[string]float64{truth: top, second: 1 - top - 0.01 - 2e-6, "investigate": 0.01, "run": 1e-6, "decide": 1e-6}
		if i%8 == 0 {
			raw[truth], raw[second] = raw[second], raw[truth]
		}
		inputs := map[string]string{"request": fmt.Sprintf("request %d", i)}
		l := store.Label{TraceID: fmt.Sprintf("trace-%02d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: "model-a",
			InputsHash: store.HashInputs(inputs), Raw: raw, Truth: truth, Weight: 1, Source: "ui", At: time.Date(2026, 11, 1, 0, i, 0, 0, time.UTC)}
		labels = append(labels, l)
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	return sp, st, labels
}

// check fails a line that lifts an option no truth ever confirmed (the line calibrate wrote before it capped such options): the
// line passes every other check, because no label shows what it does to that option.
func TestCheckFailsALineThatLiftsAnOptionWithoutTruths(t *testing.T) {
	sp, st, _ := liftedLineFixture(t)
	sp.Calibration = []spec.Calibration{{Model: "model-a", SpecHash: sp.Hash(), Method: "vector", Temperature: 0.648,
		Biases:      map[string]float64{"decide": 5.668614548791031, "edit": -3.863726573257443, "explain": -4.238980865121212, "investigate": -3.234521659203408, "run": 5.668614548791031},
		PrecisionAt: 0.94, N: 60, NHuman: 60, FittedAt: "2026-11-01"}}
	var problems []string
	capture(t, func() error { problems = checkSpec(sp, st); return nil })
	if len(problems) != 1 || !strings.Contains(problems[0], `option "decide"`) || !strings.Contains(problems[0], "calibrate --write") {
		t.Fatalf("check names the lifted option and how to redraw the line, and reports nothing else: %q", problems)
	}
}

// The line calibrate writes now for the same truths passes check, and so does a line whose options all have truths, whatever its
// biases are.
func TestCheckPassesLinesThatLiftNoOptionWithoutTruths(t *testing.T) {
	sp, st, labels := liftedLineFixture(t)
	var report strings.Builder
	cal, ok, err := verifyTop(sp, labels, 10, &report)
	if err != nil || !ok || cal.Method != "vector" {
		t.Fatalf("calibrate draws a vector line for the fixture: %+v %v %v", cal, ok, err)
	}
	cal.Model, cal.SpecHash, cal.N, cal.NHuman, cal.FittedAt = "model-a", sp.Hash(), 60, 60, "2026-11-10"
	sp.Calibration = []spec.Calibration{cal}
	var problems []string
	capture(t, func() error { problems = checkSpec(sp, st); return nil })
	if len(problems) != 0 {
		t.Fatalf("the line calibrate writes now passes check: %q (biases %v)", problems, cal.Biases)
	}
	everyOption := []store.Label{{InputsHash: "1", Truth: "a"}, {InputsHash: "2", Truth: "b"}, {InputsHash: "3", Truth: "c"}}
	if option, _, _, lifted := liftedWithoutTruth(spec.Calibration{Method: "vector", Biases: map[string]float64{"a": -2, "b": -1, "c": 3}}, everyOption); lifted {
		t.Fatalf("every option has a truth, so no bias is reported: %s", option)
	}
	if option, _, _, lifted := liftedWithoutTruth(spec.Calibration{Method: "vector", Biases: map[string]float64{"a": 1, "b": -1, "c": -0.5}}, everyOption[:2]); lifted {
		t.Fatalf("an option without truths below the mean of the others is not lifted: %s", option)
	}
}
