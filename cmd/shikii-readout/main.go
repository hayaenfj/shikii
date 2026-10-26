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
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
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
	warmPrefix string // the prefix warmed up last
	warmTokens int    // token count of that prefix (if cache_n falls short of it, warm up again next time)

	// passThrough is true when the upstream is a trained decision model (one that answers /v1/systemone itself, such as Kev or OpenJev).
	// Then requests pass through as is with no readout. shikii can always talk to readout's endpoint and never switches endpoints by model kind.
	passThrough bool
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
		writeErr(w, 502, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		writeErr(w, 502, err.Error())
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
	log.Printf("shikii-readout: model=%s upstream=%s listening on http://%s (POST /v1/systemone)", s.cfg.Model, s.cfg.Upstream, addr)
	log.Fatal(newReadoutServer(addr, mux).ListenAndServe())
}

// newReadoutServer is the HTTP server readout listens with. Slow request headers are cut off and their size is bounded; reading a request and
// writing the model's reply stay open, since a large model's reply can take minutes (0.9.6 audit of the HTTP limits).
func newReadoutServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
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

// parseRequest reads and validates the body. The criteria keep their JSON order (they are not put in a Go map).
func parseRequest(body []byte) (state string, qs []question, layout string, err error) {
	var req rawRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", nil, "", fmt.Errorf("bad json: %w", err)
	}
	if len(req.Questions) == 0 {
		return "", nil, "", errors.New("questions is required")
	}
	state = stateText(req.State)
	keys := make([]string, 0, len(req.Questions))
	for k := range req.Questions {
		keys = append(keys, k)
	}
	sort.Strings(keys) // question order does not affect the response (each is asked separately). Sorted for stability
	for _, k := range keys {
		rq := req.Questions[k]
		q, err := parseQuestion(k, rq)
		if err != nil {
			return "", nil, "", err
		}
		qs = append(qs, q)
	}
	return state, qs, req.Layout, nil
}

func parseQuestion(key string, rq rawQuestion) (question, error) {
	q := question{Key: key, Type: rq.Type, Instructions: strings.TrimSpace(rq.Instructions)}
	if q.Instructions == "" {
		return q, fmt.Errorf("questions.%s.instructions is required", key)
	}
	switch rq.Type {
	case "choice":
		var oc systemone.OrderedCriteria
		if len(rq.Criteria) == 0 || oc.UnmarshalJSON(rq.Criteria) != nil {
			return q, fmt.Errorf("questions.%s.criteria must be an object of id -> description", key)
		}
		for _, c := range oc {
			q.Options = append(q.Options, option{ID: c.ID, Description: c.Description})
		}
	case "score":
		var levels []string
		if len(rq.Criteria) == 0 || json.Unmarshal(rq.Criteria, &levels) != nil {
			return q, fmt.Errorf("questions.%s.criteria must be an array of levels (lowest first)", key)
		}
		for i, l := range levels {
			q.Options = append(q.Options, option{ID: fmt.Sprintf("%d", i), Description: l})
		}
	case "noul", "boolean":
		q.Type = "noul"
		yes, no := "yes", "no"
		if len(rq.Criteria) > 0 && string(rq.Criteria) != "null" {
			var m map[string]string
			if json.Unmarshal(rq.Criteria, &m) != nil {
				return q, fmt.Errorf("questions.%s.criteria must be a true/false (or yes/no) map", key)
			}
			for k, v := range m {
				switch strings.ToLower(k) {
				case "true", "yes":
					yes = v
				case "false", "no":
					no = v
				default:
					return q, fmt.Errorf("questions.%s.criteria: unknown key %q (true/false or yes/no)", key, k)
				}
			}
		}
		q.Options = []option{{ID: "yes", Description: yes}, {ID: "no", Description: no}}
	default:
		return q, fmt.Errorf("questions.%s.type %q is not choice, score or noul", key, rq.Type)
	}
	if n := len(q.Options); n < 2 || n > len(labels) {
		return q, fmt.Errorf("questions.%s: %d options (need 2..%d)", key, n, len(labels))
	}
	return q, nil
}

func stateText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		return buf.String()
	}
	return string(raw)
}

