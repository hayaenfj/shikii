package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// serve のテスト用: spec フォルダ・store・偽モデルを揃えて、ハンドラを httptest で立てる。
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

// 1 つの口で、聞く → 待ち行列に出る → ボタン相当の label → 行列から消える、が回る。
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

// 正解が min_n に届いたら、サーバーが自分で線を引いて spec に書く。人は calibrate を打たない。
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
	// 線が引けた後の判定は act になる
	_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix z"}})
	if d["band"] != "act" {
		t.Fatalf("after auto-calibration the band must reach act, got %v", d["band"])
	}
}

// 知らない spec は 404、spec に無い正解は 400。黙って通さない。
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

// 画面は外部の読み込み無しに動く(オフラインの社内でも開ける): index.html が読むのは自分の assets だけで、API は同じ口の /v1/。
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
	body := uiText(t, url) // 参照が ./ 以外なら uiText が落とす
	for _, must := range []string{"v1/queue", "v1/label", "v1/status"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must talk to %s", must)
		}
	}
}

// 画面は言語を選べる(英語と日本語。辞書を足せば増える)。選択はブラウザに残る(shikii.lang)。
func TestServeUIHasLanguageSwitch(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := uiText(t, url)
	for _, must := range []string{`id="lang"`, "shikii.lang", "答え合わせ待ちはありません", "nothing waiting for an answer", "localStorage"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must offer a language switch with an English and a Japanese dictionary; missing %q", must)
		}
	}
}

// 画面は Svelte で組んだバンドル(web/dist)を実行ファイルに埋め込んで配る: "/" が index.html、そこから読む assets は 200 で正しい種類、
// 知らないパスは 404(全部を index.html にするような取りこぼしは無い)。言語の選択がある(保存先 shikii.lang。選択肢の名前は各言語の辞書の langName:「日本語」「English」)。
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

// spec に display.ja があれば、画面向けの API は題名と選択肢の日本語を一緒に返す(id と英語はそのまま)。
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

// 本文が残っていない(本文を残す前の古い)記録は、人には答えられない。待ち行列はそれを answerable=false で言い、ボタンの材料(choices)を出さない。
func TestServeQueueMarksItemsWithoutTextAsUnanswerable(t *testing.T) {
	url, specPath, storeDir := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	old := store.Record{TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Model: "m",
		InputsHash: store.HashInputs(map[string]string{"message": "fix"}), Raw: map[string]float64{"change_request": 0.9},
		Top: "change_request", P: 0.9, Band: "ask", At: time.Now()} // Inputs 無し
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

// IT に詳しくない人のための画面: 既定は「かんたん表示」で、主張・言い換えの案・ハッシュ・モデル名は「くわしく」に隠れる。
// 判定ごとに人の言葉の質問、AI の予想と自信、「AI と同じ」ボタン、キー操作、学習の進み具合が出る。専門用語は画面に出さない。
func TestServeUIIsBeginnerFriendly(t *testing.T) {
	url, specPath, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	b, _ := os.ReadFile(specPath)
	src := string(b) + "display:\n  ja:\n    title: 依頼の種類\n    question: この依頼は何を求めていますか？\n"
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	body := uiText(t, url)
	for _, must := range []string{`id="mode"`, "かんたん表示", "くわしく", "AI の予想", "AI と同じ", "1〜9", "まだ学習中", "class=\"adv\"", "shikii.mode"} {
		if !strings.Contains(body, must) {
			t.Fatalf("beginner view missing %q", must)
		}
	}
	for _, never := range []string{"'線あり (", "'文言が古い'", "'holdout ' + h"} {
		if strings.Contains(body, never) {
			t.Fatalf("jargon must not appear in the page: %q", never)
		}
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

// 件数が増えた時の見せ方: 判定ごとのタブ(待ち件数の印)、1 件ずつ / 一覧、種類の数で絞り込み、スキップ、ほかに待っている数。知識のタブは「くわしく」の時だけ。
func TestServeUIHasTabsAndScalesToManyItems(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := uiText(t, url)
	for _, must := range []string{"#tabs {", "shikii.tab", "1 件ずつ", "一覧", "スキップ(あとで)", "ほかに ", "AI が迷った", "抜き取り", "さらに表示", "data-tab=\"wiki\"", "limit=200", "ArrowRight"} {
		if !strings.Contains(body, must) {
			t.Fatalf("scalable view missing %q", must)
		}
	}
}

// 待ち受け先は決め打ち: ホストでは 127.0.0.1 だけ(LAN に出さない)。容れ物(/.dockerenv がある)の中では全ての口(port の公開はホスト側が 127.0.0.1 に絞る)。
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

// 使い方の案内: 最初に 4 手順の案内が出て、閉じれば次回から出ない(右上の ? で再表示)。カードの上に「今やること」、ボタンに番号(キーと同じ)。
func TestServeUIExplainsItself(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := uiText(t, url)
	for _, must := range []string{`id="help"`, "この画面の使い方", "本当はどれだったかのボタンを押します", "わかった", "shikii.help", "本文を読んで、本当はどれだったかを押してください", `class="num"`} {
		if !strings.Contains(body, must) {
			t.Fatalf("the page must explain itself; missing %q", must)
		}
	}
}

func bindHostFor(marker string) string { return systemone.BindHostFor([]string{marker}, "") }
