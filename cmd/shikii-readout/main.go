// shikii-readout is a thin adapter that turns an ordinary chat GGUF into a decision model speaking the System One format (/v1/systemone).
//
// It does no inference of its own. It applies the model's chat template through /apply-template of the llama-server behind it
// (upstream or a fork), runs /completion for a single token, and reads the probabilities of the letters (A, B, …)
// assigned to the options. The probabilities it returns are a plain softmax with no calibration (shikii calibrate applies calibration per (spec_hash, model)).
//
//	shikii-readout --upstream http://127.0.0.1:8086 [--port 8090] [--model NAME] [--n-probs 64]
//
// At startup it checks with /tokenize that every label is a single token, and refuses to start otherwise.
// The <think> of Qwen3-family models is suppressed with enable_thinking=false in chat_template_kwargs (resent without it to upstreams that reject it).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// labels are the letters assigned to options: a bare single character (no leading space). The chat template ends with a newline,
// so the first token is the bare letter. Digits are not used because many tokenizers split them into 2 tokens.
var labels = []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}

// probFloor is the probability (before normalizing) given to labels that did not make the n_probs top list. With 0, the log cannot be taken and calibration breaks.
const probFloor = 1e-6

type config struct {
	Upstream string
	Model    string
	NProbs   int
	System   string
	Timeout  time.Duration
	// StateFirst puts the state before the options (the old layout). The default puts it after: the system message, question and options
	// form a prefix shared by every question of the same spec, so with llama-server's cache_prompt only the state needs prefill.
	StateFirst bool
}

// layout is the name of the prompt layout. It is added to the response's model so that a different layout gets a different calibration key
// (even with the same model, a different order changes how the probabilities come out).
func (c config) layout() string {
	if c.StateFirst {
		return "state-first"
	}
	return "state-last"
}

// modelName is the response's model. Without --model it is the upstream's name + "+" + the layout.
func modelName(upstream string, c config) string {
	if c.Model != "" {
		return c.Model
	}
	return upstream + "+" + c.layout()
}

type server struct {
	cfg  config
	http *http.Client
	base string // the name before the layout suffix (the upstream's name or --model). Responses for a non-default layout use base+"+"+layout

	mu             sync.Mutex
	pieces         map[string]string // for the plain layout: option id → first token (remembers the /tokenize result)
	thinkingKwarg  bool              // whether to send enable_thinking=false. Drops to false if the upstream rejects it
	missingLabels  int               // running count of labels that missed the n_probs top list (for monitoring)
	totalQuestions int

	// Prefix warm-up. The Qwen3.5/3.8 family (Bonsai's base) has recurrent layers and cannot roll the KV cache back part way.
	// Without a checkpoint exactly at the end of the prefix, cache_prompt does not help and everything is redone
	// (measured 2026-10-17: cache_n=0, 4 seconds per question). So the prefix alone (up to just before the state) is run first
	// with n_predict=0. After that only the state needs prefill (cache_n=137/154, 0.5 to 1.1 seconds per question).
	warmPrefix string                   // the prefix warmed up last
	warmTokens int                      // token count of that prefix (if cache_n falls short of it, warm up again next time)
	warmups    map[string]*prefixWarmup // only in-flight work; completed prefixes are not retained here

	// passThrough is true when the upstream is a trained decision model (one that answers /v1/systemone itself, such as Kev or OpenJev).
	// Then requests pass through as is with no readout. shikii can always talk to readout's endpoint and never switches endpoints by model kind.
	passThrough bool
}

type prefixWarmup struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	err     error
}

// detectUpstream probes once whether the upstream is a decision model (whether /v1/systemone answers). Upstream llama.cpp returns 501 for an ordinary GGUF.
func (s *server) detectUpstream(ctx context.Context) error {
	probe := map[string]any{"state": "probe", "questions": map[string]any{"q": map[string]any{"type": "noul", "instructions": "Is this a probe?"}}}
	code, err := s.post(ctx, "/v1/systemone", probe, nil)
	switch {
	case err == nil && code/100 == 2:
		s.passThrough = true
		return nil
	case code == 501 || code == 404:
		s.passThrough = false
		return nil
	case code == 400 || code == 422: // post turns anything but 2xx into err, so look only at code
		// a decision model that rejects the probe's shape with 400/422 (it did read the request). Pass through and let that model answer real requests (audit 2026-10-23).
		// 401/403/405/429 and the like are auth or front-end refusals, so instead of silently passing through, stop starting up and show why (audit R2)
		s.passThrough = true
		return nil
	}
	return fmt.Errorf("upstream /v1/systemone: %v", err)
}