// ---- prompt ----

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// buildMessages builds the conversation for one question. Options get A, B, … in display order.
// With stateFirst=false (the default) the state goes last: the leading system message, question and options form a prefix
// shared by every question of the same spec, which llama-server's cache_prompt reuses.
func buildMessages(system, state string, q question, layout string) []message {
	if layout == "plain" {
		return buildPlain(state, q)
	}
	stateFirst := layout == "state-first"
	var b strings.Builder
	stateBlock := "<state>\n" + state + "\n</state>\n"
	if stateFirst {
		b.WriteString(stateBlock)
		b.WriteByte('\n')
	}
	switch q.Type {
	case "choice":
		b.WriteString("Question: ")
		b.WriteString(q.Instructions)
		b.WriteString("\nOptions:\n")
	case "score":
		b.WriteString("Question: ")
		b.WriteString(q.Instructions)
		b.WriteString("\nScale (lowest first):\n")
	case "noul":
		b.WriteString("Is the following statement true of the state?\nStatement: ")
		b.WriteString(q.Instructions)
		b.WriteString("\nOptions:\n")
	}
	for i, o := range q.Options {
		b.WriteString(labels[i])
		b.WriteString(". ")
		if q.Type == "choice" {
			b.WriteString(o.ID)
			if o.Description != "" && o.Description != o.ID {
				b.WriteString(": ")
				b.WriteString(o.Description)
			}
		} else {
			b.WriteString(o.Description)
		}
		b.WriteByte('\n')
	}
	if !stateFirst {
		b.WriteByte('\n')
		b.WriteString(stateBlock)
	}
	b.WriteString("\nReply with one letter.")
	return []message{{Role: "system", Content: system}, {Role: "user", Content: b.String()}}
}

// buildPlain is the plain layout: no system message, the question, options as "- id: description", the input after Input:, and finally a request to answer with an id.
// It reads the first token of the option id itself, not a letter (A, B, …). For a plain chat model this can fit better than the layout meant for decision models
// (13 points on Qwen3-4B, docs/bench.md).
func buildPlain(state string, q question) []message {
	var b strings.Builder
	switch q.Type {
	case "noul":
		b.WriteString("Is the following statement true of the input?\nStatement: ")
		b.WriteString(q.Instructions)
		b.WriteString("\n\nOptions:\n")
	case "score":
		b.WriteString(q.Instructions)
		b.WriteString("\n\nScale (lowest first):\n")
	default:
		b.WriteString(q.Instructions)
		b.WriteString("\n\nOptions:\n")
	}
	for _, o := range q.Options {
		b.WriteString("- ")
		b.WriteString(o.ID)
		if o.Description != "" && o.Description != o.ID {
			b.WriteString(": ")
			b.WriteString(o.Description)
		}
		b.WriteByte('\n')
	}
	b.WriteString(plainMarker)
	b.WriteString(state)
	b.WriteString("\n\nAnswer with the option id only.")
	return []message{{Role: "user", Content: b.String()}}
}

// ---- readout ----

type tokenProb struct {
	Token string  `json:"token"`
	Prob  float64 `json:"prob"`
}

// readProbsFor picks the probability for each key (a letter, or in the plain layout the first token of an option id) from the top list and normalizes them.
func readProbsFor(top []tokenProb, keys []string) (probs []float64, missing int) {
	byTok := map[string]float64{}
	for _, t := range top {
		if _, seen := byTok[t.Token]; !seen {
			byTok[t.Token] = t.Prob
		}
	}
	probs = make([]float64, len(keys))
	var sum float64
	for i, k := range keys {
		p, ok := byTok[k]
		if !ok || p <= 0 {
			p = probFloor
			missing++
		}
		probs[i] = p
		sum += p
	}
	for i := range probs {
		probs[i] /= sum
	}
	return probs, missing
}

