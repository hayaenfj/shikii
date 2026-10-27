package decide

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// fakeServer is a mock that answers in System One form. It records the call count and the last request.
type fakeServer struct {
	srv      *httptest.Server
	calls    atomic.Int32
	last     systemone.Request
	lastBody []byte
	answer   systemone.Answer
	model    string
	auth     string
}

func newFake(t *testing.T, model string, ans systemone.Answer) *fakeServer {
	f := &fakeServer{answer: ans, model: model}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/systemone" {
			http.Error(w, "wrong path "+r.URL.Path, 404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.lastBody = body
		if err := json.Unmarshal(body, &f.last); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		resp := systemone.Response{Model: f.model, Answers: map[string]systemone.Answer{}}
		for k := range f.last.Questions {
			resp.Answers[k] = f.answer
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func noulSpec(sendTo ...string) *spec.Spec {
	return &spec.Spec{
		ID: "n", Type: spec.Noul, Question: "Same case?",
		State:      spec.State{Template: "1: {{ a }}\n2: {{ b }}", Required: []string{"a", "b"}},
		Policy:     spec.Policy{SendTo: sendTo, PII: boolPtr(false)},
		Thresholds: spec.Thresholds{Act: 0.7, Ask: 0.5},
	}
}

var okInputs = map[string]string{"a": "arson at 12 Main St", "b": "arson at 12 Main St, follow-up"}

func TestMissingRequiredAbstainsWithoutCalling(t *testing.T) {
	f := newFake(t, "jev-1.13.0", systemone.Answer{Type: "noul", Noul: 0.99})
	eps := []Endpoint{{Kind: "hosted", Client: systemone.New(f.srv.URL, "k", "jev-latest")}}
	d, err := Decide(context.Background(), noulSpec("hosted"), map[string]string{"a": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Abstain || d.Reason == "" {
		t.Fatalf("want abstain with reason, got %+v", d)
	}
	if f.calls.Load() != 0 {
		t.Fatalf("model must not be called when required input is missing; calls=%d", f.calls.Load())
	}
}

func TestTaintedSessionNeverReachesHosted(t *testing.T) {
	hosted := newFake(t, "jev-1.13.0", systemone.Answer{Type: "noul", Noul: 0.99})
	local := newFake(t, "kev-4b", systemone.Answer{Type: "noul", Noul: 0.60})
	eps := []Endpoint{
		{Kind: "hosted", Client: systemone.New(hosted.srv.URL, "k", "jev-latest")},
		{Kind: "local", Client: systemone.New(local.srv.URL, "", "kev-4b")},
	}

	// spec that allows only hosted: tainted gives abstain, and hosted is not called
	d, err := Decide(context.Background(), noulSpec("hosted"), okInputs, Session{Tainted: true}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Abstain {
		t.Fatalf("tainted + hosted-only must abstain, got band=%s", d.Band)
	}
	if hosted.calls.Load() != 0 {
		t.Fatalf("hosted endpoint was called from a tainted session (calls=%d)", hosted.calls.Load())
	}

	// spec that prefers hosted but also allows local: tainted falls back to local
	d, err = Decide(context.Background(), noulSpec("hosted", "local"), okInputs, Session{Tainted: true}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Endpoint != "local" || d.Model != "kev-4b" {
		t.Fatalf("tainted must fall back to local, got endpoint=%s model=%s", d.Endpoint, d.Model)
	}
	if hosted.calls.Load() != 0 || local.calls.Load() != 1 {
		t.Fatalf("calls hosted=%d local=%d", hosted.calls.Load(), local.calls.Load())
	}

	// not tainted: goes to hosted
	d, err = Decide(context.Background(), noulSpec("hosted", "local"), okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Endpoint != "hosted" || hosted.calls.Load() != 1 || hosted.auth != "Bearer k" {
		t.Fatalf("clean session must use hosted with bearer auth: endpoint=%s calls=%d auth=%q", d.Endpoint, hosted.calls.Load(), hosted.auth)
	}
}

// A spec with pii: true (may hold personal information) never goes to hosted, even if send_to lists hosted first. With only hosted, it abstains.
func TestPIITrueNeverReachesHosted(t *testing.T) {
	hosted := newFake(t, "jev-1.13.0", systemone.Answer{Type: "noul", Noul: 0.99})
	local := newFake(t, "kev-4b", systemone.Answer{Type: "noul", Noul: 0.60})
	eps := []Endpoint{
		{Kind: "hosted", Client: systemone.New(hosted.srv.URL, "k", "jev-latest")},
		{Kind: "local", Client: systemone.New(local.srv.URL, "", "kev-4b")},
	}
	yes := true
	sp := noulSpec("hosted", "local")
	sp.Policy.PII = &yes
	d, err := Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Endpoint != "local" || hosted.calls.Load() != 0 {
		t.Fatalf("pii=true must never use hosted: endpoint=%s hosted calls=%d", d.Endpoint, hosted.calls.Load())
	}
	onlyHosted := noulSpec("hosted")
	onlyHosted.Policy.PII = &yes
	d, err = Decide(context.Background(), onlyHosted, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Abstain || hosted.calls.Load() != 0 {
		t.Fatalf("pii=true with hosted-only spec must abstain, got band=%s hosted calls=%d", d.Band, hosted.calls.Load())
	}
}

func TestUncalibratedNeverActs(t *testing.T) {
	f := newFake(t, "jev-1.13.0", systemone.Answer{Type: "noul", Noul: 0.99})
	eps := []Endpoint{{Kind: "hosted", Client: systemone.New(f.srv.URL, "k", "jev-latest")}}
	sp := noulSpec("hosted")

	d, err := Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.IsCalibrated || d.Band != Ask {
		t.Fatalf("p=0.99 without calibration must be capped at ask, got band=%s calibrated=%v", d.Band, d.IsCalibrated)
	}

	// adding an identity calibration (bias 0, T 1) gives act
	sp.Calibration = []spec.Calibration{{Model: "jev-1.13.0", SpecHash: sp.Hash(), Method: "platt", Bias: 0, Temperature: 1}}
	d, err = Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsCalibrated || d.Band != Act {
		t.Fatalf("calibrated p=0.99 must act, got band=%s calibrated=%v", d.Band, d.IsCalibrated)
	}

	// changing one word of the question expires the calibration and act disappears
	sp.Question += " Really?"
	d, err = Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.IsCalibrated || d.Band == Act {
		t.Fatalf("editing the question must invalidate calibration and drop act, got band=%s calibrated=%v", d.Band, d.IsCalibrated)
	}

	// it also expires when the model changes version
	sp.Question = noulSpec("hosted").Question
	f.model = "jev-1.14.0"
	d, err = Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.IsCalibrated || d.Band == Act {
		t.Fatalf("a new model version must invalidate calibration, got band=%s calibrated=%v model=%s", d.Band, d.IsCalibrated, d.Model)
	}
}

func TestCalibrationChangesTheBand(t *testing.T) {
	// like had-scorer: raw 0.877 (Δ≈1.96, the upper bound for unrelated pairs) is ask or higher uncalibrated, but
	// after calibration with bias 2.2 / T 0.3 it is σ((1.96−2.2)/0.3)≈0.31, so discard.
	f := newFake(t, "m", systemone.Answer{Type: "noul", Noul: 0.877})
	eps := []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "m")}}
	sp := noulSpec("local")
	sp.Calibration = []spec.Calibration{{Model: "m", SpecHash: sp.Hash(), Method: "platt", Bias: 2.2, Temperature: 0.3}}
	d, err := Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Discard || d.P > 0.35 || d.Raw["yes"] != 0.877 {
		t.Fatalf("want discard with p≈0.31 and raw kept, got band=%s p=%.3f raw=%v", d.Band, d.P, d.Raw)
	}
}

func TestChoiceAndScoreRequestShapes(t *testing.T) {
	f := newFake(t, "m", systemone.Answer{Type: "choice", Choice: "question", Confidence: 0.6,
		Probabilities: map[string]float64{"question": 0.7, "change_request": 0.2, "other": 0.1}})
	eps := []Endpoint{{Kind: "hosted", Client: systemone.New(f.srv.URL, "k", "m")}}
	ch := &spec.Spec{
		ID: "c", Type: spec.Choice, Question: "What is asked?",
		Options: []spec.Option{
			{ID: "change_request", Description: "wants a change"},
			{ID: "question", Description: "wants an answer"},
			{ID: "other", Description: "none"},
		},
		State:      spec.State{Template: "{{ m }}", Required: []string{"m"}},
		Policy:     spec.Policy{SendTo: []string{"hosted"}},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
	}
	d, err := Decide(context.Background(), ch, map[string]string{"m": "why?"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	q := f.last.Questions["q"]
	crit, ok := q.Criteria.(map[string]any)
	if q.Type != "choice" || !ok || crit["question"] != "wants an answer" || len(crit) != 3 {
		t.Fatalf("choice criteria must be an id→description map, got %+v", q)
	}
	// order follows the spec (not alphabetical)
	var got struct {
		Questions map[string]struct {
			Criteria systemone.OrderedCriteria `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(f.lastBody, &got); err != nil {
		t.Fatal(err)
	}
	oc := got.Questions["q"].Criteria
	if len(oc) != 3 || oc[0].ID != "change_request" || oc[1].ID != "question" || oc[2].ID != "other" {
		t.Fatalf("choice criteria must keep the spec order on the wire, got %+v", oc)
	}
	if d.Top != "question" || d.P != 0.7 || d.Band != Ask || d.Confidence != 0.6 {
		t.Fatalf("choice decision: %+v", d)
	}

	f.answer = systemone.Answer{Type: "score", Score: 1.5, Confidence: 0.4,
		Probabilities: map[string]float64{"0": 0.0, "1": 0.5, "2": 0.5}}
	sc := &spec.Spec{
		ID: "s", Type: spec.Score, Question: "How related?",
		Levels:     []string{"unrelated", "related", "same case"},
		State:      spec.State{Template: "{{ m }}", Required: []string{"m"}},
		Policy:     spec.Policy{SendTo: []string{"hosted"}},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
	}
	d, err = Decide(context.Background(), sc, map[string]string{"m": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	q = f.last.Questions["q"]
	lv, ok := q.Criteria.([]any)
	if q.Type != "score" || !ok || len(lv) != 3 || lv[2] != "same case" {
		t.Fatalf("score criteria must be the ordered level list, got %+v", q)
	}
	if d.Score != 1.5 || d.Top != "1" || d.Band != Discard {
		t.Fatalf("score decision: expected value 1.5, deterministic tie → lowest key, discard; got %+v", d)
	}
}

// identity calibration is the "verified" mark. It has no constants but allows act. It has no effect unless the key (hash, model) matches.
func TestIdentityCalibrationAllowsAct(t *testing.T) {
	f := newFake(t, "jev-1.13.0", systemone.Answer{Type: "choice", Choice: "a", Confidence: 1,
		Probabilities: map[string]float64{"a": 1, "b": 0}})
	eps := []Endpoint{{Kind: "hosted", Client: systemone.New(f.srv.URL, "k", "jev-latest")}}
	sp := &spec.Spec{
		ID: "c", Type: spec.Choice, Question: "which?",
		Options:    []spec.Option{{ID: "a", Description: "A"}, {ID: "b", Description: "B"}},
		State:      spec.State{Template: "{{ m }}", Required: []string{"m"}},
		Policy:     spec.Policy{SendTo: []string{"hosted"}},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
	}
	d, err := Decide(context.Background(), sp, map[string]string{"m": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Ask || d.IsCalibrated || d.TraceID == "" || d.InputsHash == "" {
		t.Fatalf("unverified choice must be ask and carry provenance: %+v", d)
	}
	sp.Calibration = []spec.Calibration{{Model: "jev-1.13.0", SpecHash: sp.Hash(), Method: "identity", PrecisionAt: 0.9}}
	d, err = Decide(context.Background(), sp, map[string]string{"m": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Act || !d.IsCalibrated || d.Calibrated["a"] != 1 {
		t.Fatalf("identity-verified choice must act with raw unchanged: %+v", d)
	}
	sp.Calibration[0].Model = "jev-1.14.0"
	d, _ = Decide(context.Background(), sp, map[string]string{"m": "x"}, Session{}, eps)
	if d.Band == Act {
		t.Fatal("identity for another model must not allow act")
	}
}

// vector calibration rebuilds the choice distribution. A negative bias pushes back against "escaping to other".
func TestVectorCalibrationReshapesChoice(t *testing.T) {
	f := newFake(t, "kev", systemone.Answer{Type: "choice", Choice: "other", Confidence: 0.3,
		Probabilities: map[string]float64{"change_request": 0.40, "other": 0.50, "question": 0.10}})
	eps := []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "kev")}}
	sp := &spec.Spec{
		ID: "c", Type: spec.Choice, Question: "which?",
		Options:    []spec.Option{{ID: "change_request", Description: "A"}, {ID: "other", Description: "B"}, {ID: "question", Description: "C"}},
		State:      spec.State{Template: "{{ m }}", Required: []string{"m"}},
		Policy:     spec.Policy{SendTo: []string{"local"}},
		Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6},
	}
	sp.Calibration = []spec.Calibration{{Model: "kev", SpecHash: sp.Hash(), Method: "vector", Temperature: 0.5,
		Biases: map[string]float64{"other": -1.0, "change_request": 0.5, "question": 0.5}}}
	d, err := Decide(context.Background(), sp, map[string]string{"m": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsCalibrated || d.Top != "change_request" || d.Band != Act {
		t.Fatalf("vector calibration must lift change_request above other and act: %+v", d)
	}
	if d.Raw["other"] != 0.50 {
		t.Fatal("raw distribution must be kept untouched")
	}
	// vector has no effect on noul (a calibration of the wrong type is ignored, and no act appears)
	n := noulSpec("local")
	n.Calibration = []spec.Calibration{{Model: "kev", SpecHash: n.Hash(), Method: "vector", Temperature: 0.5}}
	f.answer = systemone.Answer{Type: "noul", Noul: 0.99}
	d, _ = Decide(context.Background(), n, okInputs, Session{}, eps)
	if d.IsCalibrated || d.Band == Act {
		t.Fatalf("vector on noul must be ignored: %+v", d)
	}
}

// With thresholds.level, the score band is P(level ≥ k). Looking only at the top level makes it look weak because probability splits across neighbouring levels.
func TestScoreBandUsesMassAtOrAboveLevel(t *testing.T) {
	// like a same-issue pair: 2:0.45 / 3:0.35 / 4:0.20. The top is 0.45 (< ask) but P(level≥3)=0.55 (≥ ask)
	f := newFake(t, "m", systemone.Answer{Type: "score", Score: 2.75, Confidence: 0.3,
		Probabilities: map[string]float64{"0": 0, "1": 0, "2": 0.45, "3": 0.35, "4": 0.20}})
	eps := []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "m")}}
	mk := func(level int) *spec.Spec {
		return &spec.Spec{
			ID: "s", Type: spec.Score, Question: "how related?",
			Levels:     []string{"unrelated", "same domain", "circumstantial", "directly connected", "same case"},
			State:      spec.State{Template: "{{ m }}", Required: []string{"m"}},
			Policy:     spec.Policy{SendTo: []string{"local"}},
			Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.5, Level: level},
		}
	}
	d, err := Decide(context.Background(), mk(0), map[string]string{"m": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Discard || d.P != 0.45 {
		t.Fatalf("without level the band follows the top probability: %+v", d)
	}
	d, err = Decide(context.Background(), mk(3), map[string]string{"m": "x"}, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Band != Ask || abs(d.P-0.55) > 1e-9 || d.Top != "2" || abs(d.Score-2.75) > 1e-9 {
		t.Fatalf("with level=3 the band must use P(level>=3)=0.55 while top/score stay: %+v", d)
	}
	if m := MassAtOrAbove(map[string]float64{"0": 0.1, "1": 0.2, "2": 0.7}, 1); abs(m-0.9) > 1e-9 {
		t.Fatalf("MassAtOrAbove: %f", m)
	}
}

func boolPtr(b bool) *bool { return &b }

// audit 9: even if the context step's call fails, the minimal answer (ask) is not thrown away.
func TestContextCallFailureKeepsTheMinimalAnswer(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if atomic.AddInt32(&calls, 1) == 2 || strings.Contains(req.State, "Previous turn") {
			http.Error(w, "boom", 500)
			return
		}
		probs := map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	sp := contextSpec()
	d, err := Decide(context.Background(), sp, map[string]string{"message": "fix a", "previous": "x"}, Session{}, []Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}})
	if err != nil || d == nil || d.Rung != "minimal" || d.Band != Ask || d.Minimal != nil || d.SpecHash != sp.Hash() || d.P != 0.65 {
		t.Fatalf("the minimal answer must survive a failed context call: err=%v d=%+v", err, d)
	}
	if calls != 2 {
		t.Fatalf("the context rung was tried once: %d", calls)
	}
}

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

// The spec's layout goes into the System One request (the converter changes the arrangement; decision models ignore it as an unknown field).
func TestLayoutTravelsInTheRequest(t *testing.T) {
	f := newFake(t, "m", systemone.Answer{Type: "noul", Noul: 0.9})
	sp := noulSpec("local")
	eps := []Endpoint{{Kind: "local", Client: systemone.New(f.srv.URL, "", "m")}}
	if _, err := Decide(context.Background(), sp, okInputs, Session{}, eps); err != nil {
		t.Fatal(err)
	}
	if f.last.Layout != "" {
		t.Fatalf("no layout in the spec, none in the request: %q", f.last.Layout)
	}
	sp.Layout = "plain"
	if _, err := Decide(context.Background(), sp, okInputs, Session{}, eps); err != nil {
		t.Fatal(err)
	}
	if f.last.Layout != "plain" {
		t.Fatalf("the spec's layout must reach the model: %q", f.last.Layout)
	}
}
