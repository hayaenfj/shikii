package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeUpstream pretends to be llama-server: /tokenize, /apply-template, /completion, /v1/models.
type fakeUpstream struct {
	mu            sync.Mutex
	rejectKwargs  bool            // rejects chat_template_kwargs with 400 (mimics an old upstream version)
	multiToken    map[string]bool // answers that this label is 2 tokens
	top           []tokenProb     // top list returned by /completion
	lastTemplate  map[string]any  // body of the last /apply-template
	lastPrompt    string          // prompt of the last /completion
	templateCalls int
	completions   []fakeCompletion // history of /completion (including warm-ups)
	cacheN        int              // value /completion returns as timings.cache_n
	decision      bool             // pretends to be a trained decision model (answers /v1/systemone itself, like Kev)
	rejectProbe   int              // if non-zero, pretends to be a decision model that rejects the probe (state=probe) with this status
	systemOneHits int              // number of times /v1/systemone was called
}

type fakeCompletion struct {
	Prompt   string
	NPredict int
}

func (f *fakeUpstream) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"fake-gguf"}]}`))
	})
	mux.HandleFunc("/v1/systemone", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.systemOneHits++
		decision := f.decision
		f.mu.Unlock()
		if !decision { // upstream llama.cpp returns 501 for an ordinary GGUF
			http.Error(w, `{"error":"not a decision model"}`, 501)
			return
		}
		if f.rejectProbe != 0 {
			var in struct {
				State string `json:"state"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.State == "probe" {
				http.Error(w, `{"error":"state too short"}`, f.rejectProbe)
				return
			}
		}
		_, _ = w.Write([]byte(`{"model":"fake-decision","answers":{"q":{"type":"noul","noul":0.91}},"usage":{"input_tokens":12}}`))
	})
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content    string `json:"content"`
			WithPieces bool   `json:"with_pieces"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		multi := f.multiToken[in.Content]
		f.mu.Unlock()
		if in.WithPieces { // a fake tokenizer whose first token is the first 2 characters (or all of it if shorter)
			piece := in.Content
			if len(piece) > 2 {
				piece = piece[:2]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": []map[string]any{{"id": 7, "piece": piece}}})
			return
		}
		if multi {
			_, _ = w.Write([]byte(`{"tokens":[220,16]}`))
			return
		}
		_, _ = w.Write([]byte(`{"tokens":[32]}`))
	})
	mux.HandleFunc("/apply-template", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.templateCalls++
		f.lastTemplate = in
		reject := f.rejectKwargs && in["chat_template_kwargs"] != nil
		f.mu.Unlock()
		if reject {
			http.Error(w, `{"error":"unknown field chat_template_kwargs"}`, 400)
			return
		}
		var b strings.Builder
		for _, m := range in["messages"].([]any) {
			mm := m.(map[string]any)
			b.WriteString("<|" + mm["role"].(string) + "|>" + mm["content"].(string) + "\n")
		}
		b.WriteString("<|assistant|>\n")
		_ = json.NewEncoder(w).Encode(map[string]string{"prompt": b.String()})
	})
	mux.HandleFunc("/completion", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		prompt, _ := in["prompt"].(string)
		np, _ := in["n_predict"].(float64)
		f.completions = append(f.completions, fakeCompletion{Prompt: prompt, NPredict: int(np)})
		if np > 0 {
			f.lastPrompt = prompt
		}
		top := f.top
		cacheN := f.cacheN
		f.mu.Unlock()
		if np == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{"content": "", "timings": map[string]any{"prompt_n": 100, "cache_n": 0}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content":                  top[0].Token,
			"completion_probabilities": []map[string]any{{"token": top[0].Token, "prob": top[0].Prob, "top_probs": top}},
			"timings":                  map[string]any{"prompt_n": 42, "cache_n": cacheN},
		})
	})
	return mux
}

func newTestServer(t *testing.T, f *fakeUpstream) (*server, *httptest.Server) {
	t.Helper()
	up := httptest.NewServer(f.handler())
	t.Cleanup(up.Close)
	s := &server{
		cfg:           config{Upstream: up.URL, Model: "fake-gguf", NProbs: 8, System: defaultSystem, Timeout: 10 * time.Second},
		base:          "fake-gguf",
		http:          up.Client(),
		thinkingKwarg: true,
	}
	return s, up
}

func ask(t *testing.T, s *server, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSystemOne(rec, httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader([]byte(body))))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Options get A, B, C in their JSON order. Putting them in a Go map would sort the keys and shift the positions.
func TestChoiceKeepsCriteriaOrderAndMapsLetters(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"B", 0.7}, {"A", 0.2}, {"C", 0.05}, {"The", 0.05}}}
	s, _ := newTestServer(t, f)
	// the keys are not in alphabetical order: zeta, alpha, mid
	code, out := ask(t, s, `{"state":"fix the typo","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"zeta":"Z desc","alpha":"A desc","mid":"M desc"}}}}`)
	if code != 200 {
		t.Fatalf("code %d: %v", code, out)
	}
	if !strings.Contains(f.lastPrompt, "A. zeta: Z desc\nB. alpha: A desc\nC. mid: M desc") {
		t.Fatalf("options must be lettered in request order, prompt was:\n%s", f.lastPrompt)
	}
	ans := out["answers"].(map[string]any)["q"].(map[string]any)
	if ans["choice"] != "alpha" {
		t.Fatalf("B=0.7 must map to the second option (alpha), got %v", ans["choice"])
	}
	probs := ans["probabilities"].(map[string]any)
	// normalization uses only the labels' probabilities: 0.2/0.95, 0.7/0.95, 0.05/0.95
	if math.Abs(probs["alpha"].(float64)-0.7/0.95) > 1e-9 || math.Abs(probs["zeta"].(float64)-0.2/0.95) > 1e-9 {
		t.Fatalf("probabilities must be renormalised over labels only: %v", probs)
	}
	if out["model"] != "fake-gguf" {
		t.Fatalf("model must be the configured name, got %v", out["model"])
	}
}

