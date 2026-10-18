package main

import (
	"io"
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
)

// 画面の列: ask の判定の次に、act の判定の抜き取り(20 件に 1 件)が並ぶ。残りの act / discard はその後。抜き取りには印が付く。
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

	// act の記録を 60 件(抜き取りが出るまで足す)、ask の記録を 1 件(手で書く)
	st := store.Store{Dir: storeDir}
	sampled := 0
	for i := 0; i < 200 && sampled < 2; i++ {
		code, d := postJSON(t, s.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix " + string(rune('a'+i%26)) + string(rune('a'+i/26))}})
		if code != 200 || d["band"] != "act" {
			t.Fatalf("decide: %d %v", code, d)
		}
		if store.AuditSample(d["trace_id"].(string)) {
			sampled++
		}
	}
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
	if !strings.Contains(body, "audit") || !strings.Contains(body, "抜き取り") {
		t.Fatalf("the page must name the audit sample in both languages")
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// uiAssetRef は index.html が読む自分の assets(script の src と link の href)。
var uiAssetRef = regexp.MustCompile(`(?:src|href)="([^"]+)"`)

// uiText は画面の文字を全部返す: index.html と、そこから読む assets を連結したもの。
// 画面は Svelte のバンドル(web/dist を埋め込み)なので、文言は index.html ではなく assets/*.js に入っている。
// 参照は全部 ./ からの相対(外部の読み込み無し)。それ以外があれば落とす。
func uiText(t *testing.T, url string) string {
	t.Helper()
	index := getBody(t, url+"/")
	var sb strings.Builder
	sb.WriteString(index)
	for _, m := range uiAssetRef.FindAllStringSubmatch(index, -1) {
		ref := m[1]
		if !strings.HasPrefix(ref, "./") {
			t.Fatalf("index.html must reference only its own assets (relative ./), found %q", ref)
		}
		sb.WriteString(getBody(t, url+ref[1:]))
	}
	return sb.String()
}

// check は、act の精度を人の正解だけでも測る。機械の正解(context / replay / file)で高く見えても、人が確かめた act の判定が
// min_n 以上あって精度が act に届かなければ落とす。status はその数を出す。
func TestCheckMeasuresActPrecisionOnHumanTruthsToo(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18", PrecisionAt: 1})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	add := func(i int, truth, source string) {
		in := map[string]string{"message": "m" + string(rune('a'+i))}
		l := store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Truth: truth, Weight: 1, Source: source, At: time.Now()}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		add(i, "change_request", "context") // 機械の正解: 全部当たり
	}
	add(10, "question", "audit") // 人が確かめた act の判定: 2 件とも外れ
	add(11, "question", "audit")
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdCheck([]string{specPath, "--store", storeDir})
	stdout = old
	out := buf.String()
	if err == nil || !strings.Contains(out, "human-checked") || !strings.Contains(out, "0.000") {
		t.Fatalf("overall precision is 10/12 but the 2 human-checked act decisions were both wrong; check must say so:\nerr=%v\n%s", err, out)
	}
	// 人の正解が min_n に届かないうちは落とさず、数だけ言う
	if err := os.Remove(filepath.Join(storeDir, "labels", "intent.jsonl")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		add(i, "change_request", "context")
	}
	add(10, "question", "audit")
	out = capture(t, func() error { return cmdCheck([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "human-checked act decisions: 1") {
		t.Fatalf("below min_n check reports the count without failing:\n%s", out)
	}
	out = capture(t, func() error { return cmdStatus([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "audit: 1 in 20 act decisions is shown to a person; audit answers so far: 1") {
		t.Fatalf("status must show the audit sampling state:\n%s", out)
	}
}