// optionKeys returns the tokens to read for the layout: letters (A, B, …) by default; for plain, the first token of each id (looked up with /tokenize and remembered).
// If two ids share the same first token they cannot be told apart, so it is an error.
func (s *server) optionKeys(ctx context.Context, q question, layout string) ([]string, error) {
	if layout != "plain" {
		return labels[:len(q.Options)], nil
	}
	keys := make([]string, len(q.Options))
	seen := map[string]string{}
	for i, o := range q.Options {
		piece, err := s.firstPiece(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		if other, dup := seen[piece]; dup {
			return nil, fmt.Errorf("plain layout: options %q and %q share the first token %q and cannot be told apart; use letters (state-last) or rename an option", other, o.ID, piece)
		}
		seen[piece] = o.ID
		keys[i] = piece
	}
	return keys, nil
}

// firstPiece is the text of a string's first token (/tokenize with_pieces).
func (s *server) firstPiece(ctx context.Context, text string) (string, error) {
	s.mu.Lock()
	if p, ok := s.pieces[text]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()
	var out struct {
		Tokens []struct {
			ID    int    `json:"id"`
			Piece string `json:"piece"`
		} `json:"tokens"`
	}
	if _, err := s.post(ctx, "/tokenize", map[string]any{"content": text, "with_pieces": true}, &out); err != nil {
		return "", err
	}
	if len(out.Tokens) == 0 || out.Tokens[0].Piece == "" {
		return "", fmt.Errorf("/tokenize returned no pieces for %q (with_pieces unsupported?)", text)
	}
	s.mu.Lock()
	if s.pieces == nil {
		s.pieces = map[string]string{}
	}
	s.pieces[text] = out.Tokens[0].Piece
	s.mu.Unlock()
	return out.Tokens[0].Piece, nil
}

// requestLayout picks the request's layout: if empty, the startup default; an unknown name is an error.
func (s *server) requestLayout(req string) (string, error) {
	if req == "" {
		return s.cfg.layout(), nil
	}
	if !layouts[req] {
		return "", fmt.Errorf("unknown layout %q (one of state-last, state-first, plain)", req)
	}
	return req, nil
}

// responseModel is the response's model name: as configured for the default layout, otherwise the base name + "+" + the layout (so calibration keys stay separate).
func (s *server) responseModel(layout string) string {
	if layout == s.cfg.layout() {
		return s.cfg.Model
	}
	return s.base + "+" + layout
}

// answerFor assembles the System One answer per type (the same shape as llama.cpp's decision models).
func answerFor(q question, probs []float64) map[string]any {
	best, bp := 0, -1.0
	for i, p := range probs {
		if p > bp {
			best, bp = i, p
		}
	}
	switch q.Type {
	case "noul":
		return map[string]any{"type": "noul", "noul": probs[0]}
	case "score":
		pm := map[string]float64{}
		legend := map[string]string{}
		var exp float64
		for i, o := range q.Options {
			pm[o.ID] = probs[i]
			legend[o.ID] = o.Description
			exp += float64(i) * probs[i]
		}
		return map[string]any{"type": "score", "score": exp, "probabilities": pm, "legend": legend, "confidence": bp}
	default:
		pm := map[string]float64{}
		for i, o := range q.Options {
			pm[o.ID] = probs[i]
		}
		n := float64(len(probs))
		conf := 0.0
		if n > 1 {
			conf = (bp - 1/n) / (1 - 1/n)
		}
		return map[string]any{"type": "choice", "choice": q.Options[best].ID, "probabilities": pm, "confidence": conf}
	}
}

// ---- upstream(llama-server) ----

func (s *server) post(ctx context.Context, path string, body any, out any) (int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Upstream+path, bytes.NewReader(buf))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("%s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s: bad json: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

func (s *server) upstreamModel(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.Upstream+"/v1/models", nil)
	if err != nil {
		return "", err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if len(v.Models) > 0 && v.Models[0].Name != "" {
		return v.Models[0].Name, nil
	}
	if len(v.Data) > 0 && v.Data[0].ID != "" {
		return v.Data[0].ID, nil
	}
	return "", errors.New("no model in /v1/models")
}

// checkLabels checks with /tokenize that every label is a single token.
func (s *server) checkLabels(ctx context.Context) error {
	var bad []string
	for _, l := range labels {
		var out struct {
			Tokens []json.RawMessage `json:"tokens"`
		}
		if _, err := s.post(ctx, "/tokenize", map[string]any{"content": l}, &out); err != nil {
			return err
		}
		if len(out.Tokens) != 1 {
			bad = append(bad, fmt.Sprintf("%s=%d", l, len(out.Tokens)))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("labels are not single tokens: %s", strings.Join(bad, " "))
	}
	return nil
}

// applyTemplate builds the prompt with the model's chat template. It tries enable_thinking=false,
// and if that is rejected (an upstream that does not accept kwargs) stops sending it.
func (s *server) applyTemplate(ctx context.Context, msgs []message) (string, error) {
	s.mu.Lock()
	kw := s.thinkingKwarg
	s.mu.Unlock()
	body := map[string]any{"messages": msgs}
	if kw {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	var out struct {
		Prompt string `json:"prompt"`
	}
	code, err := s.post(ctx, "/apply-template", body, &out)
	if err != nil && kw && code >= 400 && code < 500 {
		s.mu.Lock()
		s.thinkingKwarg = false
		s.mu.Unlock()
		delete(body, "chat_template_kwargs")
		_, err = s.post(ctx, "/apply-template", body, &out)
	}
	if err != nil {
		return "", err
	}
	if out.Prompt == "" {
		return "", errors.New("/apply-template returned an empty prompt")
	}
	return out.Prompt, nil
}

type completion struct {
	Content string `json:"content"`
	Probs   []struct {
		Top []tokenProb `json:"top_probs"`
	} `json:"completion_probabilities"`
	Timings struct {
		PromptN int `json:"prompt_n"`
		CacheN  int `json:"cache_n"`
	} `json:"timings"`
}

// warmPrefixFor sends the prompt's prefix (up to just before the state) to the upstream with n_predict=0 when it differs from last time,
// so that a checkpoint is made. In the state-first layout the prefix contains the state, so it does nothing.
func (s *server) warmPrefixFor(ctx context.Context, prompt, layout string) error {
	marker := stateMarker
	switch layout {
	case "state-first":
		return nil
	case "plain":
		marker = plainMarker
	}
	idx := strings.LastIndex(prompt, marker)
	if idx <= 0 {
		return nil
	}
	prefix := prompt[:idx]
	s.mu.Lock()
	same := prefix == s.warmPrefix
	s.mu.Unlock()
	if same {
		return nil
	}
	var out completion
	if _, err := s.post(ctx, "/completion", map[string]any{"prompt": prefix, "n_predict": 0, "cache_prompt": true}, &out); err != nil {
		return err
	}
	s.mu.Lock()
	s.warmPrefix = prefix
	s.warmTokens = out.Timings.PromptN + out.Timings.CacheN
	s.mu.Unlock()
	return nil
}

// noteCache looks at the response's cache_n and, if prefix reuse broke, marks it to warm up again next time.
func (s *server) noteCache(c *completion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmTokens > 0 && c.Timings.CacheN < s.warmTokens*9/10 {
		s.warmPrefix = ""
	}
}

// complete runs a single token and returns the top tokens' probabilities. The sampler is neutral (temperature 1, no truncation).
func (s *server) complete(ctx context.Context, prompt string) (*completion, error) {
	body := map[string]any{
		"prompt": prompt, "n_predict": 1, "temperature": 1.0, "top_k": 0, "top_p": 1.0, "min_p": 0.0,
		"n_probs": s.cfg.NProbs, "post_sampling_probs": true, "cache_prompt": true,
	}
	var out completion
	if _, err := s.post(ctx, "/completion", body, &out); err != nil {
		return nil, err
	}
	if len(out.Probs) == 0 {
		return nil, errors.New("/completion returned no completion_probabilities (n_probs unsupported?)")
	}
	return &out, nil
}

// ---- handlers ----

func (s *server) handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": s.cfg.Model, "object": "model"}}})
}

func (s *server) handleSystemOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST only")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if s.passThrough {
		s.forward(w, r, body)
		return
	}
	state, qs, layoutReq, err := parseRequest(body)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	layout, err := s.requestLayout(layoutReq)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()
	answers := map[string]any{}
	inputTokens := 0
	for _, q := range qs {
		keys, err := s.optionKeys(ctx, q, layout)
		if err != nil {
			writeErr(w, 422, err.Error())
			return
		}
		prompt, err := s.applyTemplate(ctx, buildMessages(s.cfg.System, state, q, layout))
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		if err := s.warmPrefixFor(ctx, prompt, layout); err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		c, err := s.complete(ctx, prompt)
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		s.noteCache(c)
		probs, missing := readProbsFor(c.Probs[0].Top, keys)
		s.mu.Lock()
		s.missingLabels += missing
		s.totalQuestions++
		s.mu.Unlock()
		if missing > 0 {
			log.Printf("question %s: %d/%d labels below n_probs=%d (floored)", q.Key, missing, len(q.Options), s.cfg.NProbs)
		}
		answers[q.Key] = answerFor(q, probs)
		inputTokens += c.Timings.PromptN
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":   s.responseModel(layout),
		"answers": answers,
		"usage":   map[string]int{"input_tokens": inputTokens, "output_tokens": len(qs)},
	})
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