// A label that missed the top list gets the floor probability, and the total stays 1. With 0, calibration's log would break.
func TestMissingLabelGetsFloorAndSumIsOne(t *testing.T) {
	probs, missing := readProbs([]tokenProb{{"A", 0.9}, {"xyz", 0.1}}, 3)
	if missing != 2 {
		t.Fatalf("B and C are missing, got %d", missing)
	}
	var sum float64
	for _, p := range probs {
		if p <= 0 {
			t.Fatalf("no label may have probability 0: %v", probs)
		}
		sum += p
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("sum must be 1, got %f", sum)
	}
	if probs[0] < 0.999 {
		t.Fatalf("A must keep almost all the mass: %v", probs)
	}
}

// noul is P(yes). yes comes first (A).
func TestNoulReturnsProbabilityOfYes(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"B", 0.75}, {"A", 0.25}}}
	s, _ := newTestServer(t, f)
	code, out := ask(t, s, `{"state":"two reports of the same robbery","questions":{"same":{"type":"noul","instructions":"the two reports describe the same case"}}}`)
	if code != 200 {
		t.Fatalf("code %d: %v", code, out)
	}
	ans := out["answers"].(map[string]any)["same"].(map[string]any)
	if math.Abs(ans["noul"].(float64)-0.25) > 1e-9 {
		t.Fatalf("noul must be P(A=yes)=0.25, got %v", ans["noul"])
	}
	if !strings.Contains(f.lastPrompt, "A. yes\nB. no") {
		t.Fatalf("noul must present yes then no:\n%s", f.lastPrompt)
	}
	// use true/false descriptions in criteria if present. Other keys get 422
	code, _ = ask(t, s, `{"state":"x","questions":{"same":{"type":"noul","instructions":"q","criteria":{"maybe":"?"}}}}`)
	if code != 422 {
		t.Fatalf("unknown noul criteria key must be 422, got %d", code)
	}
}

// score has the same shape as llama.cpp's decision models: probabilities are "0".."n-1", plus legend, and score is the expected value.
func TestScoreShapeMatchesDecisionModels(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"C", 0.6}, {"B", 0.3}, {"A", 0.1}}}
	s, _ := newTestServer(t, f)
	code, out := ask(t, s, `{"state":"x","questions":{"rel":{"type":"score","instructions":"how related?","criteria":["unrelated","same domain","same case"]}}}`)
	if code != 200 {
		t.Fatalf("code %d: %v", code, out)
	}
	ans := out["answers"].(map[string]any)["rel"].(map[string]any)
	legend := ans["legend"].(map[string]any)
	if legend["2"] != "same case" || legend["0"] != "unrelated" {
		t.Fatalf("legend must be keyed by level index: %v", legend)
	}
	if math.Abs(ans["score"].(float64)-(0*0.1+1*0.3+2*0.6)) > 1e-9 {
		t.Fatalf("score must be the expected level, got %v", ans["score"])
	}
	if !strings.Contains(f.lastPrompt, "Scale (lowest first):\nA. unrelated\nB. same domain\nC. same case") {
		t.Fatalf("scale must be presented lowest first:\n%s", f.lastPrompt)
	}
}

