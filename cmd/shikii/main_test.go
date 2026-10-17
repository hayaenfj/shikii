package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// fakeSystemOne は choice の答えを固定で返す。
func fakeSystemOne(t *testing.T, model string, probs map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		top, _ := argmax(probs)
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeSpec(t *testing.T, dir, pii string) string {
	path := filepath.Join(dir, "intent.yaml")
	src := `id: intent
version: 1
type: choice
question: What is asked?
options:
  - id: change_request
    description: wants a change
  - id: question
    description: wants an answer
  - id: other
    description: none
state:
  template: "{{ message }}"
  required: [message]
policy:
  send_to: [local]
  pii: ` + pii + `
thresholds:
  act: 0.75
  ask: 0.60
eval:
  min_n: 2
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func capture(t *testing.T, fn func() error) string {
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	defer func() { stdout = old }()
	if err := fn(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	return buf.String()
}

var traceRe = regexp.MustCompile(`trace=([0-9a-f]{16})`)

// decide → 記録 → label → calibrate(検証)の輪が閉じる。本文は pii に従って残る/残らない。
func TestDecideRecordLabelCalibrateLoop(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "kev-4b", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})

	// pii: possible → 本文は記録しない
	specPossible := writeSpec(t, dir, "possible")
	out := capture(t, func() error {
		return cmdDecide([]string{specPossible, "--local", srv.URL, "--store", storeDir, "-i", "message=fix the typo"})
	})
	if !strings.Contains(out, "band=ask") || !strings.Contains(out, "top=change_request") {
		t.Fatalf("decide output: %s", out)
	}
	m := traceRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no trace id in output: %s", out)
	}
	trace1 := m[1]
	rec, err := (store.Store{Dir: storeDir}).FindRecord("intent", trace1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Inputs != nil {
		t.Fatalf("pii=possible must not store inputs, got %v", rec.Inputs)
	}
	if rec.InputsHash == "" || rec.Model != "kev-4b" || rec.Raw["change_request"] != 0.9 {
		t.Fatalf("record: %+v", rec)
	}

	// pii: never → 本文が残る
	_ = os.MkdirAll(filepath.Join(dir, "never"), 0o755)
	specNever := writeSpec(t, filepath.Join(dir, "never"), "never")
	out = capture(t, func() error {
		return cmdDecide([]string{specNever, "--local", srv.URL, "--store", storeDir, "-i", "message=why is the sky blue"})
	})
	trace2 := traceRe.FindStringSubmatch(out)[1]
	rec2, err := (store.Store{Dir: storeDir}).FindRecord("intent", trace2)
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Inputs["message"] != "why is the sky blue" {
		t.Fatalf("pii=never must store inputs, got %v", rec2.Inputs)
	}

	// label: 1 件目は当たり、2 件目は外れ(モデルは change_request と言ったが question が正解)
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "ask", trace1, "change_request"})
	})
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", trace2, "question"}) })
	if err := cmdLabel([]string{"--store", storeDir, trace2, "operation"}); err == nil {
		t.Fatal("label with an option the model was not asked about must be rejected")
	}
	if err := cmdLabel([]string{"--store", storeDir, "deadbeefdeadbeef", "question"}); err == nil {
		t.Fatal("label for unknown trace must be rejected")
	}

	// calibrate(検証): 2 件とも p_top=0.9 ≥ act、正答 1/2 → precision 0.5 < 0.75 → identity を書かない
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPossible, "--store", storeDir, "--model", "kev-4b"})
	})
	if !strings.Contains(out, "n=2 (file=0 store=2 replay=0)") || !strings.Contains(out, "top-1 accuracy=0.500") {
		t.Fatalf("calibrate output: %s", out)
	}
	if err := cmdCalibrate([]string{specPossible, "--store", storeDir, "--model", "kev-4b", "--write"}); err == nil {
		t.Fatal("precision below act must refuse to write identity calibration")
	}

	// 別モデル名・別文言のラベルは数えない
	out = capture(t, func() error {
		// min_n=2 なので件数不足で止まる前に n を見たい → エラーは許容
		var buf bytes.Buffer
		old := stdout
		stdout = &buf
		defer func() { stdout = old; _, _ = os.Stdout.Write(nil) }()
		err := cmdCalibrate([]string{specPossible, "--store", storeDir, "--model", "jev-1.13.0"})
		stdout = old
		if err == nil || !strings.Contains(err.Error(), "no labels") {
			t.Fatalf("labels for another model must not be used, got err=%v out=%s", err, buf.String())
		}
		return nil
	})
	_ = out
}

// 検証で精度が act 以上なら identity を書き、次の decide は act を出す。
func TestVerifiedModelGetsIdentityAndActs(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "jev-1.13.0", map[string]float64{"change_request": 1, "question": 0, "other": 0})
	specPath := writeSpec(t, dir, "never")

	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix it"})
		})
		if !strings.Contains(out, "band=ask") {
			t.Fatalf("before verification must be ask: %s", out)
		}
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "jev-1.13.0", "--write"})
	})
	if !strings.Contains(out, "wrote calibration") {
		t.Fatalf("calibrate: %s", out)
	}
	b, _ := os.ReadFile(specPath)
	if !strings.Contains(string(b), "method: identity") || !strings.Contains(string(b), "model: jev-1.13.0") {
		t.Fatalf("spec must carry identity calibration for jev-1.13.0:\n%s", b)
	}
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix it"})
	})
	if !strings.Contains(out, "band=act") || !strings.Contains(out, "calibrated=true") {
		t.Fatalf("verified model must act: %s", out)
	}
}

func TestParseTruth(t *testing.T) {
	if v, err := parseTruth("noul", json.RawMessage(`true`)); err != nil || v != "yes" {
		t.Fatalf("bool true → yes: %v %v", v, err)
	}
	if v, err := parseTruth("noul", json.RawMessage(`"no"`)); err != nil || v != "no" {
		t.Fatalf("string no: %v %v", v, err)
	}
	if _, err := parseTruth("choice", json.RawMessage(`true`)); err == nil {
		t.Fatal("bool truth for choice must be rejected")
	}
	if v, err := parseTruth("choice", json.RawMessage(`"question"`)); err != nil || v != "question" {
		t.Fatalf("choice string: %v %v", v, err)
	}
}

// check は契約の検査: 文言を変えたら stale、精度が落ちたら FAIL。drift は直近ラベルの劣化で警告。
func TestCheckAndDrift(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m1", map[string]float64{"change_request": 0.95, "question": 0.03, "other": 0.02})
	specPath := writeSpec(t, dir, "never")

	var traces []string
	for i := 0; i < 4; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m1", "--write"}) })

	// 契約が成り立っている
	out := capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ok   intent") {
		t.Fatalf("check must pass: %s", out)
	}

	// 文言を変えると stale で落ちる
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCheck([]string{"--store", storeDir, specPath})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "stale spec_hash") {
		t.Fatalf("editing the question must fail check with stale hash: err=%v out=%s", err, buf.String())
	}
	if err := os.WriteFile(specPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	// drift: 直近が全部当たりなら ok
	out = capture(t, func() error {
		return cmdDrift([]string{specPath, "--store", storeDir, "--model", "m1", "--window", "10"})
	})
	if !strings.Contains(out, "ok") || !strings.Contains(out, "ask agreement:    1.000") {
		t.Fatalf("drift must be ok: %s", out)
	}

	// 外れが増える(モデルは change_request と言い続けるが正解は question)→ precision が落ちて警告
	for i := 0; i < 6; i++ {
		o := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=why"})
		})
		tr := traceRe.FindStringSubmatch(o)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "question"}) })
	}
	buf.Reset()
	stdout = &buf
	err = cmdDrift([]string{specPath, "--store", storeDir, "--model", "m1", "--window", "10"})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "WARNING") || !strings.Contains(buf.String(), "precision at act 0.400") {
		t.Fatalf("drift must warn when recent precision collapses: err=%v out=%s", err, buf.String())
	}
	// check も同じラベルで落ちる(較正時の約束が今のラベルで成り立たない)
	buf.Reset()
	stdout = &buf
	err = cmdCheck([]string{"--store", storeDir, specPath})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "precision at act is 0.400") {
		t.Fatalf("check must fail when labels no longer support act: err=%v out=%s", err, buf.String())
	}
}

// 生では確信が足りない choice が vector scaling で act に届く。識別子の鍵も同じ規則に従う。
func TestCalibrateWritesVectorWhenIdentityIsNotEnough(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	// 正解は常に change_request だが、モデルは 0.55 しか出さない(Kev-4B の「確信不足」の再現)
	srv := fakeSystemOne(t, "kev", map[string]float64{"change_request": 0.55, "other": 0.35, "question": 0.10})
	specPath := writeSpec(t, dir, "never")
	for i := 0; i < 6; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
		})
		if !strings.Contains(out, "band=discard") {
			t.Fatalf("raw 0.55 is below ask: %s", out)
		}
		tr := traceRe.FindStringSubmatch(out)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev", "--write"})
	})
	if !strings.Contains(out, "vector:") || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("calibrate must fit and write vector scaling: %s", out)
	}
	b, _ := os.ReadFile(specPath)
	if !strings.Contains(string(b), "method: vector") || !strings.Contains(string(b), "biases:") {
		t.Fatalf("spec must carry vector calibration:\n%s", b)
	}
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	if !strings.Contains(out, "band=act") || !strings.Contains(out, "calibrated=true") {
		t.Fatalf("after vector calibration the same input must act: %s", out)
	}
	out = capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ok   intent") {
		t.Fatalf("check must pass for the written vector calibration: %s", out)
	}
}

// fakeByMessage は state(メッセージ)ごとに違う分布を返す。
func fakeByMessage(t *testing.T, model string, table map[string]map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		probs, ok := table[strings.TrimSpace(req.State)]
		if !ok {
			http.Error(w, "unknown state "+req.State, 400)
			return
		}
		top, _ := argmax(probs)
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// identity でも act の precision は満たすが recall が低い。vector が precision を保ったまま recall を上げるなら vector を選ぶ。
func TestCalibratePrefersVectorWhenItRaisesRecall(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	table := map[string]map[string]float64{}
	// 10 件: 半分は確信あり(0.95)、半分は確信不足(0.55 で other に 0.35 逃げる)。正解は全部 change_request。
	for i := 0; i < 10; i++ {
		msg := "fix " + string(rune('a'+i))
		if i%2 == 0 {
			table[msg] = map[string]float64{"change_request": 0.95, "other": 0.03, "question": 0.02}
		} else {
			table[msg] = map[string]float64{"change_request": 0.55, "other": 0.35, "question": 0.10}
		}
	}
	srv := fakeByMessage(t, "kev", table)
	specPath := writeSpec(t, dir, "never")
	for msg := range table {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=" + msg})
		})
		tr := traceRe.FindStringSubmatch(out)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev", "--write"})
	})
	if !strings.Contains(out, "at act>=0.75: precision=1.000 recall=0.500") {
		t.Fatalf("raw must show precision 1.0 / recall 0.5: %s", out)
	}
	if !strings.Contains(out, "choose vector: both satisfy act") {
		t.Fatalf("vector must be chosen for higher recall: %s", out)
	}
	b, _ := os.ReadFile(specPath)
	if !strings.Contains(string(b), "method: vector") {
		t.Fatalf("spec must carry vector:\n%s", b)
	}
	// 確信不足だった入力が act に届く
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix b"})
	})
	if !strings.Contains(out, "band=act") {
		t.Fatalf("after vector the 0.55 input must act: %s", out)
	}
}

func TestRecallAtAndCrossValidation(t *testing.T) {
	probs := []float64{0.9, 0.9, 0.5, 0.5}
	truths := []bool{true, false, true, true}
	w := []float64{1, 1, 1, 1}
	if r := recallAt(0.75, probs, truths, w); math.Abs(r-1.0/3) > 1e-9 {
		t.Fatalf("recall: got %f want 1/3", r)
	}
	if p := precisionAt(0.75, probs, truths, w); p != 0.5 {
		t.Fatalf("precision: got %f want 0.5", p)
	}
	// CV は fold 数より少ないデータでは 0 を返して黙る(過学習した数字を出さない)
	sp := &spec.Spec{Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6}}
	if p, r := crossValidateVector(sp, []calib.MultiSample{{Raw: map[string]float64{"a": 0.6, "b": 0.4}, Truth: "a"}}, 5); p != 0 || r != 0 {
		t.Fatalf("too few samples must yield 0,0 got %f %f", p, r)
	}
}

// ファイルのラベルでモデルに聞いて較正したら、得たラベルは store に残り、以後 check はモデル無しで通る。
// 同じファイルで再実行しても二重に数えない。
func TestCalibrateFromFilePersistsLabelsForOfflineCheck(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	table := map[string]map[string]float64{}
	var rows []map[string]any
	for i := 0; i < 8; i++ {
		msg := "fix " + string(rune('a'+i))
		table[msg] = map[string]float64{"change_request": 0.9, "other": 0.07, "question": 0.03}
		rows = append(rows, map[string]any{"inputs": map[string]string{"message": msg}, "truth": "change_request"})
	}
	labeledPath := filepath.Join(dir, "labeled.json")
	b, _ := json.Marshal(rows)
	if err := os.WriteFile(labeledPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := fakeByMessage(t, "kev", table)
	specPath := writeSpec(t, dir, "never")

	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "n=8 (file=8 store=0 replay=0)") || !strings.Contains(out, "persisted 8 queried labels") || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("first calibrate: %s", out)
	}
	stored, err := (store.Store{Dir: storeDir}).Labels("intent", mustHash(t, specPath), "kev")
	if err != nil || len(stored) != 8 {
		t.Fatalf("8 labels must be persisted, got %d (%v)", len(stored), err)
	}
	for _, l := range stored {
		if l.At.IsZero() {
			t.Fatalf("persisted labels must carry a timestamp (drift sorts by it): %+v", l)
		}
	}

	// 2 回目: モデルを止めても、store のラベルを再利用して同じ件数で較正できる。二重に数えず、新たに残さない
	srv.Close()
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--model", "kev"})
	})
	if !strings.Contains(out, "reused 8 stored labels") || !strings.Contains(out, "n=8 (file=8 store=0 replay=0)") || strings.Contains(out, "persisted") {
		t.Fatalf("re-run must reuse stored labels offline without double counting: %s", out)
	}

	// check も store のラベルで通る(モデルは止まっている)
	out = capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ok   intent") || strings.Contains(out, "FAIL") {
		t.Fatalf("check must pass offline from persisted labels: %s", out)
	}
	// drift も同じラベルで動く(ask 由来が無いので一致率は 0)
	out = capture(t, func() error { return cmdDrift([]string{specPath, "--store", storeDir, "--model", "kev"}) })
	if !strings.Contains(out, "ok") {
		t.Fatalf("drift must run offline: %s", out)
	}
}

func mustHash(t *testing.T, specPath string) string {
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	return sp.Hash()
}

// fakeNoulByMessage は state ごとに決まった p(yes) を返す。
func fakeNoulByMessage(t *testing.T, model string, table map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		p, ok := table[strings.TrimSpace(req.State)]
		if !ok {
			http.Error(w, "unknown state "+req.State, 400)
			return
		}
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "noul", Noul: p}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeNoulSpec(t *testing.T, dir string) string {
	path := filepath.Join(dir, "same.yaml")
	src := `id: same
version: 1
type: noul
question: Same case?
state:
  template: "{{ a }}"
  required: [a]
policy:
  send_to: [local]
  pii: never
thresholds:
  act: 0.70
  ask: 0.50
eval:
  min_n: 4
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// noul でも identity と platt を比べる: 既に較正されたモデルには identity、過信するモデルには platt。
func TestNoulChoosesIdentityOrPlattByCV(t *testing.T) {
	run := func(t *testing.T, table map[string]float64, truth func(msg string) bool, wantMethod string) string {
		dir := t.TempDir()
		storeDir := filepath.Join(dir, ".shikii")
		srv := fakeNoulByMessage(t, "m", table)
		specPath := writeNoulSpec(t, dir)
		// map の走査順は毎回変わる。ラベルの並び(= CV の fold 割り)を固定する
		msgs := make([]string, 0, len(table))
		for msg := range table {
			msgs = append(msgs, msg)
		}
		sort.Strings(msgs)
		for _, msg := range msgs {
			out := capture(t, func() error {
				return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "a=" + msg})
			})
			tr := traceRe.FindStringSubmatch(out)[1]
			lab := "no"
			if truth(msg) {
				lab = "yes"
			}
			capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, lab}) })
		}
		out := capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m", "--write"}) })
		b, _ := os.ReadFile(specPath)
		if !strings.Contains(string(b), "method: "+wantMethod) {
			t.Fatalf("want %s, spec:\n%s\nout:\n%s", wantMethod, b, out)
		}
		return out
	}

	// 既に較正されている: 正例は 0.9 前後、負例は 0.1 前後 → platt は改善せず identity
	calibrated := map[string]float64{}
	for i := 0; i < 10; i++ {
		calibrated[fmt.Sprintf("pos%d", i)] = 0.86 + 0.01*float64(i)
		calibrated[fmt.Sprintf("neg%d", i)] = 0.05 + 0.01*float64(i)
	}
	out := run(t, calibrated, func(m string) bool { return strings.HasPrefix(m, "pos") }, "identity")
	if !strings.Contains(out, "choose identity") {
		t.Fatalf("expected identity: %s", out)
	}

	// 過信: 正例も負例も 0.9 以上を返すが、負例は少し低い → platt が閾値を作り直して precision を回復
	over := map[string]float64{}
	for i := 0; i < 10; i++ {
		over[fmt.Sprintf("pos%d", i)] = 0.985 + 0.001*float64(i)
		over[fmt.Sprintf("neg%d", i)] = 0.90 + 0.003*float64(i)
	}
	out = run(t, over, func(m string) bool { return strings.HasPrefix(m, "pos") }, "platt")
	if !strings.Contains(out, "choose platt: CV precision") {
		t.Fatalf("expected platt chosen for overconfident model: %s", out)
	}
}

