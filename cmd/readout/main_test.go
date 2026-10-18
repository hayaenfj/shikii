package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeUpstream は llama-server のふり。/tokenize・/apply-template・/completion・/v1/models。
type fakeUpstream struct {
	mu            sync.Mutex
	rejectKwargs  bool            // chat_template_kwargs を 400 で蹴る(本家の古い版の真似)
	multiToken    map[string]bool // このラベルは 2 トークンだと答える
	top           []tokenProb     // /completion が返す上位
	lastTemplate  map[string]any  // 最後の /apply-template 本体
	lastPrompt    string          // 最後の /completion の prompt
	templateCalls int
	completions   []fakeCompletion // /completion の履歴(warm-up を含む)
	cacheN        int              // /completion が timings.cache_n として返す値
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
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Content    string `json:"content"`
			WithPieces bool   `json:"with_pieces"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		multi := f.multiToken[in.Content]
		f.mu.Unlock()
		if in.WithPieces { // 最初のトークンは先頭 2 文字(短ければ全部)という偽のトークナイザ
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

// 選択肢は JSON の並びのまま A, B, C を割り当てる。Go の map に入れて鍵がソートされると位置が変わってしまう。
func TestChoiceKeepsCriteriaOrderAndMapsLetters(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"B", 0.7}, {"A", 0.2}, {"C", 0.05}, {"The", 0.05}}}
	s, _ := newTestServer(t, f)
	// 鍵はアルファベット順でない: zeta, alpha, mid
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
	// 正規化はラベルの確率だけで行う: 0.2/0.95, 0.7/0.95, 0.05/0.95
	if math.Abs(probs["alpha"].(float64)-0.7/0.95) > 1e-9 || math.Abs(probs["zeta"].(float64)-0.2/0.95) > 1e-9 {
		t.Fatalf("probabilities must be renormalised over labels only: %v", probs)
	}
	if out["model"] != "fake-gguf" {
		t.Fatalf("model must be the configured name, got %v", out["model"])
	}
}

// 上位に入らなかったラベルは床の確率を得て、合計は 1 のまま。0 にすると較正の log が壊れる。
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

// noul は P(yes)。yes が先頭(A)。
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
	// criteria に true/false の説明があれば使う。他のキーは 422
	code, _ = ask(t, s, `{"state":"x","questions":{"same":{"type":"noul","instructions":"q","criteria":{"maybe":"?"}}}}`)
	if code != 422 {
		t.Fatalf("unknown noul criteria key must be 422, got %d", code)
	}
}

// score は llama.cpp の決定モデルと同じ形: probabilities は "0".."n-1"、legend、score は期待値。
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

// enable_thinking=false を送る。蹴られる upstream には送り直し、以後は付けない。
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

// 起動時のラベル検査: 1 トークンでないラベルがあれば起動しない。
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

// 要求の検証: 型・instructions・選択肢の数。
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
	// 27 択は多すぎる(ラベルは 26 文字)
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
	// object の state は整形して入れる
	code, _ := ask(t, s, `{"state":{"b":1,"a":"x"},"questions":{"q":{"type":"choice","instructions":"q","criteria":{"a":"","b":""}}}}`)
	if code != 200 || !strings.Contains(f.lastPrompt, "<state>\n{\n  \"b\": 1,\n  \"a\": \"x\"\n}\n</state>") {
		t.Fatalf("object state must be pretty-printed in request key order:\n%s", f.lastPrompt)
	}
}

// 既定の配置は state を最後に置く(system・質問・選択肢が共通の接頭辞になり prefix cache が効く)。
// 配置は応答の model に付く: 並びが変われば確率の出方が変わるので、較正の鍵も変わるべき。
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

// 接頭辞の warm-up: 新しい接頭辞では state の手前までを n_predict=0 で先に流す。同じ接頭辞の 2 問目では流さない。
// 応答の cache_n が接頭辞に届かなければ、次の問いでまた流す(チェックポイントが消えた時)。
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
	// 別の spec(別の選択肢)なら接頭辞が変わるので warm-up
	ask(t, s, `{"state":"x","questions":{"q":{"type":"choice","instructions":"other?","criteria":{"c":"","d":""}}}}`)
	if len(f.completions) != 5 || f.completions[3].NPredict != 0 {
		t.Fatalf("a new prefix must be warmed, got %+v", f.completions[3:])
	}
	// cache_n が落ちた(チェックポイント消失)→ 次は warm-up し直す
	f.mu.Lock()
	f.cacheN = 3
	f.mu.Unlock()
	ask(t, s, `{"state":"y","questions":{"q":{"type":"choice","instructions":"other?","criteria":{"c":"","d":""}}}}`) // cache miss observed here
	n := len(f.completions)
	ask(t, s, `{"state":"z","questions":{"q":{"type":"choice","instructions":"other?","criteria":{"c":"","d":""}}}}`)
	if len(f.completions) != n+2 || f.completions[n].NPredict != 0 {
		t.Fatalf("after a cache miss the prefix must be warmed again, got %+v", f.completions[n:])
	}
	// state-first 配置では warm-up しない(接頭辞に state が含まれる)
	f2 := &fakeUpstream{top: []tokenProb{{"A", 1}}}
	s2, _ := newTestServer(t, f2)
	s2.cfg.StateFirst = true
	ask(t, s2, fmt.Sprintf(q, "first"))
	if len(f2.completions) != 1 {
		t.Fatalf("state-first layout must not warm up, got %+v", f2.completions)
	}
}

// 配置(layout)は要求ごとに選べる。plain は system 無し・選択肢を "- id: 説明"・入力を Input: の後ろに置き、
// 文字(A, B, …)ではなく選択肢 id の最初のトークンの確率を読む。応答の model には配置が付く(較正の鍵が分かれる)。
func TestPlainLayoutReadsOptionIdsAndNamesTheLayout(t *testing.T) {
	f := &fakeUpstream{top: []tokenProb{{"al", 0.7}, {"ze", 0.2}, {"mi", 0.05}, {"The", 0.05}}} // 偽トークナイザ: 最初のトークンは先頭 2 文字
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
	// 既定の配置の名前は今までどおり
	code, out = ask(t, s, `{"state":"x","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A","b":"B"}}}}`)
	if code != 200 || out["model"] != "fake-gguf" {
		t.Fatalf("default layout keeps the configured name: %d %v", code, out["model"])
	}
	// 知らない配置は 422。最初のトークンが重なる id も 422(読み分けられない)
	if code, _ := ask(t, s, `{"state":"x","layout":"sideways","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"a":"A","b":"B"}}}}`); code != 422 {
		t.Fatalf("unknown layout must be 422, got %d", code)
	}
	if code, out := ask(t, s, `{"state":"x","layout":"plain","questions":{"q":{"type":"choice","instructions":"what?","criteria":{"change_request":"A","change_x":"B"}}}}`); code != 422 || !strings.Contains(fmt.Sprint(out), "first token") {
		t.Fatalf("ids sharing a first token cannot be read under plain layout: %d %v", code, out)
	}
}
