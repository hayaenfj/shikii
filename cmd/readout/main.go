// shikii-readout は普通のチャット用 GGUF を System One 形式(/v1/systemone)の決定モデルにする薄いアダプタ。
//
// 推論は持たない。後ろにいる llama-server(本家でも fork でも)の /apply-template でそのモデルの
// chat template を当て、/completion を 1 トークンだけ回して、選択肢に割り当てた文字(A, B, …)の
// 確率を読む。返す確率は素の softmax で、較正はしない(較正は shikii calibrate が (spec_hash, model) ごとに当てる)。
//
//	shikii-readout --upstream http://127.0.0.1:8086 [--port 8090] [--model NAME] [--n-probs 64]
//
// 起動時に /tokenize で全ラベルが 1 トークンであることを確かめ、違えば起動しない。
// Qwen3 系の <think> は chat_template_kwargs の enable_thinking=false で潰す(受け付けない upstream には送り直す)。
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

// labels は選択肢に割り当てる文字。裸の 1 文字(先頭に空白なし)。chat template が改行で終わるので
// 最初のトークンは裸の文字になる。数字は多くのトークナイザで 2 トークンになるので使わない。
var labels = []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}

// probFloor は n_probs の上位に入らなかったラベルに与える確率(正規化前)。0 にすると log が取れず較正が壊れる。
const probFloor = 1e-6

type config struct {
	Upstream string
	Model    string
	NProbs   int
	System   string
	Timeout  time.Duration
	// StateFirst は state を選択肢の前に置く(旧配置)。既定は後ろ: system・質問・選択肢が
	// 同じ spec の全問で共通の接頭辞になり、llama-server の cache_prompt で prefill が state のぶんだけになる。
	StateFirst bool
}

// layout は prompt の配置の名前。応答の model に付けて、配置が違えば較正の鍵も違うようにする
// (同じモデルでも並びが変われば確率の出方が変わる)。
func (c config) layout() string {
	if c.StateFirst {
		return "state-first"
	}
	return "state-last"
}

// modelName は応答の model。--model が無ければ upstream の名前 + "+" + 配置。
func modelName(upstream string, c config) string {
	if c.Model != "" {
		return c.Model
	}
	return upstream + "+" + c.layout()
}

type server struct {
	cfg  config
	http *http.Client
	base string // 配置の接尾辞を付ける前の名前(upstream の名前か --model)。既定以外の配置の応答は base+"+"+配置

	mu             sync.Mutex
	pieces         map[string]string // plain 配置で読む、選択肢 id → 最初のトークン(/tokenize の結果を覚える)
	thinkingKwarg  bool              // enable_thinking=false を送るか。upstream が蹴ったら false に落とす
	missingLabels  int               // n_probs の上位に入らなかったラベルの延べ数(観測用)
	totalQuestions int

	// 接頭辞の warm-up。Qwen3.5/3.8 系(Bonsai の土台)は再帰層を持ち、KV を途中まで巻き戻せない。
	// 接頭辞のちょうど末尾にチェックポイントが無いと cache_prompt が効かず全部やり直しになる
	// (2026-10-17 の実測: cache_n=0、1 問 4 秒)。そこで接頭辞(state の手前まで)だけを n_predict=0 で
	// 先に流しておく。すると以後は state のぶんだけの prefill になる(cache_n=137/154、1 問 0.5〜1.1 秒)。
	warmPrefix string // 最後に warm-up した接頭辞
	warmTokens int    // その接頭辞のトークン数(cache_n がこれに届かなければ次回また warm-up)
}

// stateMarker は prompt 中で state が始まる印。ここより前が spec 内で共通の接頭辞。
const stateMarker = "<state>"