// forward passes the request to the upstream as is and returns the response as is (it never alters the decision model's answer).
func (s *server) forward(w http.ResponseWriter, r *http.Request, body []byte) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Upstream+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		writeErr(w, 502, systemone.Scrub(err.Error(), s.cfg.Upstream))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		writeErr(w, 502, systemone.Scrub(err.Error(), s.cfg.Upstream))
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
}

// stateMarker marks where the state starts in the prompt. Everything before it is the prefix shared within the spec.
const stateMarker = "<state>"

func main() {
	upstream := flag.String("upstream", "http://127.0.0.1:8086", "the llama-server behind it")
	port := flag.Int("port", 8090, "port to listen on")
	model := flag.String("model", "", "the response's model (the calibration key). If empty, taken from the upstream's /v1/models")
	nprobs := flag.Int("n-probs", 64, "n_probs for /completion. Make it well above the number of options")
	system := flag.String("system", defaultSystem, "system prompt")
	stateFirst := flag.Bool("state-first", false, "put the state before the options (the old layout; the prefix cache does not help)")
	flag.Parse()

	s := &server{
		cfg:           config{Upstream: strings.TrimRight(*upstream, "/"), Model: *model, NProbs: *nprobs, System: *system, Timeout: 120 * time.Second, StateFirst: *stateFirst},
		http:          &http.Client{Timeout: 120 * time.Second},
		thinkingKwarg: true,
	}
	ctx := context.Background()
	// wait for the llama-server behind it to come up (when Docker starts the model first; no reliance on curl / wget)
	wctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	if err := systemone.WaitReady(wctx, s.cfg.Upstream, 3*time.Second, log.Printf); err != nil {
		log.Fatal(err)
	}
	cancel()
	if err := s.detectUpstream(ctx); err != nil {
		log.Fatal(err)
	}
	if s.cfg.Model == "" {
		name, err := s.upstreamModel(ctx)
		if err != nil {
			log.Fatalf("upstream model name: %v (pass --model)", err)
		}
		s.base = name
		s.cfg.Model = modelName(name, s.cfg)
		if s.passThrough {
			s.cfg.Model = name // the decision model's answers pass through as is, so the name (the calibration key) stays the upstream's too
		}
	} else {
		s.base = s.cfg.Model
	}
	if s.passThrough {
		log.Printf("shikii-readout: %s answers /v1/systemone itself (decision model); passing requests through", s.cfg.Model)
	} else if err := s.checkLabels(ctx); err != nil {
		log.Fatalf("label check failed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/systemone", s.handleSystemOne)
	addr := fmt.Sprintf("%s:%d", systemone.BindHost(), *port)
	log.Printf("shikii-readout: model=%s upstream=%s listening on http://%s (POST /v1/systemone)", s.cfg.Model, systemone.RedactURL(s.cfg.Upstream), addr)
	log.Fatal(newReadoutServer(addr, mux).ListenAndServe())
}

// newReadoutServer is the HTTP server readout listens with. Slow request headers are cut off and their size is bounded; reading a request and
// writing the model's reply stays open, since a large model's reply can take minutes (0.9.6 audit of the HTTP limits); reading the request
// body is bounded too, since nothing makes it slow (0.9.7 full audit 2-8).
func newReadoutServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: time.Minute, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
}

const defaultSystem = "You are a careful classifier. Read the state, then answer the question by choosing exactly one option. Reply with the option's letter only."

// ---- request shape ----

type rawQuestion struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

type rawRequest struct {
	State     json.RawMessage        `json:"state"`
	Model     string                 `json:"model"`
	Questions map[string]rawQuestion `json:"questions"`
	Layout    string                 `json:"layout"` // layout name (state-last | state-first | plain). If empty, the default set at startup
}

// layouts are the known layouts, with the same names as the spec's layout.
var layouts = map[string]bool{"state-last": true, "state-first": true, "plain": true}

// plainMarker marks where the state starts in the plain layout (everything before it is the prefix shared within the spec).
const plainMarker = "\nInput:\n"

// question is a parsed question. Options are in display order (choice: the criteria order; score: the level order; noul: yes/no).
type question struct {
	Key          string
	Type         string
	Instructions string
	Options      []option
}

type option struct {
	ID          string // key in the response (choice: option id; score: "0".."n-1"; noul: yes/no)
	Description string
}

// ---- prompt ----

// ---- readout ----

// ---- upstream(llama-server) ----

// ---- handlers ----
