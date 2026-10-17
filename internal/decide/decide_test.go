package decide

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// fakeServer は System One 形式を返すモック。呼ばれた回数と最後のリクエストを記録する。
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

	// hosted しか許されていない spec: tainted なら abstain、hosted は呼ばれない
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

	// hosted 優先だが local も許される spec: tainted なら local に落ちる
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

	// 汚れていなければ hosted に行く
	d, err = Decide(context.Background(), noulSpec("hosted", "local"), okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.Endpoint != "hosted" || hosted.calls.Load() != 1 || hosted.auth != "Bearer k" {
		t.Fatalf("clean session must use hosted with bearer auth: endpoint=%s calls=%d auth=%q", d.Endpoint, hosted.calls.Load(), hosted.auth)
	}
}

// pii: true(個人情報が入り得る)の spec は、send_to が hosted を先に挙げていても hosted に行かない。hosted しか無ければ abstain。
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

	// 恒等の較正(bias 0, T 1)を付けると act になる
	sp.Calibration = []spec.Calibration{{Model: "jev-1.13.0", SpecHash: sp.Hash(), Method: "platt", Bias: 0, Temperature: 1}}
	d, err = Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsCalibrated || d.Band != Act {
		t.Fatalf("calibrated p=0.99 must act, got band=%s calibrated=%v", d.Band, d.IsCalibrated)
	}

	// 質問文を 1 語変えると較正は失効して act が消える
	sp.Question += " Really?"
	d, err = Decide(context.Background(), sp, okInputs, Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	if d.IsCalibrated || d.Band == Act {
		t.Fatalf("editing the question must invalidate calibration and drop act, got band=%s calibrated=%v", d.Band, d.IsCalibrated)
	}

	// モデルが別版になっても失効する
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
	// had-scorer 相当: raw 0.877(Δ≈1.96, 無関係ペアの上限)は生では ask 以上だが、
	// bias 2.2 / T 0.3 の較正後は σ((1.96−2.2)/0.3)≈0.31 で discard。
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
	// 並びは spec の順(アルファベット順ではない)
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

// identity 較正は「検証済み」の印。定数は無いが act を許す。鍵(hash, model)が合わなければ効かない。
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

// vector 較正は choice の分布を作り直す。負の bias で「other に逃げる」を押し戻す。
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
	// noul には vector は効かない(型が合わない較正は無視され、act も出ない)
	n := noulSpec("local")
	n.Calibration = []spec.Calibration{{Model: "kev", SpecHash: n.Hash(), Method: "vector", Temperature: 0.5}}
	f.answer = systemone.Answer{Type: "noul", Noul: 0.99}
	d, _ = Decide(context.Background(), n, okInputs, Session{}, eps)
	if d.IsCalibrated || d.Band == Act {
		t.Fatalf("vector on noul must be ignored: %+v", d)
	}
}

// score の帯は thresholds.level があれば P(level ≥ k)。最上位だけ見ると隣接レベルに割れて弱く見える。
func TestScoreBandUsesMassAtOrAboveLevel(t *testing.T) {
	// 同一の不具合の組に相当: 2:0.45 / 3:0.35 / 4:0.20。最上位は 0.45 (< ask) だが P(level≥3)=0.55 (≥ ask)
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
