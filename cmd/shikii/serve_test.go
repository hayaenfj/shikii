package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// For serve tests: set up a spec folder, a store and a fake model, and stand up the handler with httptest.
func newServeFixture(t *testing.T, pii string, probs map[string]float64) (srvURL string, specPath string, storeDir string) {
	t.Helper()
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath = writeSpec(t, specsDir, pii) // id: intent, min_n: 2
	storeDir = filepath.Join(dir, ".shikii")
	model := fakeSystemOne(t, "m", probs)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: storeDir, ef: ef, autoCalibrate: true})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL, specPath, storeDir
}

func postJSON(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func getJSON(t *testing.T, url string, into any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode
}

// On one endpoint: ask → it appears in the queue → a label like a button press → it leaves the queue.
func TestServeDecideQueueAndLabel(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 || d["band"] != "ask" || d["trace_id"] == "" {
		t.Fatalf("decide: %d %v", code, d)
	}
	trace := d["trace_id"].(string)

	var q struct {
		Items []map[string]any `json:"items"`
	}
	if getJSON(t, url+"/v1/queue?spec=intent", &q) != 200 || len(q.Items) != 1 {
		t.Fatalf("queue must list the unlabeled decision: %+v", q)
	}
	item := q.Items[0]
	if item["trace_id"] != trace || item["inputs"].(map[string]any)["message"] != "fix the typo" {
		t.Fatalf("queue item must carry the stored text (pii: true): %v", item)
	}
	choices, _ := item["choices"].([]any)
	if len(choices) != 3 {
		t.Fatalf("queue item must offer the spec's options as choices: %v", item)
	}

	code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": trace, "truth": "change_request", "source": "ui"})
	if code != 200 || l["truth"] != "change_request" {
		t.Fatalf("label: %d %v", code, l)
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 0 {
		t.Fatalf("labeled decision must leave the queue: %+v", q)
	}
	var st map[string]any
	getJSON(t, url+"/v1/status", &st)
	specs := st["specs"].([]any)
	if len(specs) != 1 || specs[0].(map[string]any)["labels"].(float64) != 1 {
		t.Fatalf("status must count the label: %v", st)
	}
}

// When truths reach min_n, the server draws the line itself and writes it into the spec. The person never types calibrate.
func TestServeAutoCalibratesWhenEnoughLabels(t *testing.T) {
	url, specPath, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	for i := 0; i < 2; i++ {
		_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix " + string(rune('a'+i))}})
		code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request"})
		if code != 200 {
			t.Fatalf("label %d: %v", i, l)
		}
		if i == 0 && l["calibrated"] == true {
			t.Fatal("must not calibrate below min_n")
		}
		if i == 1 && l["calibrated"] != true {
			t.Fatalf("second label reaches min_n=2: server must calibrate and say so: %v", l)
		}
	}
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if c := sp.CalibrationFor("m"); c == nil || c.Method != "identity" || c.SpecHash != sp.Hash() {
		t.Fatalf("spec must now carry a calibration for model m: %+v", sp.Calibration)
	}
	// After the line is drawn, decisions are act
	_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix z"}})
	if d["band"] != "act" {
		t.Fatalf("after auto-calibration the band must reach act, got %v", d["band"])
	}
}

// An unknown spec is 404, a truth not in the spec is 400. Nothing is let through silently.
func TestServeRejectsUnknownSpecAndBadTruth(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	if code, _ := postJSON(t, url+"/v1/decide", map[string]any{"spec": "nope", "inputs": map[string]string{"message": "x"}}); code != 404 {
		t.Fatalf("unknown spec must be 404, got %d", code)
	}
	_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "x"}})
	if code, _ := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": "banana"}); code != 400 {
		t.Fatalf("truth outside the spec must be 400, got %d", code)
	}
	if code, _ := postJSON(t, url+"/v1/label", map[string]any{"trace_id": "0000000000000000", "truth": "question"}); code != 404 {
		t.Fatalf("unknown trace must be 404, got %d", code)
	}
}

// The page works without external loads (it opens even inside an offline company network): index.html loads only its own assets, and the API is /v1/ on the same endpoint.
func TestServeUIIsSelfContained(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	html := buf.String()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET / must serve HTML: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, never := range []string{"https://", "http://"} {
		if strings.Contains(html, never) {
			t.Fatalf("UI must not load anything from outside: found %q", never)
		}
	}
	body := uiText(t, url) // uiText fails if a reference is not ./
	for _, must := range []string{"v1/queue", "v1/label", "v1/status"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must talk to %s", must)
		}
	}
}

// The page is distributed as a Svelte bundle (web/dist) embedded in the binary: "/" is index.html, the assets it loads are 200 with the right type,
// and unknown paths are 404 (no catch-all that turns everything into index.html). There is a language choice (stored in shikii.lang; the option names are each dictionary's langName: 「日本語」「English」).
// The choice is kept in the browser, and there is an English and a Japanese dictionary (adding a dictionary adds more languages).
func TestServeUIServesEmbeddedBundleWithLanguagePicker(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	index := getBody(t, url+"/")
	refs := uiAssetRef.FindAllStringSubmatch(index, -1)
	if len(refs) < 2 {
		t.Fatalf("index.html must load its script and stylesheet from assets/: %s", index)
	}
	for _, m := range refs {
		ref := m[1]
		if !strings.HasPrefix(ref, "./assets/") {
			t.Fatalf("assets must be served from ./assets/, found %q", ref)
		}
		resp, err := http.Get(url + ref[1:])
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if resp.StatusCode != 200 {
			t.Fatalf("asset %s must be served, got %d", ref, resp.StatusCode)
		}
		if (strings.HasSuffix(ref, ".js") && !strings.Contains(ct, "javascript")) || (strings.HasSuffix(ref, ".css") && !strings.Contains(ct, "text/css")) {
			t.Fatalf("asset %s has the wrong content type %q", ref, ct)
		}
	}
	for _, p := range []string{"/nope", "/assets/nope.js", "/web/dist/index.html", "/assets/"} {
		resp, err := http.Get(url + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("unknown path %s must be 404, got %d", p, resp.StatusCode)
		}
	}
	body := uiText(t, url)
	for _, tc := range []struct {
		what string
		must []string
	}{
		{"the picker, kept in the browser", []string{`id="lang"`, "shikii.lang", "localStorage"}},
		{"each dictionary's langName", []string{"日本語", "English"}},
		{"an English and a Japanese dictionary", []string{"あなたを待っている物はありません", "nothing waiting for an answer"}},
	} {
		for _, must := range tc.must {
			if !strings.Contains(body, must) {
				t.Fatalf("the language picker must be in the bundle (%s); missing %q", tc.what, must)
			}
		}
	}
}

// If the spec has display.ja, the page's API returns the Japanese title and choices along with them (id and English unchanged).
func TestServeExposesDisplayTranslations(t *testing.T) {
	url, specPath, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	b, _ := os.ReadFile(specPath)
	withDisplay := string(b) + `display:
  ja:
    title: 依頼の種類
    options:
      change_request: 変更の依頼
      question: 質問
      other: その他
`
	if err := os.WriteFile(specPath, []byte(withDisplay), 0o644); err != nil {
		t.Fatal(err)
	}
	var specs struct {
		Specs []map[string]any `json:"specs"`
	}
	getJSON(t, url+"/v1/specs", &specs)
	if len(specs.Specs) != 1 || specs.Specs[0]["titles"].(map[string]any)["ja"] != "依頼の種類" {
		t.Fatalf("specs must carry titles per language: %v", specs.Specs)
	}
	postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix"}})
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	choices := q.Items[0]["choices"].([]any)
	first := choices[0].(map[string]any)
	if first["truth"] != "change_request" || first["labels"].(map[string]any)["ja"] != "変更の依頼" {
		t.Fatalf("choices must carry per-language labels: %v", first)
	}
}