// 部分ラベル "!edit"(結果から「変更ではなかった」だけ分かった)は precision の評価に効き、学習からは外れる。
func TestPartialLabelsCountAgainstPrecision(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.07, "other": 0.03})
	specPath := writeSpec(t, dir, "never")
	var traces []string
	for i := 0; i < 5; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=do it"})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	// モデルは全部 change_request 0.9 と言った。2 件は本当に変更、2 件は「変更ではなかった」(外れ)、
	// 1 件は「質問ではなかった」(change_request と矛盾しないので当たり扱い)
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", traces[0], "change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", traces[1], "change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", "--weight", "0.7", traces[2], "!change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", "--weight", "0.7", traces[3], "!change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", "--weight", "0.7", traces[4], "!question"})
	})
	out := capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m"}) })
	if !strings.Contains(out, "labels: 2 full, 3 partial") {
		t.Fatalf("partial labels must be counted separately: %s", out)
	}
	// precision@act = (2 + 0.7) / (2 + 0.7 + 0.7·2) = 0.659 — "!change_request" は外れ、"!question" は当たり
	if !strings.Contains(out, "at act>=0.75: precision=0.659") {
		t.Fatalf("partial labels must be scored as 'anything but x': %s", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Fatalf("precision below act must warn: %s", out)
	}
}