// Send enable_thinking=false. For an upstream that rejects it, resend without it and stop sending it.
func TestThinkingKwargWithFallback(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"A", 1}}}
	s, _ := newTestServer(t, f)
	ask(t, s, `{"state":"x","questions":{"q":{"type":"choice","instructions":"q","criteria":{"a":"","b":""}}}}`)
	if f.lastTemplate["chat_template_kwargs"] == nil {
		t.Fatal("enable_thinking=false must be sent to a willing upstream")
	}
	if !strings.Contains(f.lastPrompt, "<|assistant|>") {
		t.Fatalf("prompt must come from /apply-template:\n%s", f.lastPrompt)
	}

	f2 := &fakeUpstream{top: []tokenProb{{"A", 1}}, rejectKwargs: true}
	s2, _ := newTestServer(t, f2)
	code, out := ask(t, s2, `{"state":"x","questions":{"q":{"type":"choice","instructions":"q","criteria":{"a":"","b":""}}}}`)
	if code != 200 {
		t.Fatalf("rejecting upstream must be retried without kwargs, got %d %v", code, out)
	}
	if f2.templateCalls != 2 || f2.lastTemplate["chat_template_kwargs"] != nil {
		t.Fatalf("expected one rejected call then one without kwargs, calls=%d last=%v", f2.templateCalls, f2.lastTemplate)
	}
	ask(t, s2, `{"state":"y","questions":{"q":{"type":"choice","instructions":"q","criteria":{"a":"","b":""}}}}`)
	if f2.templateCalls != 3 {
		t.Fatalf("after a rejection the kwarg must not be retried every time, calls=%d", f2.templateCalls)
	}
}

// Label check at startup: refuse to start if any label is not a single token.
func TestLabelCheckRejectsMultiTokenLabels(t *testing.T) {
	ok := &fakeUpstream{}
	s, _ := newTestServer(t, ok)
	if err := s.checkLabels(context.Background()); err != nil {
		t.Fatalf("all single tokens must pass: %v", err)
	}
	bad := &fakeUpstream{multiToken: map[string]bool{"Q": true}}
	s2, _ := newTestServer(t, bad)
	err := s2.checkLabels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Q=2") {
		t.Fatalf("a 2-token label must be reported, got %v", err)
	}
}

// Request validation: type, instructions, number of options.
func TestRequestValidation(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"A", 1}}}
	s, _ := newTestServer(t, f)
	cases := map[string]string{
		"unknown type":    `{"state":"x","questions":{"q":{"type":"rank","instructions":"q","criteria":{"a":"","b":""}}}}`,
		"no instructions": `{"state":"x","questions":{"q":{"type":"choice","criteria":{"a":"","b":""}}}}`,
		"one option":      `{"state":"x","questions":{"q":{"type":"choice","instructions":"q","criteria":{"a":""}}}}`,
		"array criteria":  `{"state":"x","questions":{"q":{"type":"choice","instructions":"q","criteria":["a","b"]}}}`,
		"no questions":    `{"state":"x"}`,
	}
	for name, body := range cases {
		if code, _ := ask(t, s, body); code != 422 {
			t.Errorf("%s: want 422, got %d", name, code)
		}
	}
	// 27 choices are too many (there are 26 labels)
	var crit strings.Builder
	crit.WriteByte('{')
	for i := 0; i < 27; i++ {
		if i > 0 {
			crit.WriteByte(',')
		}
		crit.WriteString(`"o` + string(rune('a'+i)) + `":""`)
	}
	crit.WriteByte('}')
	if code, _ := ask(t, s, `{"state":"x","questions":{"q":{"type":"choice","instructions":"q","criteria":`+crit.String()+`}}}`); code != 422 {
		t.Errorf("27 options: want 422, got %d", code)
	}
	// an object state is inserted formatted
	code, _ := ask(t, s, `{"state":{"b":1,"a":"x"},"questions":{"q":{"type":"choice","instructions":"q","criteria":{"a":"","b":""}}}}`)
	if code != 200 || !strings.Contains(f.lastPrompt, "<state>\n{\n  \"b\": 1,\n  \"a\": \"x\"\n}\n</state>") {
		t.Fatalf("object state must be pretty-printed in request key order:\n%s", f.lastPrompt)
	}
}

