package decide

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// A yes/no answer that carries no value is not an answer of zero: read as zero it becomes top=yes p=0 band=discard,
// a confident "no" that is recorded and counted. Decide must return an error, as it does when the model call fails
// and as it does for a choice answer without a usable distribution. A value outside 0..1 is not a probability either.
func TestDecideRefusesAYesNoAnswerWithoutAUsableValue(t *testing.T) {
	for _, c := range []struct{ name, answer string }{
		{"no noul field", `{"type":"noul"}`},
		{"null noul", `{"type":"noul","noul":null}`},
		{"a choice answer to a yes/no question", `{"type":"choice","choice":"a","probabilities":{"a":0.9,"b":0.1}}`},
		{"an empty answer", `{}`},
		{"a negative value", `{"type":"noul","noul":-0.2}`},
		{"a value above one", `{"type":"noul","noul":1.5}`},
	} {
		d, err := Decide(context.Background(), noulSpec("local"), okInputs, Session{}, answerServer(t, c.answer))
		if err == nil {
			t.Errorf("%s: want an error, got top=%q p=%v band=%s raw=%v", c.name, d.Top, d.P, d.Band, d.Raw)
			continue
		}
		if !strings.Contains(err.Error(), "probabilities") {
			t.Errorf("%s: the error must name what is wrong with the answer, in the words used for a choice answer: %v", c.name, err)
		}
	}
	// The raw read used by measurements goes through the same check.
	if raw, _, err := AskOrdered(context.Background(), noulSpec("local"), "x", nil, answerServer(t, `{"type":"noul"}`)[0].Client); err == nil {
		t.Errorf("AskOrdered: want an error for a yes/no answer without a value, got %v", raw)
	}
}

// With state.context, a context answer without a value is a failed call of that rung: the minimal answer stays,
// as it does for a choice answer without a usable distribution.
func TestDecideKeepsTheMinimalAnswerWhenTheContextYesNoAnswerHasNoValue(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"noul","noul":0.6}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"noul"}}}`))
	}))
	t.Cleanup(srv.Close)
	sp := noulSpec("local")
	sp.State.Context = "1: {{ a }}\n2: {{ b }}\nBefore: {{ previous }}"
	inputs := map[string]string{"a": okInputs["a"], "b": okInputs["b"], "previous": "y"}
	d, err := Decide(context.Background(), sp, inputs, Session{}, []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || d.Top != "yes" || d.P != 0.6 || d.Band != Ask || d.Rung != "minimal" || d.Minimal != nil || d.SpecHash != sp.Hash() || !strings.Contains(d.Reason, "context rung failed") {
		t.Fatalf("want the minimal answer with the context failure as the reason, got calls=%d %+v", calls, d)
	}
}

// stepsServer is a model that answers the two yes/no steps of a broken-down choice with the given JSON.
func stepsServer(t *testing.T, first, second string) []Endpoint {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","answers":{"step-0":` + first + `,"step-1":` + second + `}}`))
	}))
	t.Cleanup(srv.Close)
	return []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
}

func twoSteps() *spec.Spec {
	sp := threeWay()
	sp.Steps = []spec.Step{{Ask: "Is it anything but C?", No: "c"}, {Ask: "Is it A?", Yes: "a", No: "b"}}
	return sp
}

