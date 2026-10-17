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

	"github.com/hayaenfj/shikii/internal/spec"
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
	url, _, _ := newServeFixture(t, "local", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
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
		t.Fatalf("queue item must carry the stored text (pii: local): %v", item)
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
	url, specPath, _ := newServeFixture(t, "local", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
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
	url, _, _ := newServeFixture(t, "local", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
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

// 画面は 1 枚の HTML で、外部の読み込み無しに動く(オフラインの社内でも開ける)。
func TestServeUIIsSelfContained(t *testing.T) {
	url, _, _ := newServeFixture(t, "local", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
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
	for _, must := range []string{"/v1/queue", "/v1/label", "/v1/status"} {
		if !strings.Contains(html, must) {
			t.Fatalf("UI must talk to %s", must)
		}
	}
	for _, never := range []string{"https://", "http://", "<script src"} {
		if strings.Contains(html, never) {
			t.Fatalf("UI must not load anything from outside: found %q", never)
		}
	}
}

// 画面は言語を選べる(英語と日本語。辞書を足せば増える)。選択はブラウザに残る。
func TestServeUIHasLanguageSwitch(t *testing.T) {
	url, _, _ := newServeFixture(t, "local", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	html := buf.String()
	for _, must := range []string{`id="lang"`, `value="en"`, `value="ja"`, "答え合わせ待ちはありません", "nothing waiting for an answer", "localStorage"} {
		if !strings.Contains(html, must) {
			t.Fatalf("UI must offer a language switch with an English and a Japanese dictionary; missing %q", must)
		}
	}
}

// 正解がまだ無い spec でも status の keys は空配列(null だと画面の JS が落ちる)。
func TestServeStatusNeverReturnsNullLists(t *testing.T) {
	url, _, _ := newServeFixture(t, "local", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	resp, err := http.Get(url + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if strings.Contains(buf.String(), "null") {
		t.Fatalf("status must use empty lists, never null:\n%s", buf.String())
	}
}

// 同じ id の spec が 2 つあれば version の大きい方を使う(文言 v1 → v2 を同じ id で持つ運用のため)。同じ version なら誤り。
func TestLoadSpecsPicksHighestVersionForDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	v1 := writeSpec(t, dir, "never")
	b, _ := os.ReadFile(v1)
	v2 := strings.Replace(strings.Replace(string(b), "version: 1", "version: 2", 1), "question: What is asked?", "question: What is asked, exactly?", 1)
	if err := os.WriteFile(filepath.Join(dir, "intent-v2.yaml"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := loadSpecs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sp := specs["intent"]; sp == nil || sp.Version != 2 || !strings.Contains(sp.Question, "exactly") {
		t.Fatalf("duplicate id must resolve to the higher version: %+v", specs["intent"])
	}
	// 同じ version で id が重なるのは誤り
	if err := os.WriteFile(filepath.Join(dir, "intent-dup.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSpecs(dir); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("same id and same version must be an error, got %v", err)
	}
}
