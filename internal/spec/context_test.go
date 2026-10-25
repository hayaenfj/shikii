package spec

import (
	"strings"
	"testing"
)

// state.context is a layout that "adds context and asks again only when unsure". It uses every required input and adds one or more inputs not in template.
func TestContextTemplateMustAddAnInputAndKeepRequired(t *testing.T) {
	sp := choiceSpec()
	sp.State.Context = "Previous turn:\n{{ previous }}\n\nMessage:\n{{ message }}"
	if err := sp.Validate(); err != nil {
		t.Fatalf("a context template that adds an input must validate: %v", err)
	}
	if got := sp.State.ContextInputs(); len(got) != 1 || got[0] != "previous" {
		t.Fatalf("the added inputs are the placeholders beyond required: %v", got)
	}
	bad := map[string]string{
		"drops the required input": "Previous turn:\n{{ previous }}",
		"adds nothing":             "Message again:\n{{ message }}",
		"adds only text":           "Think carefully.\n{{ message }}",
	}
	for name, tpl := range bad {
		sp := choiceSpec()
		sp.State.Context = tpl
		if err := sp.Validate(); err == nil || !strings.Contains(err.Error(), "state.context") {
			t.Fatalf("%s must be rejected with a state.context error, got %v", name, err)
		}
	}
}

// The context question has its own hash. The minimal question's hash does not change when context is added (existing lines stay).
// One spec can hold calibration for both steps, and only lines for old hashes are replaced.
func TestContextRungHasItsOwnHashAndCalibration(t *testing.T) {
	sp := choiceSpec()
	before := sp.Hash()
	if sp.HasContext() || sp.ContextHash() != "" || sp.ContextSpec() != nil {
		t.Fatalf("a spec without state.context has no context rung")
	}
	sp.State.Context = "Previous turn:\n{{ previous }}\n\nMessage:\n{{ message }}"
	if sp.Hash() != before {
		t.Fatalf("adding state.context must not change the minimal question's hash: %s → %s", before, sp.Hash())
	}
	csp := sp.ContextSpec()
	if csp == nil || csp.Hash() != sp.ContextHash() || csp.Hash() == sp.Hash() {
		t.Fatalf("the context rung is the spec asked with the context template, under its own hash: %+v", csp)
	}
	if csp.State.Template != sp.State.Context || csp.State.Context != "" || strings.Join(csp.State.Required, ",") != "message,previous" {
		t.Fatalf("the context spec asks with the context template and requires the added inputs: %+v", csp.State)
	}
	if !sp.LiveHash(sp.Hash()) || !sp.LiveHash(sp.ContextHash()) || sp.LiveHash("stale") {
		t.Fatalf("both rungs' hashes are live; others are stale")
	}
	// WithVariant carries context over (the adopted version also asks in two steps). WithoutContext asks only the minimal question.
	v := sp.WithVariant(Variant{Name: "t", Template: "msg: {{ message }}"})
	if v.State.Context != sp.State.Context {
		t.Fatalf("a template variant must keep the context template: %+v", v.State)
	}
	if m := sp.WithoutContext(); m.HasContext() || m.Hash() != sp.Hash() {
		t.Fatalf("WithoutContext is the minimal question under the same hash: %+v", m.State)
	}
	// calibration: two step lines coexist for the same model, and the old-hash line is replaced
	sp.PutCalibration(Calibration{Model: "m", SpecHash: "stale", Method: "identity"})
	sp.PutCalibration(Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity"})
	sp.PutCalibration(Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "identity"})
	sp.PutCalibration(Calibration{Model: "other", SpecHash: sp.Hash(), Method: "identity"})
	if len(sp.Calibration) != 3 {
		t.Fatalf("one line per (model, rung), stale lines replaced: %+v", sp.Calibration)
	}
	if sp.CalibrationFor("m") == nil || sp.CalibrationFor("m").SpecHash != sp.Hash() {
		t.Fatalf("the minimal spec finds its own line")
	}
	if c := sp.ContextSpec().CalibrationFor("m"); c == nil || c.SpecHash != sp.ContextHash() {
		t.Fatalf("the context spec finds the context line")
	}
	sp.PutCalibration(Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "vector", Temperature: 1})
	if len(sp.Calibration) != 3 || sp.ContextSpec().CalibrationFor("m").Method != "vector" {
		t.Fatalf("re-fitting a rung replaces that rung's line only: %+v", sp.Calibration)
	}
}