// An (old) record whose text is not kept (from before text was kept) cannot be answered by a person. The queue says so with answerable=false and gives no button material (choices).
func TestServeQueueMarksItemsWithoutTextAsUnanswerable(t *testing.T) {
	url, specPath, storeDir := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	old := store.Record{TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Model: "m",
		InputsHash: store.HashInputs(map[string]string{"message": "fix"}), Raw: map[string]float64{"change_request": 0.9},
		Top: "change_request", P: 0.9, Band: "ask", At: time.Now()} // no Inputs
	if err := (store.Store{Dir: storeDir}).AppendRecord(old); err != nil {
		t.Fatal(err)
	}
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 1 {
		t.Fatalf("queue: %+v", q)
	}
	it := q.Items[0]
	if it["answerable"] != false || len(it["choices"].([]any)) != 0 || it["inputs"] != nil {
		t.Fatalf("without text the item must be marked unanswerable and carry no choices: %v", it)
	}
}

// A page for people who are not into IT: ask in people's words, show no jargon. The "simple / detailed" switch was removed in 0.8.1; everything is always shown (no switch button, saved preference or hiding mark).
// Each decision shows the question in people's words, the AI's guess and confidence, a "same as the AI" button, key operations and learning progress. No technical terms on the page.
func TestServeUIIsBeginnerFriendly(t *testing.T) {
	url, specPath, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	b, _ := os.ReadFile(specPath)
	src := string(b) + "display:\n  ja:\n    title: 依頼の種類\n    question: この依頼は何を求めていますか？\n"
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	body := uiText(t, url)
	for _, must := range []string{"AI の予想", "AI と同じ", "1〜9", "まだ学習中", "data-page=\"wiki\""} {
		if !strings.Contains(body, must) {
			t.Fatalf("beginner view missing %q", must)
		}
	}
	for _, never := range []string{"'線あり (", "'文言が古い'", "'holdout ' + h", `id="mode"`, "かんたん表示", "shikii.mode", "body.advanced", "AI の仮採点を抜き打ちで確認", "抜き取り", "較正", "答え合わせ", "spot check"} {
		if strings.Contains(body, never) {
			t.Fatalf("jargon must not appear in the page: %q", never)
		}
	}
	// The hiding mark adv remains nowhere as a class word (also catches forms side by side with other words like `class="k adv"`. A plain text match in never let it through: audit C-3)
	hiddenMark := regexp.MustCompile(`class="[^"]*\badv\b[^"]*"`)
	if !hiddenMark.MatchString(`<span class="k adv">`) {
		t.Fatal("the hidden-mark check must catch adv among other classes")
	}
	if m := hiddenMark.FindString(body); m != "" {
		t.Fatalf("the hidden mark adv must not remain: %s", m)
	}
	var st struct {
		Specs []struct {
			Questions map[string]string `json:"questions"`
			Question  string            `json:"question"`
		} `json:"specs"`
	}
	getJSON(t, url+"/v1/status", &st)
	if len(st.Specs) != 1 || st.Specs[0].Questions["ja"] != "この依頼は何を求めていますか？" || st.Specs[0].Question == "" {
		t.Fatalf("status must carry the question in people's words: %+v", st.Specs)
	}
}

// How it looks when items grow (0.9.1): the top has only 3: "answer (mark with the total waiting), knowledge, how the AI is doing". The answer page shows every question's waiting items one at a time (skip, count left),
// and how the AI is doing is a column per question (show more, number waiting elsewhere). The per-question menu on the left, the one-at-a-time / list switch and the kind filter from up to 0.8 are gone
func TestServeUIHasTabsAndScalesToManyItems(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := uiText(t, url)
	for _, must := range []string{`id="tabs"`, `data-page="answer"`, `data-page="wiki"`, `data-page="ai"`, "shikii.page", "shikii.tab", "スキップ(あとで)", "残り ", "ほかに ", "さらに表示", "limit=200", "ArrowRight"} {
		if !strings.Contains(body, must) {
			t.Fatalf("scalable view missing %q", must)
		}
	}
	for _, never := range []string{"'1 件ずつ'", "queue-filters", "view-switch", "chips projects", "summary-line"} {
		if strings.Contains(body, never) {
			t.Fatalf("the 0.8 navigation must be gone: %q", never)
		}
	}
}

// Where it listens is fixed: on a host, only 127.0.0.1 (never exposed to the LAN). Inside a container (where /.dockerenv exists), every interface (the host side limits port publishing to 127.0.0.1).
func TestBindHostIsLoopbackExceptInsideAContainer(t *testing.T) {
	if got := bindHostFor(filepath.Join(t.TempDir(), "no-such-file")); got != "127.0.0.1" {
		t.Fatalf("on a host shikii must listen on loopback only, got %q", got)
	}
	marker := filepath.Join(t.TempDir(), ".dockerenv")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := bindHostFor(marker); got != "0.0.0.0" {
		t.Fatalf("inside a container the port must be reachable from the host, got %q", got)
	}
}

// Usage guide: a 4-step guide shows first, and once closed it does not show next time (shown again with ? at the top right). Above the answer page is "what to do now", and buttons have numbers (the same as the keys).
func TestServeUIExplainsItself(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := uiText(t, url)
	for _, must := range []string{`id="help"`, "この画面の使い方", "本当はどれだったかのボタンを押します", "わかった", "shikii.help", "会話を読んで、本当はどうだったかを押してください", "data-num"} {
		if !strings.Contains(body, must) {
			t.Fatalf("the page must explain itself; missing %q", must)
		}
	}
}

func bindHostFor(marker string) string { return systemone.BindHostFor([]string{marker}, "") }

func TestHTTPServerLimitsSlowConnections(t *testing.T) {
	srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 || srv.MaxHeaderBytes <= 0 {
		t.Fatalf("the HTTP server must bound headers, reads, writes, idle connections and header size: %+v", srv)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv.ReadHeaderTimeout = 50 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	defer srv.Close()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nX-Slow:"); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != io.EOF {
		t.Fatalf("an incomplete header must be closed after ReadHeaderTimeout, got %v", err)
	}
}

// The page has a "wording" section: read results, run tune, and a person checks meaning and adopts / rejects. Both languages.
func TestServeUIHasWordingSection(t *testing.T) {
	url, _, _, _ := newTuneFixture(t)
	html := uiText(t, url)
	for _, must := range []string{"v1/tune?spec=", "v1/tune/run", "v1/tune/adopt", "meaning_same", "意味は同じ", "意味が違う", "same meaning", "meaning changed", "v1/tune/variant", "言い換えの案を書く", "write a rewording"} {
		if !strings.Contains(html, must) {
			t.Fatalf("UI must have the wording section; missing %q", must)
		}
	}
}