// A broken-down choice is asked as yes/no steps. A step without a value read as zero sends the whole remaining
// mass down the "no" branch: a confident option nobody answered.
func TestDecideRefusesABrokenDownChoiceWithAStepWithoutAUsableValue(t *testing.T) {
	for _, c := range []struct{ name, first, second string }{
		{"the last step has no value", `{"type":"noul","noul":0.9}`, `{"type":"noul"}`},
		{"the first step is null", `{"type":"noul","noul":null}`, `{"type":"noul","noul":0.9}`},
		{"a step is a choice answer", `{"type":"noul","noul":0.9}`, `{"type":"choice","probabilities":{"a":0.9,"b":0.1}}`},
		{"a step above one", `{"type":"noul","noul":1.5}`, `{"type":"noul","noul":0.9}`},
		{"a negative step", `{"type":"noul","noul":0.9}`, `{"type":"noul","noul":-0.5}`},
	} {
		d, err := Decide(context.Background(), twoSteps(), map[string]string{"m": "x"}, Session{}, stepsServer(t, c.first, c.second))
		if err == nil {
			t.Errorf("%s: want an error, got top=%q p=%v band=%s raw=%v", c.name, d.Top, d.P, d.Band, d.Raw)
			continue
		}
		if !strings.Contains(err.Error(), "probabilities") {
			t.Errorf("%s: the error must name what is wrong with the answer: %v", c.name, err)
		}
	}
}

// Guard: a value of exactly zero is an answer (the model said "no"), and answers with a value decide as before.
func TestDecideKeepsAYesNoAnswerThatCarriesAValue(t *testing.T) {
	for _, c := range []struct {
		answer string
		p      float64
		band   Band
	}{
		{`{"type":"noul","noul":0}`, 0, Discard},
		{`{"type":"noul","noul":0.0}`, 0, Discard},
		{`{"type":"noul","noul":0.05}`, 0.05, Discard},
		{`{"type":"noul","noul":0.6}`, 0.6, Ask},
		{`{"type":"noul","noul":1}`, 1, Ask}, // uncalibrated: never act
	} {
		d, err := Decide(context.Background(), noulSpec("local"), okInputs, Session{}, answerServer(t, c.answer))
		if err != nil {
			t.Fatalf("%s: %v", c.answer, err)
		}
		if d.Top != "yes" || d.P != c.p || d.Band != c.band || d.Raw["yes"] != c.p || d.Raw["no"] != 1-c.p {
			t.Fatalf("%s: got top=%q p=%v band=%s raw=%v, want yes %v %s", c.answer, d.Top, d.P, d.Band, d.Raw, c.p, c.band)
		}
	}
	raw, _, err := AskOrdered(context.Background(), noulSpec("local"), "x", nil, answerServer(t, `{"type":"noul","noul":0}`)[0].Client)
	if err != nil || raw["yes"] != 0 || raw["no"] != 1 {
		t.Fatalf("AskOrdered must keep returning the raw distribution of a zero answer: %v %v", raw, err)
	}
}

// Guard: a fake model that builds its answers from the Answer type keeps working, a zero included.
func TestDecideKeepsAYesNoAnswerBuiltFromTheAnswerType(t *testing.T) {
	for _, p := range []float64{0, 0.6} {
		f := newFake(t, "m", systemone.Answer{Type: "noul", Noul: p})
		d, err := Decide(context.Background(), noulSpec("local"), okInputs, Session{}, []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "m")}})
		if err != nil {
			t.Fatalf("p=%v: %v", p, err)
		}
		if d.Top != "yes" || d.P != p {
			t.Fatalf("p=%v: got top=%q p=%v band=%s", p, d.Top, d.P, d.Band)
		}
	}
}

// Guard: broken-down steps that carry their values decide as before, a step of exactly zero included.
func TestDecideKeepsABrokenDownChoiceWhoseStepsCarryValues(t *testing.T) {
	d, err := Decide(context.Background(), twoSteps(), map[string]string{"m": "x"}, Session{}, stepsServer(t, `{"type":"noul","noul":1}`, `{"type":"noul","noul":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Top != "b" || d.P != 1 || d.Raw["a"] != 0 || d.Raw["c"] != 0 {
		t.Fatalf("a zero on the last step is a certain b: %+v", d)
	}
	d, err = Decide(context.Background(), twoSteps(), map[string]string{"m": "x"}, Session{}, stepsServer(t, `{"type":"noul","noul":0.9}`, `{"type":"noul","noul":0.8}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Top != "a" || d.P < 0.7199 || d.P > 0.7201 {
		t.Fatalf("0.9 x 0.8 is a at 0.72: %+v", d)
	}
}