// The default layout puts the state last (the system message, question and options form a shared prefix, so the prefix cache helps).
// The layout is added to the response's model: a different order changes how probabilities come out, so the calibration key should change too.
func TestStateLastLayoutAndModelSuffix(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"A", 1}}}
	s, _ := newTestServer(t, f)
	ask(t, s, `{"state":"THE-STATE","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A desc","b":"B desc"}}}}`)
	opt, st := strings.Index(f.lastPrompt, "A. a: A desc"), strings.Index(f.lastPrompt, "<state>\nTHE-STATE\n</state>")
	if opt < 0 || st < 0 || !(opt < st) {
		t.Fatalf("default layout must put options before the state (opt=%d state=%d):\n%s", opt, st, f.lastPrompt)
	}
	if !strings.HasSuffix(strings.TrimSpace(f.lastPrompt), "Reply with one letter.\n<|assistant|>") {
		t.Fatalf("the instruction must stay last so the answer token follows it:\n%s", f.lastPrompt)
	}

	s.cfg.StateFirst = true
	ask(t, s, `{"state":"THE-STATE","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A desc","b":"B desc"}}}}`)
	opt, st = strings.Index(f.lastPrompt, "A. a: A desc"), strings.Index(f.lastPrompt, "<state>\nTHE-STATE\n</state>")
	if !(st < opt) {
		t.Fatalf("--state-first must put the state before the options:\n%s", f.lastPrompt)
	}

	if got := modelName("bonsai-2", config{}); got != "bonsai-2+state-last" {
		t.Fatalf("model must carry the layout, got %q", got)
	}
	if got := modelName("bonsai-2", config{StateFirst: true}); got != "bonsai-2+state-first" {
		t.Fatalf("model must carry the layout, got %q", got)
	}
	if got := modelName("bonsai-2", config{Model: "pinned"}); got != "pinned" {
		t.Fatalf("--model must win, got %q", got)
	}
}

// Prefix warm-up: for a new prefix, send everything up to the state first with n_predict=0. Not for the second question with the same prefix.
// If the response's cache_n falls short of the prefix, send it again on the next question (when the checkpoint is gone).
func TestPrefixWarmUpOncePerPrefixAndAfterCacheMiss(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"A", 1}}, cacheN: 100}
	s, _ := newTestServer(t, f)
	q := `{"state":"%s","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A desc","b":"B desc"}}}}`
	ask(t, s, fmt.Sprintf(q, "first"))
	if len(f.completions) != 2 || f.completions[0].NPredict != 0 {
		t.Fatalf("first question must be preceded by a warm-up (n_predict=0), got %+v", f.completions)
	}
	warm, full := f.completions[0].Prompt, f.completions[1].Prompt
	if strings.Contains(warm, "<state>") || !strings.HasPrefix(full, warm) || !strings.HasPrefix(full[len(warm):], "<state>") {
		t.Fatalf("warm-up prompt must be exactly the part before <state>:\nwarm=%q\nfull=%q", warm, full)
	}
	ask(t, s, fmt.Sprintf(q, "second"))
	if len(f.completions) != 3 {
		t.Fatalf("same prefix must not be warmed again, got %d completions", len(f.completions))
	}
	// another spec (other options) changes the prefix, so warm up
	ask(t, s, `{"state":"x","questions":{"q":{"type":"choice","instructions":"other?","criteria":{"c":"","d":""}}}}`)
	if len(f.completions) != 5 || f.completions[3].NPredict != 0 {
		t.Fatalf("a new prefix must be warmed, got %+v", f.completions[3:])
	}
	// cache_n dropped (checkpoint lost) → warm up again next time
	f.mu.Lock()
	f.cacheN = 3
	f.mu.Unlock()
	ask(t, s, `{"state":"y","questions":{"q":{"type":"choice","instructions":"other?","criteria":{"c":"","d":""}}}}`) // cache miss observed here
	n := len(f.completions)
	ask(t, s, `{"state":"z","questions":{"q":{"type":"choice","instructions":"other?","criteria":{"c":"","d":""}}}}`)
	if len(f.completions) != n+2 || f.completions[n].NPredict != 0 {
		t.Fatalf("after a cache miss the prefix must be warmed again, got %+v", f.completions[n:])
	}
	// no warm-up in the state-first layout (the prefix contains the state)
	f2 := &fakeUpstream{top: []tokenProb{{"A", 1}}}
	s2, _ := newTestServer(t, f2)
	s2.cfg.StateFirst = true
	ask(t, s2, fmt.Sprintf(q, "first"))
	if len(f2.completions) != 1 {
		t.Fatalf("state-first layout must not warm up, got %+v", f2.completions)
	}
}