// Audit 5 item 8: serve's startup waits for the model's endpoint when there is --local, and does not wait without it (hosted only). A failure to wait is a startup failure.
func TestServeWaitsForTheLocalModelOnlyWhenThereIsOne(t *testing.T) {
	var asked []string
	fake := func(ctx context.Context, url string, every time.Duration, logf func(string, ...any)) error {
		asked = append(asked, url)
		if dl, ok := ctx.Deadline(); !ok || time.Until(dl) < 14*time.Minute {
			t.Fatalf("the wait must have a deadline of about 15 minutes")
		}
		if url == "http://127.0.0.1:1" {
			return errors.New("not ready")
		}
		return nil
	}
	if err := waitForLocalModel("", false, fake); err != nil || len(asked) != 0 {
		t.Fatalf("hosted only: nothing to wait for, got %v %v", err, asked)
	}
	// When there is a key and Jev is used, it does not wait even with --local (the rule is not to start a local model, so waiting would keep the page from opening; 2026-10-23)
	if err := waitForLocalModel("http://127.0.0.1:1", true, fake); err != nil || len(asked) != 0 {
		t.Fatalf("with a key the page must open without a local model: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:8080", false, fake); err != nil || len(asked) != 1 || asked[0] != "http://127.0.0.1:8080" {
		t.Fatalf("a local model is waited for at its URL: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:1", false, fake); err == nil {
		t.Fatalf("a model that never answers must fail the start")
	}
}

// Redoing past decisions (the owner's request, 2026-10-18): (1) a list of what was answered and correcting answers (the new answer wins),
// (2) asking the AI again: decide the same text again with the current wording and line, keeping a new record with redo_of. The original leaves the queue, and the person's answer is copied to the new record.
func TestPastDecisionsCanBeRedone(t *testing.T) {
	url, _, storeDir := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	trace := d["trace_id"].(string)
	// Redo a record not yet answered → the original leaves the queue and a new record appears
	code, res := postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": trace})
	if code != 200 || res["recorded"] != true || res["before"].(map[string]any)["trace_id"] != trace || res["after"].(map[string]any)["trace_id"] == trace {
		t.Fatalf("redo must decide again and record a new trace: %d %v", code, res)
	}
	newTrace := res["after"].(map[string]any)["trace_id"].(string)
	var q struct{ Items []map[string]any }
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	ids := map[string]bool{}
	for _, it := range q.Items {
		ids[it["trace_id"].(string)] = true
	}
	if ids[trace] || !ids[newTrace] {
		t.Fatalf("the redone record replaces the original in the queue: %v", ids)
	}
	// A person answers → it appears in answered. Correcting the answer uses the new answer
	postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": newTrace, "truth": "question"})
	var a struct{ Items []map[string]any }
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["truth"] != "question" || a.Items[0]["correct"] != false || a.Items[0]["redo_of"] != trace {
		t.Fatalf("answered list shows the person's answer and whether the AI matched: %v", a.Items)
	}
	postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": newTrace, "truth": "change_request"})
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["truth"] != "change_request" || a.Items[0]["correct"] != true {
		t.Fatalf("the newest answer wins: %v", a.Items)
	}
	// Redo an answered record → the person's answer is copied to the new record, and right/wrong before and now are returned
	code, res = postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": newTrace})
	if code != 200 || res["truth"] != "change_request" || res["correct_before"] != true || res["correct_after"] != true {
		t.Fatalf("redo of an answered decision carries the answer and compares: %d %v", code, res)
	}
	// Audit 6 item 4: answered shows 1 item (with redo_of), not the original, and the answered count stays at the number of texts a person answered = 1
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["redo_of"] != newTrace {
		t.Fatalf("the redone record replaces the original in the answered list too: %v", a.Items)
	}
	var stat struct{ Specs []map[string]any }
	getJSON(t, url+"/v1/status", &stat)
	if stat.Specs[0]["labels"] != 1.0 {
		t.Fatalf("answered count is the number of texts a person answered (1), got %v", stat.Specs[0]["labels"])
	}
	// Audit 6 item 8a: limit (answer 2, then narrow to 1)
	_, d2 := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "why is it slow?"}})
	postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": d2["trace_id"], "truth": "question"})
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 2 {
		t.Fatalf("two texts answered: %d", len(a.Items))
	}
	getJSON(t, url+"/v1/answered?spec=intent&limit=1", &a)
	if len(a.Items) != 1 {
		t.Fatalf("limit must cap the answered list: %d", len(a.Items))
	}
	st := store.Store{Dir: storeDir}
	labeled, _ := st.LabeledTraces("intent")
	if l, ok := labeled[res["after"].(map[string]any)["trace_id"].(string)]; !ok || l.Truth != "change_request" || !store.HumanSource(l.Source) {
		t.Fatalf("the person's answer must be carried to the new record as a human truth: %+v", l)
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 0 {
		t.Fatalf("a redone answered decision does not come back to the queue: %v", q.Items)
	}
	// A record without text cannot be redone
	if err := st.AppendRecord(store.Record{TraceID: "old", SpecID: "intent", SpecHash: "h", Type: "choice", Band: "ask", InputsHash: "x", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if code, _ := postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": "old"}); code != 409 {
		t.Fatalf("no text → 409, got %d", code)
	}
	// A record with only machine truths (context / judge) is not "answered"
	mach := store.Record{TraceID: "mach", SpecID: "intent", SpecHash: "h", Type: "choice", Band: "act", Top: "question", P: 0.9, Inputs: map[string]string{"message": "why?"}, InputsHash: "y", Model: "m", At: time.Now()}
	if err := st.AppendRecord(mach); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(store.Label{TraceID: "mach", SpecID: "intent", SpecHash: "h", Type: "choice", Model: "m", InputsHash: "y", Truth: "question", Weight: 0.9, Source: "judge", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	for _, it := range a.Items {
		if it["trace_id"] == "mach" {
			t.Fatalf("a machine truth is not a person's answer: %v", it)
		}
	}
	// The page has the parts
	body := uiText(t, url)
	for _, must := range []string{"v1/answered", "v1/redo", "回答済み", "AI にもう一度聞く", "別のボタンを押すと答えを直せます"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must offer the redo: missing %q", must)
		}
	}
}

// Audit 6 items 5 and 8c: a redo is recorded with the same manners as /v1/decide. If it reaches act on a context rung, a machine truth (source context) stays on the minimal rung,
// and the copy of the person's answer goes on 2 rungs (the context hash and the minimal hash).
func TestRedoRecordsLikeDecideOnTheContextRung(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	var ef endpointFlags
	ef.model, ef.local = "m", srv.URL
	ts := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(dir, "decisions"), storeDir: storeDir, ef: ef}))
	defer ts.Close()
	inputs := map[string]string{"message": "fix a", "previous": "I renamed the module"}
	code, d := postJSON(t, ts.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": inputs})
	if code != 200 || d["rung"] != "context" || d["band"] != "act" {
		t.Fatalf("fixture: context act expected, got %d %v", code, d)
	}
	if n := len(readLabels(t, storeDir, "intent")); n != 1 {
		t.Fatalf("decide on the context rung leaves one machine truth, got %d", n)
	}
	code, res := postJSON(t, ts.URL+"/v1/redo", map[string]any{"spec": "intent", "trace_id": d["trace_id"]})
	if code != 200 || res["recorded"] != true {
		t.Fatalf("redo: %d %v", code, res)
	}
	labels := readLabels(t, storeDir, "intent")
	if len(labels) != 2 || labels[1].Source != "context" || labels[1].Rung != "minimal" {
		t.Fatalf("redo must record like decide: a second context truth for the minimal rung, got %+v", labels)
	}
	// A person answers the original, then redoes it → the copy is on 2 rungs
	postJSON(t, ts.URL+"/v1/label", map[string]any{"spec": "intent", "trace_id": d["trace_id"], "truth": "question"})
	before := len(readLabels(t, storeDir, "intent"))
	postJSON(t, ts.URL+"/v1/redo", map[string]any{"spec": "intent", "trace_id": d["trace_id"]})
	after := readLabels(t, storeDir, "intent")
	copied := 0
	for _, l := range after[before:] {
		if l.Source == "ui" && l.Truth == "question" {
			copied++
		}
	}
	if copied != 2 {
		t.Fatalf("the person's answer is carried to both rungs of the new record (2 labels), got %d of %+v", copied, after[before:])
	}
}