func main() {
	upstream := flag.String("upstream", "http://127.0.0.1:8086", "後ろの llama-server")
	port := flag.Int("port", 8090, "待ち受けポート")
	model := flag.String("model", "", "応答の model(較正の鍵)。空なら upstream の /v1/models から取る")
	nprobs := flag.Int("n-probs", 64, "/completion の n_probs。選択肢の数より十分大きく")
	system := flag.String("system", defaultSystem, "system prompt")
	stateFirst := flag.Bool("state-first", false, "state を選択肢の前に置く(旧配置。prefix cache が効かない)")
	flag.Parse()

	s := &server{
		cfg:           config{Upstream: strings.TrimRight(*upstream, "/"), Model: *model, NProbs: *nprobs, System: *system, Timeout: 120 * time.Second, StateFirst: *stateFirst},
		http:          &http.Client{Timeout: 120 * time.Second},
		thinkingKwarg: true,
	}
	ctx := context.Background()
	// 後ろの llama-server が立つのを自分で待つ(Docker で先にモデルを起こす時。curl / wget に頼らない)
	wctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	if err := systemone.WaitReady(wctx, s.cfg.Upstream, 3*time.Second, log.Printf); err != nil {
		log.Fatal(err)
	}
	cancel()
	if s.cfg.Model == "" {
		name, err := s.upstreamModel(ctx)
		if err != nil {
			log.Fatalf("upstream model name: %v (pass --model)", err)
		}
		s.base = name
		s.cfg.Model = modelName(name, s.cfg)
	} else {
		s.base = s.cfg.Model
	}
	if err := s.checkLabels(ctx); err != nil {
		log.Fatalf("label check failed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/systemone", s.handleSystemOne)
	addr := fmt.Sprintf("%s:%d", systemone.BindHost(), *port)
	log.Printf("shikii-readout: model=%s upstream=%s listening on http://%s (POST /v1/systemone)", s.cfg.Model, s.cfg.Upstream, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

const defaultSystem = "You are a careful classifier. Read the state, then answer the question by choosing exactly one option. Reply with the option's letter only."

// ---- 要求の形 ----

type rawQuestion struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

type rawRequest struct {
	State     json.RawMessage        `json:"state"`
	Model     string                 `json:"model"`
	Questions map[string]rawQuestion `json:"questions"`
	Layout    string                 `json:"layout"` // 配置の名前(state-last | state-first | plain)。空なら起動時の既定
}

// layouts は知っている配置。spec の layout と同じ名前。
var layouts = map[string]bool{"state-last": true, "state-first": true, "plain": true}

// plainMarker は plain 配置で state が始まる印(ここより前が spec 内で共通の接頭辞)。
const plainMarker = "\nInput:\n"

// question は解釈済みの質問。Options は提示順(choice は criteria の並び、score は level の並び、noul は yes/no)。
type question struct {
	Key          string
	Type         string
	Instructions string
	Options      []option
}

type option struct {
	ID          string // 応答のキー(choice は選択肢 id、score は "0".."n-1"、noul は yes/no)
	Description string
}

// parseRequest は本体を読んで検証する。criteria の並びは JSON の並びのまま(Go の map に入れない)。
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
	sort.Strings(keys) // 質問の順は応答に影響しない(それぞれ独立に聞く)。安定のために並べる
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

// ---- プロンプト ----

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// buildMessages は 1 質問ぶんの会話を作る。選択肢は提示順に A, B, … を付ける。
// stateFirst=false(既定)では state を最後に置く: 先頭の system・質問・選択肢が同じ spec の全問で
// 共通の接頭辞になり、llama-server の cache_prompt で再利用される。
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

// buildPlain は plain 配置: system 無し、質問、選択肢を "- id: 説明"、入力を Input: の後ろ、最後に id で答えるよう頼む。
// 文字(A, B, …)ではなく選択肢 id そのものの最初のトークンを読む。素のチャット用モデルには、決定モデル向けの並べ方より合うことがある
// (Qwen3-4B で 13 ポイント、docs/bench.md)。
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

// ---- 読み出し ----

type tokenProb struct {
	Token string  `json:"token"`
	Prob  float64 `json:"prob"`
}

// readProbs は上位トークンの確率からラベルの確率を拾い、正規化する。
// 上位に入らなかったラベルには probFloor を与える(missing に数える)。
func readProbs(top []tokenProb, n int) (probs []float64, missing int) {
	return readProbsFor(top, labels[:n])
}

// readProbsFor は keys(文字、または plain 配置では選択肢 id の最初のトークン)ごとの確率を上位から拾って正規化する。
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

// optionKeys は配置に応じて読むトークンを返す: 既定は文字(A, B, …)、plain は各 id の最初のトークン(/tokenize で調べ、覚える)。
// 2 つの id が同じ最初のトークンなら読み分けられないので誤り。
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

// firstPiece は文字列の最初のトークンの文字列(/tokenize with_pieces)。
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

// requestLayout は要求の配置を決める: 空なら起動時の既定、知らない名前は誤り。
func (s *server) requestLayout(req string) (string, error) {
	if req == "" {
		return s.cfg.layout(), nil
	}
	if !layouts[req] {
		return "", fmt.Errorf("unknown layout %q (one of state-last, state-first, plain)", req)
	}
	return req, nil
}

// responseModel は応答の model 名。既定の配置なら設定どおり、違う配置なら土台の名前 + "+" + 配置(較正の鍵が分かれる)。
func (s *server) responseModel(layout string) string {
	if layout == s.cfg.layout() {
		return s.cfg.Model
	}
	return s.base + "+" + layout
}

// answerFor は System One の answer を型ごとに組む(llama.cpp の決定モデルと同じ形)。
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

// checkLabels は全ラベルが 1 トークンかを /tokenize で確かめる。
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

// applyTemplate はそのモデルの chat template で prompt を作る。enable_thinking=false を試し、
// 蹴られたら(kwargs を受けない upstream)以後は付けない。
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

// warmPrefixFor は prompt の接頭辞(state の手前まで)を、前回と違えば upstream に n_predict=0 で流し、
// チェックポイントを作らせる。state-first 配置では接頭辞が state を含むので何もしない。
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

// noteCache は応答の cache_n を見て、接頭辞の再利用が崩れていたら次回 warm-up し直す印を付ける。
func (s *server) noteCache(c *completion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmTokens > 0 && c.Timings.CacheN < s.warmTokens*9/10 {
		s.warmPrefix = ""
	}
}

// complete は 1 トークンだけ回して上位トークンの確率を返す。サンプラは素通し(温度 1、切り捨て無し)。
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

// ---- ハンドラ ----

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
