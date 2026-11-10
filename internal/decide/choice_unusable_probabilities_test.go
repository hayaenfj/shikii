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

// answerServer is a model that answers every question with the given JSON for the answer "q".
func answerServer(t *testing.T, answer string) []Endpoint {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":` + answer + `}}`))
	}))
	t.Cleanup(srv.Close)
	return []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
}

func threeWay() *spec.Spec {
	return &spec.Spec{
		ID: "c", Type: spec.Choice, Question: "which?",
		Options:    []spec.Option{{ID: "a", Description: "A"}, {ID: "b", Description: "B"}, {ID: "c", Description: "C"}},
		State:      spec.State{Template: "{{ m }}", Required: []string{"m"}},
		Policy:     spec.Policy{SendTo: []string{"local"}, PII: boolPtr(false)},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
	}
}

func threeLevels() *spec.Spec {
	sp := threeWay()
	sp.Type, sp.Options, sp.Levels = spec.Score, nil, []string{"low", "middle", "high"}
	return sp
}

// A choice or score answer that carries no usable distribution is not an answer: there is no top option to
// record, and a record with an empty top and p = -1 can never be given a truth (the truth must be one of the
// record's raw keys). Decide must return an error, as it does when the answer is missing altogether.
func TestDecideRefusesAChoiceAnswerWithoutAUsableDistribution(t *testing.T) {
	for _, c := range []struct {
		name, answer string
		sp           *spec.Spec
	}{
		{"no probabilities field", `{"type":"choice","choice":"a","confidence":0.9}`, threeWay()},
		{"empty probabilities", `{"type":"choice","probabilities":{}}`, threeWay()},
		{"null probabilities", `{"type":"choice","probabilities":null}`, threeWay()},
		{"all zero", `{"type":"choice","probabilities":{"a":0,"b":0,"c":0}}`, threeWay()},
		{"a negative value", `{"type":"choice","probabilities":{"a":-0.2,"b":0.9,"c":0.3}}`, threeWay()},
		{"a value above one", `{"type":"choice","probabilities":{"a":1.5,"b":0.2,"c":0.1}}`, threeWay()},
		{"a noul answer to a choice question", `{"type":"noul","noul":0.9}`, threeWay()},
		{"score: no probabilities field", `{"type":"score","score":1.5,"confidence":0.9}`, threeLevels()},
		{"score: all zero", `{"type":"score","probabilities":{"0":0,"1":0,"2":0}}`, threeLevels()},
		{"score: a negative value", `{"type":"score","probabilities":{"0":-0.2,"1":0.9,"2":0.3}}`, threeLevels()},
	} {
		d, err := Decide(context.Background(), c.sp, map[string]string{"m": "x"}, Session{}, answerServer(t, c.answer))
		if err == nil {
			t.Errorf("%s: want an error, got top=%q p=%v band=%s raw=%v", c.name, d.Top, d.P, d.Band, d.Raw)
			continue
		}
		if !strings.Contains(err.Error(), "probabilities") {
			t.Errorf("%s: the error must name what is wrong with the answer: %v", c.name, err)
		}
	}
	// The order-bias measurement reads the same answers.
	if raw, _, err := AskOrdered(context.Background(), threeWay(), "x", nil, answerServer(t, `{"type":"choice","probabilities":{}}`)[0].Client); err == nil {
		t.Errorf("AskOrdered: want an error for an empty distribution, got %v", raw)
	}
}

// With state.context, an unusable answer on the context rung is a failed call of that rung: the minimal answer
// stays, as it does when the call itself fails.
func TestDecideKeepsTheMinimalAnswerWhenTheContextAnswerIsUnusable(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"choice","probabilities":{"a":0.65,"b":0.3,"c":0.05}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"choice","probabilities":{}}}}`))
	}))
	t.Cleanup(srv.Close)
	sp := threeWay()
	sp.State.Context = "{{ m }}\nBefore: {{ previous }}"
	d, err := Decide(context.Background(), sp, map[string]string{"m": "x", "previous": "y"}, Session{}, []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || d.Top != "a" || d.P != 0.65 || d.Band != Ask || d.Rung != "minimal" || !strings.Contains(d.Reason, "context rung failed") {
		t.Fatalf("want the minimal answer with the context failure as the reason, got calls=%d %+v", calls, d)
	}
}

// Not a number cannot be written in JSON, so such a response already fails where it is read.
func TestDecideRefusesAResponseThatIsNotJSON(t *testing.T) {
	_, err := Decide(context.Background(), threeWay(), map[string]string{"m": "x"}, Session{}, answerServer(t, `{"type":"choice","probabilities":{"a":NaN,"b":0.5}}`))
	if err == nil || !strings.Contains(err.Error(), "bad response") {
		t.Fatalf("a NaN in the response must fail as a bad response: %v", err)
	}
}

// Guard: distributions that are usable stay usable. A readout that reports only its top labels gives values that
// do not sum to one, single options at zero are normal, and the order-bias measurement reads the same answers.
func TestDecideKeepsAnswersWhoseProbabilitiesDoNotSumToOne(t *testing.T) {
	for _, c := range []struct {
		answer string
		top    string
		p      float64
		band   Band
	}{
		{`{"type":"choice","probabilities":{"a":0.62,"b":0.05,"c":0.000001}}`, "a", 0.62, Ask},
		{`{"type":"choice","probabilities":{"a":0.9,"b":0.6,"c":0.3}}`, "a", 0.9, Ask}, // uncalibrated: never act
		{`{"type":"choice","probabilities":{"a":1,"b":0,"c":0}}`, "a", 1, Ask},
		{`{"type":"choice","probabilities":{"a":0.2,"b":0.5}}`, "b", 0.5, Discard}, // an option left out
	} {
		d, err := Decide(context.Background(), threeWay(), map[string]string{"m": "x"}, Session{}, answerServer(t, c.answer))
		if err != nil {
			t.Fatalf("%s: %v", c.answer, err)
		}
		if d.Top != c.top || d.P != c.p || d.Band != c.band {
			t.Fatalf("%s: got top=%q p=%v band=%s, want %q %v %s", c.answer, d.Top, d.P, d.Band, c.top, c.p, c.band)
		}
	}
	sc, err := Decide(context.Background(), threeLevels(), map[string]string{"m": "x"}, Session{}, answerServer(t, `{"type":"score","probabilities":{"0":0,"1":0.5,"2":0.5}}`))
	if err != nil || sc.Top != "1" || sc.Score != 1.5 {
		t.Fatalf("a score answer with a level at zero must stay usable: %+v %v", sc, err)
	}
	raw, _, err := AskOrdered(context.Background(), threeWay(), "x", []int{2, 1, 0}, answerServer(t, `{"type":"choice","probabilities":{"a":0.62,"b":0.05}}`)[0].Client)
	if err != nil || raw["a"] != 0.62 {
		t.Fatalf("AskOrdered must keep returning the raw distribution: %v %v", raw, err)
	}
}