// writeNamedNoulSpec writes a noul spec with the given id (min_n 4) into dir.
func writeNamedNoulSpec(t *testing.T, dir, id string) string {
	t.Helper()
	path := filepath.Join(dir, id+".yaml")
	src := "id: " + id + "\nversion: 1\ntype: noul\nquestion: Same case?\nstate:\n  template: \"{{ a }}\"\n  required: [a]\npolicy:\n  send_to: [local]\n  pii: false\nthresholds:\n  act: 0.70\n  ask: 0.50\neval:\n  min_n: 4\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 0.9.4 audit: the page's quality note says "the AI will try other wordings". Only the hook's questions get other wordings tried
// (variantsContext / hook variants submit accept hookSpecs only), so the result says whether that is so, and a one-sided set of truths
// is told as one kind of answer, not as "this wording cannot tell the answers apart".
func TestServeRefusalSaysWhetherWordingsAreTriedAndOneAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, id, want  string
		oneSided, tries bool
	}{
		{"own question, reversed", "own-check", "quality", false, false},
		{"hook question, reversed", "needs-plan", "quality", false, true},
		{"hook question, one-sided", "needs-plan", "one_answer", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			specsDir := filepath.Join(dir, "decisions")
			if err := os.MkdirAll(specsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeNamedNoulSpec(t, specsDir, tc.id)
			table := map[string]float64{}
			for i := 0; i < 4; i++ {
				table[fmt.Sprint(i)] = 0.9
				if i%2 == 0 {
					table[fmt.Sprint(i)] = 0.1
				}
			}
			model := fakeNoulByMessage(t, "m", table)
			var ef endpointFlags
			ef.model, ef.local = "m", model.URL
			srv := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true}))
			t.Cleanup(srv.Close)
			for i := 0; i < 4; i++ {
				_, d := postJSON(t, srv.URL+"/v1/decide", map[string]any{"spec": tc.id, "inputs": map[string]string{"a": fmt.Sprint(i)}})
				truth := "no"
				if i%2 == 0 || tc.oneSided {
					truth = "yes"
				}
				if code, l := postJSON(t, srv.URL+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": truth}); code != 200 {
					t.Fatalf("label: %d %v", code, l)
				}
			}
			var status struct {
				Specs []map[string]any `json:"specs"`
			}
			getJSON(t, srv.URL+"/v1/status", &status)
			result, _ := status.Specs[0]["calibration_result"].(map[string]any)
			if result["reason"] != tc.want || result["tries_wordings"] != tc.tries {
				t.Fatalf("want reason %s, tries_wordings %v: %v", tc.want, tc.tries, result)
			}
		})
	}
}

// 0.9.7 re-audit C-6: a page whose model answered first (the usual case), stopped by the check, returned "use of closed network connection".
func TestReauditCOwnerStopAfterModelReadyReturnsError(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	ticks := pageOwnerTicksByHand(t)
	up := pageListening(t)
	done := make(chan error, 1)
	go func() { // no --local: the model is ready at once
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", ""})
	}()
	select {
	case <-up:
	case err := <-done:
		t.Fatalf("precondition: the page started: %v", err)
	}
	if err := os.Remove(pagePath(common, repo)); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now() // the owner check runs now
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a page stopped by the check ends with an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("it does not stop")
	}
}

// 0.9.7 re-audit C-1: a page started by hand with --port 0 and a store not named .shikii was stopped by the minute check, although its record pointed at it.
// Only the hook's page (a project's .shikii on a free port) runs that check, so a serve started by hand is never stopped by it.
func TestReauditCHandServeWithOtherStoreNameIsNotStopped(t *testing.T) {
	for _, c := range []struct {
		hub   bool
		port  int
		store string
		want  bool
	}{
		{false, 0, "/work/p/.shikii", true},     // the hook's page
		{false, 0, "/data/other", false},        // by hand, a store under another name (C-1)
		{false, 8095, "/work/p/.shikii", false}, // by hand, a fixed port
		{true, 0, "/home/.shikii/agent", false}, // the hub
	} {
		if got := startedByHook(c.hub, c.port, c.store); got != c.want {
			t.Errorf("startedByHook(%v, %d, %s) = %v, want %v: only the hook's page is stopped by the owner check", c.hub, c.port, c.store, got, c.want)
		}
	}
}

// D1: a serve pointed at the same project with another case spelling (--port 0, store named .shikii) took another lock and kept running
// as a second page while the page the hook started held the lock (case-insensitive disk).
func TestAuditFinalD1OnePageAcrossCaseSpellings(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	initAt(t, repo)
	other := strings.ToUpper(repo)
	a, errA := os.Stat(repo)
	b, errB := os.Stat(other)
	if other == repo || errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Skip("on a case-sensitive disk the two spellings are not the same folder")
	}
	takePageLock(t, common, repo) // the page the hook started (the recorded spelling) is running
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(other, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		rec, ok := readPageRecord(common, other)
		t.Fatalf("D1: a second page of the same project (%s and %s are the same folder) keeps running (record under the other spelling %v: %+v)", repo, other, ok, rec)
	}
}

