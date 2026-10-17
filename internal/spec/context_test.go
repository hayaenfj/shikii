package spec

import (
	"strings"
	"testing"
)

// state.context は「迷った時だけ文脈を足して聞き直す」並べ方。required の入力を全部使い、template に無い入力を 1 つ以上足す。
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

// 文脈を足した聞き方は別の指紋を持つ。最小の質問の指紋は context を足しても変わらない(今ある線は消えない)。
// 較正は両方の段を 1 つの spec に持て、古い指紋の線だけが置き換わる。
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
	// WithVariant は context を引き継ぐ(採用した版も 2 段で聞く)。WithoutContext は最小の質問だけ。
	v := sp.WithVariant(Variant{Name: "t", Template: "msg: {{ message }}"})
	if v.State.Context != sp.State.Context {
		t.Fatalf("a template variant must keep the context template: %+v", v.State)
	}
	if m := sp.WithoutContext(); m.HasContext() || m.Hash() != sp.Hash() {
		t.Fatalf("WithoutContext is the minimal question under the same hash: %+v", m.State)
	}
	// 較正: 同じモデルで 2 段の線が共存し、古い指紋の線は置き換わる
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