// score + thresholds.level: 報告の precision も P(level ≥ k) と「正解レベル ≥ k」で測る。
func TestScoreLevelReports(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	// 分布は 2:0.10 / 3:0.45 / 4:0.45 → 最上位 0.45(同点は小さい鍵 "3")、P(≥3)=0.90
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "score", Score: 3.35, Confidence: 0.4,
				Probabilities: map[string]float64{"0": 0, "1": 0, "2": 0.10, "3": 0.45, "4": 0.45}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	specPath := filepath.Join(dir, "lvl.yaml")
	src := `id: lvl
version: 1
type: score
question: how related?
levels: [unrelated, same domain, circumstantial, directly connected, same case]
state:
  template: "{{ m }}"
  required: [m]
policy:
  send_to: [local]
  pii: never
thresholds:
  act: 0.75
  ask: 0.50
  level: 3
eval:
  min_n: 2
`
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var traces []string
	for i := 0; i < 4; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "m=x"})
		})
		if !strings.Contains(out, "band=ask") || !strings.Contains(out, "p=0.900") {
			t.Fatalf("decide must band on P(level>=3)=0.90 (ask, since uncalibrated): %s", out)
		}
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	// 正解: 3 件は level 4(事象あり)、1 件は level 1(事象なし)
	for _, tr := range traces[:3] {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "4"}) })
	}
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", traces[3], "1"}) })
	out := capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m"}) })
	// P(≥3)=0.90 ≥ act で 4 件とも act 側、正解は 3/4 → precision 0.750
	if !strings.Contains(out, "at act>=0.75: precision=0.750 recall=1.000") {
		t.Fatalf("score-level precision must be measured on P(level>=k) vs truth>=k: %s", out)
	}
}

