package main

import (
	"bytes"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

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
