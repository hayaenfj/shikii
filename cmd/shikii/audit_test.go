package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// 2026-10-17 の監査で見つかった穴を塞ぐテスト。全部、直す前に落ちるのを確認してから直した。

// 監査 C1: identity と vector の採点が別のラベル集合(部分ラベルの有無)で行われ、calibrate が書いた直後に check が落ちた。
// 部分ラベル "!x" が外れを含むなら、vector の CV も同じ外れを数えなければならない。
func TestVectorCVScoresPartialLabelsLikeCheck(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false") // act 0.75, min_n 2
	for i := 0; i < 10; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=full %d", i)})
		})
		capture(t, func() error {
			return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
		})
	}
	for i := 0; i < 5; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=partial %d", i)})
		})
		capture(t, func() error {
			return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "!change_request"})
		})
	}
	// precision@act は 10/15 = 0.667 < 0.75: identity も vector も書いてはいけない
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m", "--write"})
	stdout = old
	if err == nil {
		t.Fatalf("calibrate must refuse to write when partial labels pull precision below act:\n%s", buf.String())
	}
	if sp := mustLoad(t, specPath); len(sp.Calibration) != 0 {
		t.Fatalf("nothing may be written: %+v", sp.Calibration)
	}
	if !strings.Contains(buf.String(), "vector 5-fold CV at act: precision=0.667") {
		t.Fatalf("vector CV must be scored on the same 15 labels (precision 0.667):\n%s", buf.String())
	}
}

// 監査 C2/C3: 同じ入力に複数の正解(ファイルの行と、後から人が付けたもの / 同じ trace の訂正)があると、
// calibrate と check で数え方が違った。どちらも「入力ごとに最新の正解 1 つ」で数える。
func TestCalibrateAndCheckUseLatestTruthPerInput(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	rows := `[{"inputs":{"message":"fix 0"},"truth":"change_request"},{"inputs":{"message":"fix 1"},"truth":"change_request"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	// 同じ本文 "fix 0" を実運用で判定し、人が「質問だった」と訂正する
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix 0"})
	})
	tr := traceRe.FindStringSubmatch(out)[1]
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "question"}) })
	// check: 最新の正解で "fix 0" は外れ → 1/2 = 0.5 < act → 落ちる
	if err := cmdCheck([]string{"--store", storeDir, specPath}); err == nil {
		t.Fatal("check must see the corrected truth and fail")
	}
	// calibrate も同じ集合(n=2、最新の正解)で見る。3 件に増えない
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	_ = cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	stdout = old
	if !strings.Contains(buf.String(), " n=2 ") || !strings.Contains(buf.String(), "top-1 accuracy=0.500") {
		t.Fatalf("calibrate must count one latest truth per input (n=2, accuracy 0.5):\n%s", buf.String())
	}
	// 同じ trace に 2 回付けても、数えるのは最新の 1 つ
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	buf.Reset()
	stdout = &buf
	_ = cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	stdout = old
	if !strings.Contains(buf.String(), " n=2 ") || !strings.Contains(buf.String(), "top-1 accuracy=1.000") {
		t.Fatalf("a corrected label must replace, not add:\n%s", buf.String())
	}
}

// 監査 S1: --tainted で記録した本文は、後の聞き直しでも hosted に送ってはいけない。
func TestTaintedTextIsNeverReplayedToHosted(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "k")
	var hostedCalls, localCalls atomic.Int32
	answer := func(model string, calls *atomic.Int32) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			var req systemone.Request
			_ = json.NewDecoder(r.Body).Decode(&req)
			resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
			for k := range req.Questions {
				resp.Answers[k] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}}
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(s.Close)
		return s
	}
	hosted := answer("m", &hostedCalls)
	local := answer("m", &localCalls)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	specPath := filepath.Join(dir, "intent.yaml")
	src := strings.Replace(strings.Replace(string(mustRead(t, writeSpec(t, dir, "false"))), "send_to: [local]", "send_to: [hosted, local]", 1), "min_n: 2", "min_n: 1", 1)
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--hosted", hosted.URL, "--local", local.URL, "--tainted", "--store", storeDir, "-i", "message=PROD ROW"})
	})
	if hostedCalls.Load() != 0 || localCalls.Load() != 1 {
		t.Fatalf("tainted decide must go local: hosted=%d local=%d", hostedCalls.Load(), localCalls.Load())
	}
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
	})
	edited := strings.Replace(src, "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// 聞き直し(tainted フラグ無しで呼んでも)は、tainted だった本文を hosted に送らない
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--hosted", hosted.URL, "--local", local.URL, "--store", storeDir, "--labeled", "none"})
	})
	if hostedCalls.Load() != 0 {
		t.Fatalf("replay sent tainted text to hosted (%d calls)", hostedCalls.Load())
	}
	if localCalls.Load() != 2 {
		t.Fatalf("replay must have asked local once more, got %d", localCalls.Load())
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 監査 S2/S3: serve は他オリジンからの POST、JSON 以外の POST、大きすぎる本文を受け付けない。
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

// 監査 S5: 手書きの identity ブロック(n が min_n 未満、fitted_at 無し)は check が弾く。
func TestCheckRejectsBlocksThatCalibrateWouldNotWrite(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false") // min_n 2
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
	})
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", PrecisionAt: 1, N: 1})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCheck([]string{"--store", storeDir, specPath})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "min_n") {
		t.Fatalf("check must reject n=1 < min_n=2 and say so: err=%v\n%s", err, buf.String())
	}
}

// 監査 S6: MCP 経由のラベルは出どころが "mcp" と分かる。weight は 1 を超えない。
func TestMCPLabelsAreMarkedAndWeightIsBounded(t *testing.T) {
	send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	call := func(id int, name string, args map[string]any) map[string]any {
		return result(t, send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
	}
	d := call(1, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}})["structuredContent"].(map[string]any)
	l := call(2, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request"})
	if l["isError"] == true || l["structuredContent"].(map[string]any)["source"] != "mcp" {
		t.Fatalf("label from MCP must carry source=mcp: %v", l)
	}
	heavy := call(3, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request", "weight": 5})
	if heavy["isError"] != true {
		t.Fatalf("weight > 1 must be rejected: %v", heavy)
	}
	st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
	labs, _ := st.LabeledTraces("intent")
	if len(labs) != 1 || labs[d["trace_id"].(string)].Source != "mcp" {
		t.Fatalf("stored label: %+v", labs)
	}
}

// 監査 C12: 手で温度を 0 にした線は drift が NaN で ok と言ってはいけない。
func TestDriftRejectsZeroTemperature(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
	})
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "vector", Temperature: 0, Biases: map[string]float64{}, N: 2, FittedAt: "2026-10-17"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if err := cmdDrift([]string{specPath, "--store", storeDir, "--model", "m"}); err == nil || !strings.Contains(err.Error(), "temperature") {
		t.Fatalf("drift must refuse a block with temperature 0, got %v", err)
	}
}
