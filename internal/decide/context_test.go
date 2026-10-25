package decide

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// A context-sensitive fake model: with the body alone it is unsure (0.65). With the previous exchange (Previous turn) attached it is confident (0.9). "why" is confident from the body alone.
func contextSensitiveModel(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var states []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		states = append(states, req.State)
		mu.Unlock()
		probs := map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}
		switch {
		case strings.Contains(req.State, "Previous turn"):
			probs = map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02}
		case strings.Contains(req.State, "why"):
			probs = map[string]float64{"change_request": 0.03, "question": 0.95, "other": 0.02}
		case strings.Contains(req.State, "hmm"):
			probs = map[string]float64{"change_request": 0.4, "question": 0.35, "other": 0.25} // no option reaches ask
		}
		top, _ := argmax(probs)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), states...) }
}

func contextSpec() *spec.Spec {
	sp := &spec.Spec{
		ID: "intent", Type: spec.Choice, Question: "What is asked?",
		Options:    []spec.Option{{ID: "change_request", Description: "wants a change"}, {ID: "question", Description: "wants an answer"}, {ID: "other", Description: "none"}},
		State:      spec.State{Template: "{{ message }}", Required: []string{"message"}, Context: "Previous turn:\n{{ previous }}\n\nMessage:\n{{ message }}"},
		Policy:     spec.Policy{SendTo: []string{"local"}, PII: boolPtr(false)},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
		Eval:       spec.Eval{MinN: 2},
	}
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity"})
	return sp
}

// Only when unsure (ask band), and when the context inputs are present, add context and ask again. The first answer stays in minimal; the second is recorded under the context step's hash.
func TestContextIsAddedOnlyWhenUnsureAndAvailable(t *testing.T) {
	srv, states := contextSensitiveModel(t)
	sp := contextSpec()
	eps := []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
	in := map[string]string{"message": "fix a", "previous": "I renamed the module"}

	// the context step has no line: asks again, but the answer is uncalibrated and stays ask
	d, err := Decide(context.Background(), sp, in, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Rung != "context" || d.Minimal == nil || d.Minimal.P != 0.65 || d.Minimal.Band != string(Ask) || d.Minimal.SpecHash != sp.Hash() {
		t.Fatalf("the minimal pass must be kept on the decision: rung=%s minimal=%+v", d.Rung, d.Minimal)
	}
	if d.SpecHash != sp.ContextHash() || d.P != 0.9 || d.IsCalibrated || d.Band != Ask {
		t.Fatalf("the second pass is recorded under the context hash and is uncalibrated until it has its own line: %+v", d)
	}
	got := states()
	if len(got) != 2 || !strings.Contains(got[1], "Previous turn:\nI renamed the module") || !strings.Contains(got[1], "Message:\nfix a") {
		t.Fatalf("two calls: minimal then context: %q", got)
	}

	// the context step has a line: becomes act
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "identity"})
	d, err = Decide(context.Background(), sp, in, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Act || !d.IsCalibrated || d.Rung != "context" {
		t.Fatalf("with its own line the context rung may act: %+v", d)
	}

	// no context input: asks once, stays minimal and ask
	d, err = Decide(context.Background(), sp, map[string]string{"message": "fix a"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Rung != "minimal" || d.Minimal != nil || d.Band != Ask || d.SpecHash != sp.Hash() || len(states()) != 5 {
		t.Fatalf("without the added inputs there is nothing to add: %+v (calls=%d)", d, len(states()))
	}

	// confident from the body alone: does not ask again even with context
	d, err = Decide(context.Background(), sp, map[string]string{"message": "why a", "previous": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Rung != "minimal" || d.Band != Act || len(states()) != 6 {
		t.Fatalf("a confident minimal answer is final: %+v (calls=%d)", d, len(states()))
	}

	// a multi-choice discard (no option reaches ask) is unsure too: it moves up when there is context
	d, err = Decide(context.Background(), sp, map[string]string{"message": "hmm a", "previous": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Rung != "context" || d.Minimal == nil || d.Minimal.Band != string(Discard) || len(states()) != 8 {
		t.Fatalf("a choice that reaches no option is unsure and climbs: %+v (calls=%d)", d, len(states()))
	}
}

// A noul discard is a confident "no", so it does not move up even with context.
func TestNoulDiscardIsNotUnsure(t *testing.T) {
	f := newFake(t, "m", systemone.Answer{Type: "noul", Noul: 0.05})
	sp := noulSpec("local")
	sp.State.Context = "Context: {{ c }}\n1: {{ a }}\n2: {{ b }}"
	in := map[string]string{"a": okInputs["a"], "b": okInputs["b"], "c": "same street"}
	d, err := Decide(context.Background(), sp, in, Session{}, []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "m")}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Discard || d.Rung != "minimal" || f.calls.Load() != 1 {
		t.Fatalf("a confident no is final: %+v (calls=%d)", d, f.calls.Load())
	}
	if unsure(sp, Ask) != true || unsure(sp, Discard) != false || unsure(&spec.Spec{Type: spec.Score, Thresholds: spec.Thresholds{Level: 2}}, Discard) != false || unsure(&spec.Spec{Type: spec.Score}, Discard) != true {
		t.Fatalf("unsure: ask always; discard only where the band comes from the top option")
	}
}