// E2: once the lock is released, a reply in flight is cut by the write timeout again - not kept for longResponseLimit.
// The test runs the reply's check by hand and moves its clock, so the write timeout after the release is already over when the handler writes.
func TestAuditE2DeadlineReturnsAfterRelease(t *testing.T) {
	tick := tickReplies(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	working, finish := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(working)
		<-finish // long work that does not use the lock
		io.WriteString(w, "done")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	release := s.lockCalibration() // a short calibration elsewhere
	got := fetchAsync(func() (*http.Response, error) { return http.Get(ts.URL) })
	<-working
	tick() // during the calibration the reply is kept alive
	release()
	back := pastReplyClock(t)
	tick() // the write timeout the reply gets back at this check is over before the handler writes
	back()
	close(finish)
	if r := <-got; r.err == nil && r.body == "done" {
		t.Fatal("E2: the lock was free past the write timeout, yet the reply was not cut: the 45-minute deadline set during the calibration stayed")
	}
}

// eaf2c9b: a slow reader must not hold a connection past the write timeout. While a calibration is active, it was held without bound:
// once the handler starts writing, the write timeout runs from then on, even though the calibration had extended the reply's deadline.
// The reader's slowness is the clock: when the handler writes, the write timeout from its first byte is already over.
func TestAuditSlowReaderHeldDuringCalibration(t *testing.T) {
	tick := tickReplies(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	working, finish := make(chan struct{}), make(chan struct{})
	wrote := make(chan error, 1)
	big := bytes.Repeat([]byte("x"), 1<<20)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(working)
		<-finish
		_, err := w.Write(big)
		wrote <- err
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	release := s.lockCalibration() // a background tune
	defer release()
	got := fetchAsync(func() (*http.Response, error) { return http.Get(ts.URL) })
	<-working
	tick() // the calibration extends the reply's deadline
	pastReplyClock(t)
	close(finish)
	if err := <-wrote; err == nil {
		t.Fatal("a reply that started writing during a calibration kept the calibration's deadline: a client that reads slowly holds it without bound")
	}
	<-got
}

// R5 (E): trackedWriter hides http.Flusher and io.ReaderFrom of the connection's writer.
func TestRe2TrackedWriterKeepsFlusher(t *testing.T) {
	var flusher, readerFrom bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flusher = w.(http.Flusher)
		_, readerFrom = w.(io.ReaderFrom)
		io.WriteString(w, "ok")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !flusher || !readerFrom {
		t.Fatalf("E: the writer handed to handlers lost http.Flusher=%v io.ReaderFrom=%v", flusher, readerFrom)
	}
}

// The page's queue: after the ask decisions come spot checks of act decisions (1 in 20). The remaining act / discard come after. Spot checks are marked.
func TestQueuePutsAuditSamplesRightAfterAskItems(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, specsDir, "false")
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	s := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: storeDir, ef: ef}))
	t.Cleanup(s.Close)

	// act records with fixed trace ids, 2 of them spot-check samples, written plain, sampled, plain, sampled, plain so the queue has to move
	// the samples forward; and 1 ask record. Fixed ids: the case is there on every run (random ids left it out about 1 run in 30,000).
	st := store.Store{Dir: storeDir}
	var samples, plains []string
	for i := 0; len(samples) < 2 || len(plains) < 3; i++ {
		id := fmt.Sprintf("%016x", i)
		if store.AuditSample(id) {
			if len(samples) < 2 {
				samples = append(samples, id)
			}
		} else if len(plains) < 3 {
			plains = append(plains, id)
		}
	}
	at := time.Now().Add(-time.Hour)
	for n, id := range []string{plains[0], samples[0], plains[1], samples[1], plains[2]} {
		in := map[string]string{"message": fmt.Sprintf("fix %d", n)}
		rec := store.Record{TraceID: id, SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", Inputs: in, InputsHash: store.HashInputs(in),
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Top: "change_request", P: 0.9, Band: "act", At: at.Add(time.Duration(n) * time.Minute)}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	sampled := len(samples)
	askRec := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", Inputs: map[string]string{"message": "hmm"},
		InputsHash: store.HashInputs(map[string]string{"message": "hmm"}), Raw: map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}, Top: "change_request", P: 0.65, Band: "ask", At: time.Now()}
	if err := st.AppendRecord(askRec); err != nil {
		t.Fatal(err)
	}
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, s.URL+"/v1/queue?spec=intent&limit=1000", &q)
	if len(q.Items) < 3 || q.Items[0]["band"] != "ask" || q.Items[0]["audit"] != false {
		t.Fatalf("ask first: %+v", q.Items[:3])
	}
	seenPlain := false
	nAudit := 0
	for _, it := range q.Items[1:] {
		audit := it["audit"] == true
		if audit != (it["band"] == "act" && store.AuditSample(it["trace_id"].(string))) {
			t.Fatalf("audit flag must be the 1-in-20 sample of act decisions: %+v", it)
		}
		if audit {
			nAudit++
			if seenPlain {
				t.Fatalf("audit samples must come before the unsampled act decisions: %+v", q.Items)
			}
		} else {
			seenPlain = true
		}
	}
	if nAudit != sampled {
		t.Fatalf("every sampled act decision is in the queue: %d vs %d", nAudit, sampled)
	}
	body := uiText(t, s.URL)
	if !strings.Contains(body, "shown to check") || !strings.Contains(body, "確かめのため") {
		t.Fatalf("the page must name the audit sample in both languages")
	}
}

// Audit S2/S3: serve refuses POSTs from other origins, non-JSON POSTs and bodies that are too large.
func TestServeRejectsCrossOriginWrongTypeAndOversizedPosts(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := `{"spec":"intent","inputs":{"message":"x"}}`
	post := func(ctype, origin string, b string) int {
		req, _ := http.NewRequest(http.MethodPost, url+"/v1/decide", strings.NewReader(b))
		req.Header.Set("Content-Type", ctype)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post("application/json", "https://evil.example", body); c != 403 {
		t.Fatalf("cross-origin POST must be 403, got %d", c)
	}
	if c := post("text/plain", "", body); c != 415 {
		t.Fatalf("non-JSON POST must be 415, got %d", c)
	}
	big := `{"spec":"intent","inputs":{"message":"` + strings.Repeat("x", 2<<20) + `"}}`
	if c := post("application/json", "", big); c != 413 {
		t.Fatalf("oversized POST must be 413, got %d", c)
	}
	if c := post("application/json", "http://127.0.0.1:8095", body); c != 200 {
		t.Fatalf("same-host origin must pass, got %d", c)
	}
	if c := post("application/json", "", body); c != 200 {
		t.Fatalf("no origin (curl, programs) must pass, got %d", c)
	}
}

func TestAutoCalibrationReportsQualityWithoutLosingAnswers(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			url, path, dir := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
			for i := 0; i < 2; i++ {
				_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": fmt.Sprint(i)}})
				truth := "change_request"
				if i == 1 && !success {
					truth = "question"
				}
				code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": truth})
				if code != 200 || (i == 1 && l["calibrated"] != success) {
					t.Fatalf("label: %v", l)
				}
			}
			var status struct {
				Specs []map[string]any `json:"specs"`
			}
			getJSON(t, url+"/v1/status", &status)
			s := status.Specs[0]
			result, ok := s["calibration_result"].(map[string]any)
			want := "quality"
			if success {
				want = "written"
			}
			if !ok || result["reason"] != want || result["human_truths"] != float64(2) || result["min_n"] != float64(2) {
				t.Fatalf("structured calibration result: %v", s)
			}
			if result["hash"] != s["hash"] || result["model"] != "m" || result["at"] == "" {
				t.Fatalf("calibration identity: %v", result)
			}
			if len(readLabels(t, dir, "intent")) != 2 || (len(mustLoad(t, path).Calibration) > 0) != success {
				t.Fatal("answers or calibration changed incorrectly")
			}
		})
	}
}

