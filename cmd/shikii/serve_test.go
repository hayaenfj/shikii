package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// The page lets you choose a language (English and Japanese; adding a dictionary adds more). The choice is kept in the browser (shikii.lang).
func TestServeUIHasLanguageSwitch(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := uiText(t, url)
	for _, must := range []string{`id="lang"`, "shikii.lang", "あなたを待っている物はありません", "nothing waiting for an answer", "localStorage"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must offer a language switch with an English and a Japanese dictionary; missing %q", must)
		}
	}
}

// The page is distributed as a Svelte bundle (web/dist) embedded in the binary: "/" is index.html, the assets it loads are 200 with the right type,
// and unknown paths are 404 (no catch-all that turns everything into index.html). There is a language choice (stored in shikii.lang; the option names are each dictionary's langName: 「日本語」「English」).
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
	for _, must := range []string{`id="lang"`, "shikii.lang", "日本語", "English"} {
		if !strings.Contains(body, must) {
			t.Fatalf("the language picker must be in the bundle; missing %q", must)
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