// The layout can be chosen per request. plain has no system message, puts options as "- id: description" and the input after Input:,
// and reads the probability of the option id's first token instead of a letter (A, B, …). The layout is added to the response's model (calibration keys stay separate).
func TestPlainLayoutReadsOptionIdsAndNamesTheLayout(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"al", 0.7}, {"ze", 0.2}, {"mi", 0.05}, {"The", 0.05}}} // fake tokenizer: the first token is the first 2 characters
	s, _ := newTestServer(t, f)
	code, out := ask(t, s, `{"state":"fix the typo","layout":"plain","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"zeta":"Z desc","alpha":"A desc","mid":"M desc"}}}}`)
	if code != 200 {
		t.Fatalf("code %d: %v", code, out)
	}
	for _, must := range []string{"what?", "- zeta: Z desc\n- alpha: A desc\n- mid: M desc", "Input:\nfix the typo", "Answer with the option id only."} {
		if !strings.Contains(f.lastPrompt, must) {
			t.Fatalf("plain layout missing %q:\n%s", must, f.lastPrompt)
		}
	}
	if strings.Contains(f.lastPrompt, "<|system|>") || strings.Contains(f.lastPrompt, "<state>") || strings.Contains(f.lastPrompt, "A. ") {
		t.Fatalf("plain layout has no system message, no state tags and no letters:\n%s", f.lastPrompt)
	}
	ans := out["answers"].(map[string]any)["q"].(map[string]any)
	probs := ans["probabilities"].(map[string]any)
	if ans["choice"] != "alpha" || math.Abs(probs["alpha"].(float64)-0.7/0.95) > 1e-9 || math.Abs(probs["zeta"].(float64)-0.2/0.95) > 1e-9 {
		t.Fatalf("probabilities must be read from the ids' first tokens: %v", ans)
	}
	if out["model"] != "fake-gguf+plain" {
		t.Fatalf("the layout must be in the model name so calibration keys differ, got %v", out["model"])
	}
	// the default layout keeps its name as before
	code, out = ask(t, s, `{"state":"x","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A","b":"B"}}}}`)
	if code != 200 || out["model"] != "fake-gguf" {
		t.Fatalf("default layout keeps the configured name: %d %v", code, out["model"])
	}
	// an unknown layout gets 422. So do ids sharing a first token (they cannot be told apart)
	if code, _ := ask(t, s, `{"state":"x","layout":"sideways","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A","b":"B"}}}}`); code != 422 {
		t.Fatalf("unknown layout must be 422, got %d", code)
	}
	if code, out := ask(t, s, `{"state":"x","layout":"plain","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"change_request":"A","change_x":"B"}}}}`); code != 422 || !strings.Contains(fmt.Sprint(out), "first token") {
		t.Fatalf("ids sharing a first token cannot be read under plain layout: %d %v", code, out)
	}
}

// readProbs picks the labels' probabilities from the top tokens and normalizes them.
// A label that missed the top list gets probFloor (counted in missing).
func readProbs(top []tokenProb, n int) (probs []float64, missing int) {
	return readProbsFor(top, labels[:n])
}