// serve: a decision with added context looks so on the page and is not old wording (stale). The person's answer goes on both rungs, and status shows counts per rung.
func TestServeShowsTheRungAndLabelsBothRungs(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	writeContextSpec(t, dir)
	storeDir := filepath.Join(dir, ".shikii")
	var ef endpointFlags
	ef.model, ef.local = "m", srv.URL
	h := newServer(serveOpts{specsDir: filepath.Join(dir, "decisions"), storeDir: storeDir, ef: ef})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)

	code, d := postJSON(t, s.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix a", "previous": "I renamed the module"}})
	if code != 200 || d["rung"] != "context" || d["minimal"] == nil {
		t.Fatalf("decide: %d %v", code, d)
	}
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, s.URL+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 1 || q.Items[0]["rung"] != "context" || q.Items[0]["stale"] != false {
		t.Fatalf("the queue must say the decision was asked again with context and is not stale: %+v", q.Items)
	}
	code, l := postJSON(t, s.URL+"/v1/label", map[string]any{"trace_id": d["trace_id"], "spec": "intent", "truth": "question", "source": "ui"})
	if code != 200 || l["rungs"] != 2.0 {
		t.Fatalf("label must reach both rungs: %d %v", code, l)
	}
	if labels := readLabels(t, storeDir, "intent"); len(labels) != 2 || labels[0].Rung != "context" || labels[1].Rung != "minimal" {
		t.Fatalf("labels: %+v", labels)
	}
	var st struct {
		Specs []struct {
			Keys   []map[string]any `json:"keys"`
			Funnel map[string]any   `json:"funnel"`
		} `json:"specs"`
	}
	getJSON(t, s.URL+"/v1/status", &st)
	if len(st.Specs) != 1 || len(st.Specs[0].Keys) != 2 || st.Specs[0].Keys[0]["rung"] != "minimal" || st.Specs[0].Keys[1]["rung"] != "context" || st.Specs[0].Keys[1]["state"] != "collecting" {
		t.Fatalf("status must list a key per rung, both live: %+v", st.Specs)
	}
	if f := st.Specs[0].Funnel; f["human"] != 1.0 || f["human_per_thousand"] != 1000.0 {
		t.Fatalf("funnel: %+v", f)
	}
}

// (5) When /v1/redo (and every endpoint that takes a spec) has no spec, it says "a spec is needed" and what to add, instead of `unknown spec ""`.
func TestMissingSpecIdSaysSpecIsRequired(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	for _, path := range []string{"/v1/redo", "/v1/decide", "/v1/tune/run"} {
		code, res := postJSON(t, url+path, map[string]any{"trace_id": d["trace_id"], "inputs": map[string]string{"message": "x"}})
		if code != 400 || !strings.Contains(fmt.Sprint(res["error"]), "spec is required") || !strings.Contains(fmt.Sprint(res["error"]), "decisions/") {
			t.Fatalf("%s without spec must say spec is required and where the id comes from: %d %v", path, code, res)
		}
	}
	code, res := postJSON(t, url+"/v1/redo", map[string]any{"spec": "nope", "trace_id": d["trace_id"]})
	if code != 404 || !strings.Contains(fmt.Sprint(res["error"]), `unknown spec "nope"`) {
		t.Fatalf("a wrong id is still unknown: %d %v", code, res)
	}
}

// The page (serve) does the same; a store without specs makes it return right after reading its flags, so no page is started.
func TestServeLocalReachesHookDecisions(t *testing.T) {
	oldLocal := hookLocalURL
	t.Cleanup(func() { hookLocalURL = oldLocal })
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	hookLocalURL = "http://127.0.0.1:8080"
	if err := cmdServe([]string{"--store", t.TempDir(), "--local", "http://127.0.0.1:47392"}); err == nil {
		t.Fatal("a store without specs is refused before a page starts")
	}
	if hookLocalURL != "http://127.0.0.1:47392" {
		t.Fatalf("serve's --local reaches the decisions made through hookDecide: %s", hookLocalURL)
	}
}

// 0.9.7: a second page of the same project exits while the first holds the page lock, even when the first answered /v1/status too slowly
// for the hook's probe (two pages per project were found running after the 0.9.6 swap). It neither listens nor writes the page record.
func TestSecondPageOfTheSameProjectExitsWhileTheFirstHoldsTheLock(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	takePageLock(t, common, repo)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a second page exits quietly: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a second page of the same project kept running while the first held the page lock")
	}
	if rec, ok := readPageRecord(common, repo); ok {
		t.Fatalf("the second page must not overwrite the first page's record: %+v", rec)
	}
	if len(opened) != 0 {
		t.Fatalf("the second page must not open the browser: %v", opened)
	}
}

// The page that holds the lock starts even when the old record's pid is alive (now another process): its own lock must not make that record look up.
func TestPageStartsWhenTheOldRecordsPidWasReused(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	if err := writePageRecord(common, repo, 1, os.Getpid()); err != nil { // a live pid, a port that does not answer
		t.Fatal(err)
	}
	ticks := pageOwnerTicksByHand(t)
	up := pageListening(t)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-up:
	case err := <-done:
		t.Fatalf("the page did not start: the old record with a reused pid looked up through the page's own lock (%v)", err)
	}
	if rec, ok := readPageRecord(common, repo); !ok || rec.Port == 1 {
		t.Fatalf("the page that started writes its own record: %+v", rec)
	}
	if err := os.Remove(pagePath(common, repo)); err != nil { // stop the page
		t.Fatal(err)
	}
	ticks <- time.Now()
	select {
	case <-done:
	case <-time.After(3 * time.Second): // only turns a page that never stops into a failure
		t.Fatal("the page does not stop once its record is gone")
	}
}

// 0.9.7 audit B-b: a page whose record no longer points at it (reconnecting a moved project removes the old path's record, uninstall too)
// or whose store is gone stops by itself instead of running on as an orphan.
func TestOrphanPageStopsByItself(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	ticks := pageOwnerTicksByHand(t)
	up := pageListening(t)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-up:
	case err := <-done:
		t.Fatalf("precondition: the page started: %v", err)
	}
	if err := os.Remove(pagePath(common, repo)); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now() // the owner check runs now
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a page whose record is gone keeps running")
	}
}

// 0.9.6 audit of the HTTP limits (eaf2c9b): the server cuts every response after WriteTimeout (2 minutes). While no calibration is active a
// slow reply is still cut (the limit stays); while a calibration holds or waits for the lock, replies in flight are kept alive (0.9.7).
func TestSlowRepliesAreCutUnlessACalibrationIsActive(t *testing.T) {
	tick := tickReplies(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	for _, active := range []bool{false, true} {
		working, finish := make(chan struct{}), make(chan struct{})
		slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			expireWrite(t, w) // the long work has run past the write timeout
			close(working)
			<-finish
			io.WriteString(w, "done")
		})
		ts := httptest.NewUnstartedServer(slow)
		ts.Config = newHTTPServer(slow)
		ts.Start()
		var release func()
		if active {
			release = s.lockCalibration()
		}
		got := fetchAsync(func() (*http.Response, error) { return http.Get(ts.URL) })
		<-working
		tick()
		if release != nil {
			release()
		}
		close(finish)
		r := <-got
		ts.Close()
		err, body := r.err, r.body
		if active && (err != nil || body != "done") {
			t.Fatalf("while a calibration is active the reply arrives after the write timeout: %v %q", err, body)
		}
		if !active && err == nil && body == "done" {
			t.Fatal("while no calibration is active the write timeout still cuts a slow reply")
		}
	}
}