// 順序バイアスの測定: 先頭の選択肢に確率を足す偏ったモデルでは flip と位置の偏りが出て、偏りの無いモデルでは出ない。
func TestBiasDetectsOrderSensitivity(t *testing.T) {
	mkServer := func(firstBonus float64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Questions map[string]struct {
					Criteria systemone.OrderedCriteria `json:"criteria"`
				} `json:"questions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			q := req.Questions["q"]
			// 素の確率: change_request 0.45 / question 0.40 / other 0.15。先頭に firstBonus を足して正規化
			base := map[string]float64{"change_request": 0.45, "question": 0.40, "other": 0.15}
			probs := map[string]float64{}
			var sum float64
			for pos, c := range q.Criteria {
				p := base[c.ID]
				if pos == 0 {
					p += firstBonus
				}
				probs[c.ID] = p
				sum += p
			}
			for k := range probs {
				probs[k] /= sum
			}
			top, _ := argmax(probs)
			resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{
				"q": {Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.5},
			}}
			_ = json.NewEncoder(w).Encode(resp)
		}))
	}
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "never")
	var rows []map[string]any
	for i := 0; i < 6; i++ {
		rows = append(rows, map[string]any{"inputs": map[string]string{"message": fmt.Sprintf("m%d", i)}, "truth": "change_request"})
	}
	b, _ := json.Marshal(rows)
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	biased := mkServer(0.30)
	defer biased.Close()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdBias([]string{specPath, "--labeled", labeledPath, "--local", biased.URL, "--permutations", "6", "--rows", "6"})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "WARNING") {
		t.Fatalf("biased model must be flagged: err=%v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "queries=42") {
		t.Fatalf("6 rows × (1 + 6 permutations) = 42 queries: %s", buf.String())
	}
	if strings.Contains(buf.String(), "flip rate (top changes with order): 0.000") {
		t.Fatalf("biased model must flip the top for some orders:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "pos 1: +0.") {
		t.Fatalf("the first slot must show a positive shift for the first-option-biased model:\n%s", buf.String())
	}

	fair := mkServer(0)
	defer fair.Close()
	out := capture(t, func() error {
		return cmdBias([]string{specPath, "--labeled", labeledPath, "--local", fair.URL, "--permutations", "6", "--rows", "6"})
	})
	if !strings.Contains(out, "flip rate (top changes with order): 0.000") || !strings.Contains(out, "position shift spread (max−min): 0.000") || !strings.Contains(out, "\nok") {
		t.Fatalf("order-insensitive model must pass cleanly:\n%s", out)
	}
}

// status は記録・ラベル・較正の棚卸し。古い文言のラベルは stale、足りなければ collecting、揃えば ready。
func TestStatusInventory(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "never") // min_n: 2
	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "ask", traces[0], "change_request"})
	})
	out := capture(t, func() error { return cmdStatus([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "intent: records=3 labels=1") || !strings.Contains(out, "collecting (1/2)") || !strings.Contains(out, "by source: ask=1") {
		t.Fatalf("status before enough labels:\n%s", out)
	}
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", traces[1], "!change_request"})
	})
	out = capture(t, func() error { return cmdStatus([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ready to calibrate") || !strings.Contains(out, "by truth:  !change_request=1 change_request=1") {
		t.Fatalf("status when enough labels:\n%s", out)
	}
	// 文言を変えると、溜まったラベルは stale と出る
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdStatus([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "stale hash") {
		t.Fatalf("status must flag labels from the old wording:\n%s", out)
	}
	// spec を渡さなくても数は出る
	out = capture(t, func() error { return cmdStatus([]string{"--store", storeDir}) })
	if !strings.Contains(out, "intent: records=3 labels=2") || !strings.Contains(out, "spec not given") {
		t.Fatalf("status without spec:\n%s", out)
	}
}

// gc は既定で表示だけ。--apply で「古くてラベルの無い記録」だけ消し、ラベル付きは残る。
func TestGCKeepsLabeledAndIsDryRunByDefault(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "never")
	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, traces[0], "change_request"}) })

	// 既定の 30d では今日の記録は何も対象にならない
	out := capture(t, func() error { return cmdGC([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "intent: would remove 0") {
		t.Fatalf("fresh records must not be candidates under the default age:\n%s", out)
	}
	// 年齢 0 なら、ラベルの無い 2 件が候補。表示だけで消えない
	out = capture(t, func() error { return cmdGC([]string{"--store", storeDir, "--older-than", "0", specPath}) })
	if !strings.Contains(out, "intent: would remove 2") || !strings.Contains(out, "keep 1 labeled") || !strings.Contains(out, "--apply") {
		t.Fatalf("dry run output:\n%s", out)
	}
	st := capture(t, func() error { return cmdStatus([]string{"--store", storeDir}) })
	if !strings.Contains(st, "intent: records=3 labels=1") {
		t.Fatalf("dry run must not delete anything:\n%s", st)
	}
	// --apply で消える。ラベルは残る
	out = capture(t, func() error { return cmdGC([]string{"--store", storeDir, "--older-than", "0", "--apply", specPath}) })
	if !strings.Contains(out, "intent: removed 2") {
		t.Fatalf("apply output:\n%s", out)
	}
	st = capture(t, func() error { return cmdStatus([]string{"--store", storeDir}) })
	if !strings.Contains(st, "intent: records=1 labels=1") {
		t.Fatalf("after apply:\n%s", st)
	}
	// spec を渡さなければ store にある全部の spec が対象
	out = capture(t, func() error { return cmdGC([]string{"--store", storeDir, "--older-than", "0"}) })
	if !strings.Contains(out, "intent: would remove 0") {
		t.Fatalf("gc without spec must still cover the store:\n%s", out)
	}
	// 変な年齢は拒む
	if err := cmdGC([]string{"--store", storeDir, "--older-than", "3 weeks", specPath}); err == nil {
		t.Fatal("unparsable --older-than must be an error")
	}
}

func TestParseAge(t *testing.T) {
	cases := map[string]time.Duration{"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, "0": 0, "1w": 7 * 24 * time.Hour}
	for in, want := range cases {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Fatalf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-1d", "soon", "3 weeks"} {
		if _, err := parseAge(bad); err == nil {
			t.Fatalf("parseAge(%q) must fail", bad)
		}
	}
}

// label --last は spec の直近の記録に付く(trace を控えなくてよい)。
func TestLabelLastRecord(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "never")
	var traces []string
	for _, msg := range []string{"first", "second"} {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=" + msg})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	out := capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--spec-id", "intent", "--last", "question"})
	})
	if !strings.Contains(out, "labeled "+traces[1]) {
		t.Fatalf("--last must label the most recent record (%s), got: %s", traces[1], out)
	}
	if err := cmdLabel([]string{"--store", storeDir, "--last", "question"}); err == nil {
		t.Fatal("--last without --spec-id must be rejected")
	}
	if err := cmdLabel([]string{"--store", storeDir, "--spec-id", "nothing", "--last", "question"}); err == nil {
		t.Fatal("--last on a spec with no records must be rejected")
	}
}

// 文言を変えると較正は消えるが、人が付けた正解は消えない。pii: local で本文が残っていれば、
// calibrate が同じ入力を新しい文言(新しいハッシュ)でモデルに聞き直し、人の手を借りずに線を引き直す。
func TestCalibrateReplaysTruthsAfterWordingChange(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "local") // min_n: 2
	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	oldHash := mustLoad(t, specPath).Hash()

	// 文言を変える → ハッシュが変わる → 今の (hash, model) にはラベルが無い
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	newHash := mustLoad(t, specPath).Hash()
	if newHash == oldHash {
		t.Fatal("test setup: wording change must change the hash")
	}

	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "replayed 3") || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("calibrate must replay the 3 stored truths against the new wording and write:\n%s", out)
	}
	st := store.Store{Dir: storeDir}
	got, err := st.Labels("intent", newHash, "m")
	if err != nil || len(got) != 3 {
		t.Fatalf("replayed labels must be persisted under the new hash: %d %v", len(got), err)
	}
	for _, l := range got {
		if l.Source != "replay" || l.Truth != "change_request" {
			t.Fatalf("replayed label must keep the human truth and say where it came from: %+v", l)
		}
	}
	// 2 回目は聞き直さない(もう新しいハッシュにラベルがある)
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir})
	})
	if strings.Contains(out, "replayed") || !strings.Contains(out, "store=3") {
		t.Fatalf("second run must use the persisted labels, not replay again:\n%s", out)
	}
	// check はモデル無しで通る(新しいハッシュのラベルが store にある)
	capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
}

// 本文が残っていない(pii: possible)なら聞き直せない。黙って 0 件にせず、理由を言う。
func TestCalibrateCannotReplayWithoutStoredText(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "possible")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	tr := traceRe.FindStringSubmatch(out)[1]
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, tr, "change_request"}) })
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir})
	stdout = old
	if err == nil {
		t.Fatal("no labels for the new hash and nothing to replay: calibrate must fail")
	}
	if !strings.Contains(buf.String(), "1 earlier truth") || !strings.Contains(buf.String(), "pii: local") {
		t.Fatalf("calibrate must say how many truths exist but cannot be replayed, and why:\n%s", buf.String())
	}
}

// ファイルから較正した正解も貯金に入る。文言を変えた後、ファイル無し(--labeled none)でも聞き直しで線が戻る。
func TestReplayCoversTruthsThatCameFromAFile(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "never")
	rows := `[{"inputs":{"message":"fix a"},"truth":"change_request"},{"inputs":{"message":"fix b"},"truth":"change_request"},{"inputs":{"message":"fix c"},"truth":"change_request"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", "none", "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "replayed 3") || !strings.Contains(out, "wrote calibration") || strings.Contains(out, "file=3") {
		t.Fatalf("truths from the file must be replayable without the file:\n%s", out)
	}
}

func mustLoad(t *testing.T, path string) *spec.Spec {
	t.Helper()
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}