// If the upstream is a trained decision model (answers /v1/systemone itself), readout passes requests through with no readout.
// So shikii can always talk to readout's endpoint (8080), and the endpoint does not change whether the model is Kev or an ordinary GGUF.
func TestPassesThroughToADecisionModelUpstream(t *testing.T) {
	f := &fakeUpstream{decision: true, top: []tokenProb{{Token: "A", Prob: 0.9}, {Token: "B", Prob: 0.1}}}
	s, _ := newTestServer(t, f)
	if err := s.detectUpstream(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.passThrough {
		t.Fatal("a 2xx /v1/systemone upstream must be passed through")
	}
	code, out := ask(t, s, `{"state":"x","questions":{"q":{"type":"noul","instructions":"Is this a probe?"}}}`)
	if code != 200 || out["model"] != "fake-decision" || out["answers"].(map[string]any)["q"].(map[string]any)["noul"] != 0.91 {
		t.Fatalf("the upstream's own answer must come back unchanged: %d %v", code, out)
	}
	f.mu.Lock()
	hits, completions := f.systemOneHits, len(f.completions)
	f.mu.Unlock()
	if hits != 2 || completions != 0 { // once for the startup probe, once for the real request. No letter readout
		t.Fatalf("pass-through must not read letters: systemone=%d completions=%d", hits, completions)
	}

	plain := &fakeUpstream{top: []tokenProb{{Token: "A", Prob: 0.9}, {Token: "B", Prob: 0.1}}}
	s2, _ := newTestServer(t, plain)
	if err := s2.detectUpstream(context.Background()); err != nil || s2.passThrough {
		t.Fatalf("a 501 upstream is a plain GGUF and is read out: err=%v passThrough=%v", err, s2.passThrough)
	}
	// a decision model that rejects the probe with 422 (it did read the request shape) is passed through too (audit 2026-10-23: it used to fail at startup)
	strict := &fakeUpstream{decision: true, rejectProbe: 422, top: []tokenProb{{Token: "A", Prob: 0.9}}}
	s3, _ := newTestServer(t, strict)
	if err := s3.detectUpstream(context.Background()); err != nil || !s3.passThrough {
		t.Fatalf("a decision model that rejects the probe with 4xx must still be passed through: err=%v passThrough=%v", err, s3.passThrough)
	}
	// 401 (auth) or 405 are not signs of a decision model, so stop starting up instead of silently passing through (audit R2)
	for _, code := range []int{401, 403, 405, 429} {
		locked := &fakeUpstream{decision: true, rejectProbe: code, top: []tokenProb{{Token: "A", Prob: 0.9}}}
		s4, _ := newTestServer(t, locked)
		if err := s4.detectUpstream(context.Background()); err == nil || s4.passThrough {
			t.Fatalf("%d must fail startup with the reason, not pass through: err=%v passThrough=%v", code, err, s4.passThrough)
		}
	}
}

// 0.9.6 audit of the HTTP limits, low 1: shikii-readout ships with shikii but listened with http.ListenAndServe and no limits, so a client
// that sent headers very slowly could hold its connections. It now bounds the header phase and size; model replies can take long, so the
// write phase stays open.
func TestReadoutServerLimitsSlowHeaders(t *testing.T) {
	srv := newReadoutServer("127.0.0.1:0", http.NewServeMux())
	if srv.ReadHeaderTimeout <= 0 || srv.MaxHeaderBytes <= 0 || srv.WriteTimeout != 0 {
		t.Fatalf("readout bounds headers but not writes: %+v", srv)
	}
}

// 0.9.7 full audit 2-8 (eaf2c9b: a slow connection must not hold resources): a request body sent one byte at a time was never cut by
// readout. Reading the request is bounded; here the bound is shortened to see that a dribbled body is cut by it.
func TestAuditReadoutSlowBodyHeld(t *testing.T) {
	var done atomic.Bool
	srv := newReadoutServer("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(io.LimitReader(r.Body, 4<<20))
		done.Store(true)
	}))
	if srv.ReadTimeout <= 0 || srv.ReadTimeout > time.Minute {
		t.Fatalf("reading a request is bounded: %v", srv.ReadTimeout)
	}
	srv.ReadTimeout = 500 * time.Millisecond
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln)
	defer srv.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "POST /v1/systemone HTTP/1.1\r\nHost: x\r\nContent-Length: 1000\r\n\r\n")
	for i := 0; i < 30; i++ { // 3 seconds, one byte every 100ms
		if _, err := c.Write([]byte("x")); err != nil {
			return // cut: fine
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !done.Load() {
		t.Fatal("a body dribbled for 3s, past the read bound, still holds the handler")
	}
}