// An answer that brings the truths to min_n calibrates in the same request, so it waits for the calibration lock. While a background tune holds
// the lock past the write timeout, that reply still arrives (the answer was saved; before 0.9.6 the page showed a connection error).
func TestAnswerThatCalibratesBehindATuneStillReplies(t *testing.T) {
	tick := tickReplies(t)
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "true") // min_n 2
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	s := newServerState(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true})
	h := s.handler()
	var behind atomic.Bool // the next answer waits behind the tune
	waiting, goOn := make(chan struct{}), make(chan struct{})
	late := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if behind.Load() {
			expireWrite(t, w) // waiting behind the tune has run past the write timeout
			close(waiting)
			<-goOn
		}
		h.ServeHTTP(w, r)
	})
	ts := httptest.NewUnstartedServer(late)
	ts.Config = newHTTPServer(late)
	ts.Start()
	defer ts.Close()
	label := func(trace any) (map[string]any, error) {
		resp, err := http.Post(ts.URL+"/v1/label", "application/json", strings.NewReader(fmt.Sprintf(`{"trace_id":%q,"truth":"change_request"}`, trace)))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var out map[string]any
		return out, json.NewDecoder(resp.Body).Decode(&out)
	}
	var traces []any
	for _, m := range []string{"x", "y"} {
		code, d := postJSON(t, ts.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": m}})
		if code != 200 {
			t.Fatalf("decide: %d %v", code, d)
		}
		traces = append(traces, d["trace_id"])
	}
	if _, err := label(traces[0]); err != nil {
		t.Fatal(err)
	}
	release := s.lockCalibration() // a background tune holds the lock
	behind.Store(true)
	type labeled struct {
		out map[string]any
		err error
	}
	got := make(chan labeled, 1)
	go func() { out, err := label(traces[1]); got <- labeled{out, err} }()
	<-waiting
	tick() // the reply's check sees the tune
	close(goOn)
	release()
	r := <-got
	out, err := r.out, r.err
	if err != nil {
		t.Fatalf("the answer's reply arrives after waiting for the lock past the write timeout: %v", err)
	}
	if out["calibrated"] != true {
		t.Fatalf("precondition: the second answer calibrates (so it waited for the lock): %v", out)
	}
}

