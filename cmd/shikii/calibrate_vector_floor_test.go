package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/probability"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// The line calibrate would write for a prompt-kind-like question must not turn "edit 0.80, run 0.04" into an
// automatic "run". The truths are fixed: edit or explain, the model right most of the time, run and decide always
// at the floor a readout gives a label outside its top list (1e-6).
func TestCalibrateDoesNotWriteALineThatActsOnAnOptionSeenOnlyAtTheFloor(t *testing.T) {
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
	var labels []store.Label
	for i := 0; i < 60; i++ {
		truth, second := "edit", "explain"
		if i%3 == 0 {
			truth, second = "explain", "edit"
		}
		top := 0.70 + 0.05*float64(i%4)
		raw := map[string]float64{truth: top, second: 1 - top - 0.01 - 2e-6, "investigate": 0.01, "run": 1e-6, "decide": 1e-6}
		if i%8 == 0 { // the model is wrong now and then, on its least sure answers (top 0.70)
			raw[truth], raw[second] = raw[second], raw[truth]
		}
		labels = append(labels, store.Label{Raw: raw, Truth: truth, Weight: 1, Source: "ui"})
	}
	var report strings.Builder
	cal, ok, err := verifyTop(sp, labels, 10, &report)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("this fixture is meant to pass calibrate's own checks (precision at act): %+v\n%s", cal, report.String())
	}
	t.Logf("calibrate would write: method=%s temperature=%.3f biases=%v precision_at_act=%.3f", cal.Method, cal.Temperature, cal.Biases, cal.PrecisionAt)
	raw := map[string]float64{"edit": 0.80, "explain": 0.15, "investigate": 0.01, "run": 0.04, "decide": 1e-6}
	dist := raw
	if cal.Method == "vector" {
		dist = calib.Vector{Temperature: cal.Temperature, Biases: cal.Biases}.Apply(raw)
	}
	top, p := probability.Argmax(dist)
	if top != "edit" {
		band := "ask"
		if p >= sp.Thresholds.Act {
			band = "act"
		}
		t.Fatalf("the model answered edit at 0.80 and run at 0.04; with the written line the decision is %s at %.4f (band %s)", top, p, band)
	}
}
