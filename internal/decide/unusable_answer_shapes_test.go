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

func answering(t *testing.T, body string) []Endpoint {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
}

// A choice broken down into yes/no steps is a choice: a step's value outside 0..1 is not a probability, and the product
// distribution built from it (top a at 1.7, or b at 0.7 next to a at -0.4) must not become a decision.
func TestASteppedChoiceRefusesAStepValueOutsideZeroToOne(t *testing.T) {
	sp := threeWay()
	sp.Steps = []spec.Step{{Ask: "is it a?", Yes: "a"}, {Ask: "is it b?", Yes: "b", No: "c"}}
	for _, body := range []string{
		`{"model":"m","answers":{"step-0":{"type":"noul","noul":1.7},"step-1":{"type":"noul","noul":0.5}}}`,
		`{"model":"m","answers":{"step-0":{"type":"noul","noul":-0.4},"step-1":{"type":"noul","noul":0.5}}}`,
		`{"model":"m","answers":{"step-0":{"type":"noul","noul":0.5},"step-1":{"type":"noul","noul":1.0000001}}}`,
	} {
		d, err := Decide(context.Background(), sp, map[string]string{"m": "x"}, Session{}, answering(t, body))
		if err == nil || !strings.Contains(err.Error(), "not a probability") {
			t.Errorf("a stepped choice took %s as a decision: %v %+v", body, err, d)
		}
	}
	// the ends of the range are answers
	d, err := Decide(context.Background(), sp, map[string]string{"m": "x"}, Session{},
		answering(t, `{"model":"m","answers":{"step-0":{"type":"noul","noul":0},"step-1":{"type":"noul","noul":1}}}`))
	if err != nil || d.Top != "b" || d.P != 1 {
		t.Errorf("steps answered 0 and 1 decide b at 1: %v %+v", err, d)
	}
}

// An answer that gives its probability to an option the question does not have has no top option a truth could be given to.
// An unknown key at zero is not an answer for it and changes nothing.
func TestAChoiceRefusesAProbabilityForAnOptionTheQuestionDoesNotHave(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","answers":{"q":{"type":"choice","probabilities":{"zzz":0.9}}}}`,
		`{"model":"m","answers":{"q":{"type":"choice","probabilities":{"a":0.3,"zzz":0.7}}}}`,
	} {
		d, err := Decide(context.Background(), threeWay(), map[string]string{"m": "x"}, Session{}, answering(t, body))
		if err == nil || !strings.Contains(err.Error(), `"zzz" is not an option of this question`) {
			t.Errorf("%s became a decision: %v %+v", body, err, d)
		}
	}
	d, err := Decide(context.Background(), threeWay(), map[string]string{"m": "x"}, Session{},
		answering(t, `{"model":"m","answers":{"q":{"type":"choice","probabilities":{"a":0.7,"b":0.2,"c":0.1,"zzz":0}}}}`))
	if err != nil || d.Top != "a" {
		t.Errorf("an unknown key at zero changes nothing: %v %+v", err, d)
	}
}