// 0.8.0: decisions a person does not answer (wiki-conflict / wiki-place) are not counted as waiting for confirmation; only the number waiting for an outcome is shown. Hook decisions keep the old behavior, since they have spot checks for the person.
func TestStatusDoesNotCountMachineOnlySpecsAsWaitingForAPerson(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	for i := 0; i < 3; i++ {
		rec := store.Record{TraceID: fmt.Sprintf("wc%d", i), SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "a", "new_lesson": "b"}, InputsHash: fmt.Sprintf("h%d", i), Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	// "Unsure" in decisions whose truth comes from an outcome waits for the outcome; "unsure" in decisions a person answers waits for the person
	st.AppendRecord(store.Record{TraceID: "pk1", SpecID: "prompt-kind", Type: "choice", Model: "m", Inputs: map[string]string{"request": "x"}, InputsHash: "pk1", Raw: map[string]float64{"edit": .5, "explain": .5}, Top: "edit", Band: "ask", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "np1", SpecID: "needs-plan", Type: "noul", Model: "m", Inputs: map[string]string{"request": "x", "agreed": "a", "open": "o"}, InputsHash: "np1", Raw: map[string]float64{"yes": .5, "no": .5}, Top: "yes", Band: "ask", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "np2", SpecID: "needs-plan", Type: "noul", Model: "m", Inputs: map[string]string{"request": "y", "agreed": "a", "open": "o"}, InputsHash: "np2", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []map[string]any `json:"specs"`
		Model map[string]any   `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String()[:200])
	}
	if body.Model["name"] == nil {
		t.Fatalf("the AI's state: which model is deciding: %v", body.Model)
	}
	got := map[string]map[string]any{}
	for _, sp := range body.Specs {
		got[sp["id"].(string)] = sp
	}
	wc := got["wiki-conflict"]
	if wc == nil || wc["person_answers"] != false || wc["waiting_for_person"] != float64(0) || wc["awaiting_results"] != float64(3) || wc["unlabeled"] != float64(3) || wc["truth_source"] != "results" {
		t.Fatalf("decisions a person does not answer: 0 waiting for a person, 3 waiting for an outcome: %v", wc)
	}
	if pk := got["prompt-kind"]; pk["waiting_for_person"] != float64(0) || pk["awaiting_results"] != float64(1) {
		t.Fatalf("unsure decisions whose truth comes from an outcome wait for the outcome: %v", pk)
	}
	if np := got["needs-plan"]; np["waiting_for_person"] != float64(1) || np["person_answers"] != true || np["awaiting_results"] != float64(0) || np["settled_by_ai"] != float64(1) {
		t.Fatalf("unsure decisions a person answers wait for the person; act outside spot checks is not waiting for an outcome but decided by the AI: %v", np)
	}
	nq := httptest.NewRecorder()
	h.ServeHTTP(nq, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=needs-plan", nil))
	if !strings.Contains(nq.Body.String(), `"np1"`) || !strings.Contains(nq.Body.String(), `"np2"`) {
		t.Fatalf("the queue of questions a person gives truths to also shows act (the person can correct it; told apart by waits_for_person): %s", nq.Body.String()[:300])
	}
	q := httptest.NewRecorder()
	h.ServeHTTP(q, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=prompt-kind", nil))
	if !strings.Contains(strings.ReplaceAll(q.Body.String(), " ", ""), `"waits_for_person":false`) {
		t.Fatalf("queue items also say whether they wait for a person: %s", q.Body.String()[:200])
	}
}

// 0.8.0: for contradiction decisions, once the draft is saved (the insertion place got an outcome truth), the items for pages it did not touch are "undecided"
// and are neither counted as waiting for an outcome nor shown in the queue (even after saving, 61 items looked like they waited for an outcome forever). Before saving, they wait for an outcome.
func TestUndecidedConflictJudgmentsLeaveTheQueueOnceTheDraftIsSaved(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	draft := "新しい教訓の文"
	st.AppendRecord(store.Record{TraceID: "wc1", SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "既存の記事 A", "new_lesson": draft}, InputsHash: "h1", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "wp1", SpecID: "wiki-place", SpecHash: "sh", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "既存の記事 A", "new_lesson": draft}, InputsHash: "h2", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()})
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	status := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var body struct {
			Specs []map[string]any `json:"specs"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		for _, sp := range body.Specs {
			if sp["id"] == "wiki-conflict" {
				return sp
			}
		}
		return nil
	}
	if sp := status(); sp["awaiting_results"] != float64(1) || sp["undecided"] != float64(0) {
		t.Fatalf("before saving, waiting for an outcome: %v", sp)
	}
	// The draft was saved (the insertion-place decision got an outcome truth)
	labels, err := store.LabelsFor(&store.Record{TraceID: "wp1", SpecID: "wiki-place", SpecHash: "sh", Type: "noul", Model: "m", InputsHash: "h2", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no"}, "no", placeOtherWeight, "outcome")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	if sp := status(); sp["awaiting_results"] != float64(0) || sp["undecided"] != float64(1) || sp["unlabeled"] != float64(0) {
		t.Fatalf("after saving, undecided (settled) and not counted as waiting for an outcome: %v", sp)
	}
	q := httptest.NewRecorder()
	h.ServeHTTP(q, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=wiki-conflict", nil))
	if strings.Contains(q.Body.String(), `"wc1"`) {
		t.Fatalf("undecided records are not shown in the queue: %s", q.Body.String()[:200])
	}
}

// Before and after narrowing the count, the priority order, the exclusion of redo originals and the original text returned do not change.
func TestServeQueueLimitKeepsPriorityAndOriginals(t *testing.T) {
	url, path, dir := newServeFixture(t, "false", map[string]float64{"change_request": 1})
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: dir}
	ids := []string{"0000000000000001", "0000000000000002", "0000000000000003", "0000000000000004"}
	for i, id := range ids {
		inputs := map[string]string{"message": "fixture"}
		rec := store.Record{TraceID: id, SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Band: "ask", Inputs: inputs, InputsHash: store.HashInputs(inputs), At: time.Unix(int64(i), 0)}
		if i == 1 {
			rec.Band = "discard"
		}
		if i == 3 {
			rec.RedoOf = ids[2]
		}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveRecordText(rec, map[string]string{"message": "fixture original " + id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct {
		query string
		want  []string
	}{{"&limit=1", []string{ids[3]}}, {"&limit=2", []string{ids[3], ids[0]}}, {"", []string{ids[3], ids[0], ids[1]}}} {
		var result struct {
			Items []struct {
				TraceID   string            `json:"trace_id"`
				Originals map[string]string `json:"original_inputs"`
			} `json:"items"`
		}
		if code := getJSON(t, url+"/v1/queue?spec=intent"+check.query, &result); code != 200 || len(result.Items) != len(check.want) {
			t.Fatalf("queue %s: status=%d rows=%+v", check.query, code, result.Items)
		}
		for i, item := range result.Items {
			if item.TraceID != check.want[i] || item.Originals["message"] != "fixture original "+item.TraceID {
				t.Fatalf("queue %s row %d: got %+v, want %s with its original", check.query, i, item, check.want[i])
			}
		}
	}
}

// Audit 0.9.3 low 5: `shikii status` shows the same learning count as the page (keys[].n, lineStateOf's N) and one number per line.
// Machine truths (context / judge) and a re-label of the same input must not raise it; the page's AI view says it has "the same numbers as `shikii status`".
func TestStatusLineCountMatchesThePage(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	specPath := filepath.Join(root, "decisions", "needs-plan.yaml")
	sp := mustLoad(t, specPath)
	st := store.Store{Dir: root}
	lab := func(trace, inputs, truth, source string) store.Label {
		return store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: inputs, Truth: truth, Weight: 1, Source: source, At: time.Now()}
	}
	labels := []store.Label{lab("a", "a", "yes", "ui"), lab("b", "b", "no", "ask"), lab("c", "c", "no", "outcome"),
		lab("a", "a", "no", "ui"),                                     // a re-label of input a: still one truth
		lab("x", "x", "yes", "context"), lab("y", "y", "no", "judge")} // machine truths: not counted
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []struct {
			ID   string           `json:"id"`
			Keys []map[string]any `json:"keys"`
		} `json:"specs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	page := -1
	for _, s := range body.Specs {
		if s.ID == sp.ID && len(s.Keys) == 1 {
			page = int(s.Keys[0]["n"].(float64))
		}
	}
	if page != 3 {
		t.Fatalf("keys[].n is calibration's N: the latest truth per input from people and results (3), got %d: %s", page, rec.Body.String())
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", root, specPath}) })
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "m1 @ "+sp.Hash()) {
			line = l
		}
	}
	m := regexp.MustCompile(`^\s*m1 @ \S+\s+(\d+)(.*)$`).FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("no status line for m1:\n%s", out)
	}
	if m[1] != fmt.Sprint(page) {
		t.Fatalf("status says %s where the page says %d:\n%s", m[1], page, line)
	}
	if regexp.MustCompile(`\d+/\d+`).MatchString(m[2]) {
		t.Fatalf("the status line shows one count, not a second n/min_n beside it: %q", line)
	}
}

// outcomeFixture is a project whose changes-in-scope has n decisions an outcome answered "yes", and the ones OutcomeCheckSample picks.
func outcomeFixture(t *testing.T, n int) (root string, sampled []store.Record) {
	t.Helper()
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root = filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "changes-in-scope.yaml"))
	st := store.Store{Dir: root}
	for i := 0; i < n; i++ {
		inputs := map[string]string{"request": fmt.Sprint("change ", i), "files": "a.go"}
		r := store.Record{TraceID: fmt.Sprintf("%016x", i+1), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Inputs: inputs, InputsHash: store.HashInputs(inputs), Model: "m", Raw: map[string]float64{"yes": .6, "no": .4}, Top: "yes", P: .6, Band: "ask", At: time.Now()}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		labels, err := store.LabelsFor(&r, "yes", 0.5, "outcome")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
		if store.OutcomeCheckSample(r.TraceID) {
			sampled = append(sampled, r)
		}
	}
	return root, sampled
}

// 0.11: 1 in 10 decisions an outcome answered goes to the person while that outcome's agreement is unsettled, marked as an outcome
// check; the others stay out; once the person's answers settle it (here: clearly below act), none is asked any more.
func TestQueueAsksThePersonToCheckSomeOutcomeTruths(t *testing.T) {
	root, sampled := outcomeFixture(t, 60)
	if len(sampled) < 4 {
		t.Fatalf("the fixed sample picks some of 60: %d", len(sampled))
	}
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions")})
	queue := func() []map[string]any {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/queue?spec=changes-in-scope&limit=1000", nil))
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Items
	}
	items := queue()
	if len(items) != len(sampled) {
		t.Fatalf("only the sampled outcome truths are shown: %d of %d", len(items), len(sampled))
	}
	for _, it := range items {
		if it["review_reason"] != "outcome" || it["audit"] != true || it["waits_for_person"] != true {
			t.Fatalf("an outcome check waits for the person and says why: %v", it)
		}
	}
	st := store.Store{Dir: root}
	for _, r := range sampled[:3] { // the person says the outcome was wrong 3 times: 0 of 3 agree, the interval's upper end 0.56 < act 0.75
		labels, err := store.LabelsFor(&r, "no", 1, "audit")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	if items := queue(); len(items) != 0 {
		t.Fatalf("once settled, no more outcome checks are asked: %d", len(items))
	}
}

// prompt-kind's outcome truths are not checked by the person (0.11): the request alone cannot tell what the agent will do, so the
// question is left stopped and the person's time goes to the questions whose outcome rule is in doubt.
func TestQueueDoesNotAskToCheckPromptKindOutcomes(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "prompt-kind.yaml"))
	st := store.Store{Dir: root}
	sampled := 0
	for i := 0; i < 60; i++ {
		inputs := map[string]string{"request": fmt.Sprint("do ", i)}
		r := store.Record{TraceID: fmt.Sprintf("%016x", i+1), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Inputs: inputs, InputsHash: store.HashInputs(inputs), Model: "m", Raw: map[string]float64{"edit": .6, "explain": .4}, Top: "edit", P: .6, Band: "ask", At: time.Now()}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		labels, err := store.LabelsFor(&r, "edit", 1, "outcome")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
		if store.OutcomeCheckSample(r.TraceID) {
			sampled++
		}
	}
	if sampled == 0 {
		t.Fatal("the fixed sample picks some of 60")
	}
	r := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions")}).ServeHTTP(r, httptest.NewRequest("GET", "/v1/queue?spec=prompt-kind&limit=1000", nil))
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("no outcome check is asked for prompt-kind: %d", len(body.Items))
	}
}
