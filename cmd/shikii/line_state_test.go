package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.8.3: 正解が eval.min_n に届いた後の状態を、件数だけで「ready to calibrate」と言わず、片方の答えしか無い・分けられない・
// 線が引けた、に分けて status に出し、本人にもターンの終わりに 1 回だけ知らせる(状態が変わったらまた 1 回)。
func TestLineStatesAreToldApartAndAnnouncedOnce(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := filepath.Join(agent, "decisions", "changes-in-scope.yaml")
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	add := func(truth string, n int) {
		var labels []store.Label
		for i := 0; i < n; i++ {
			labels = append(labels, store.Label{TraceID: fmt.Sprintf("%s%03d", truth, i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: fmt.Sprintf("%s%03d", truth, i), Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	status := func() string {
		return capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	}

	add("no", sp.Eval.MinN)
	if out := status(); strings.Contains(out, "ready to calibrate") || !strings.Contains(out, fmt.Sprintf("only one answer so far (no=%d)", sp.Eval.MinN)) {
		t.Fatalf("片方の答えしか無い時は、件数が足りても線を引けないと出す:\n%s", out)
	}
	first := lineSummary(agent)
	if !strings.Contains(first, "changes-in-scope") || !strings.Contains(first, "only one answer") {
		t.Fatalf("片方の答えしか無いことを本人に知らせる: %q", first)
	}
	if again := lineSummary(agent); again != "" {
		t.Fatalf("同じ状態は二度知らせない: %q", again)
	}

	add("yes", 5)
	if out := status(); !strings.Contains(out, "enough truths: the line is drawn automatically") {
		t.Fatalf("両方の答えがあれば、自動で線を引く途中と出す:\n%s", out)
	}
	guard := filepath.Join(agent, "calibrating-changes-in-scope")
	if err := os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m1 %d %s\n", sp.Eval.MinN+5, sp.Hash())), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * hookCalibrateGap)
	if err := os.Chtimes(guard, old, old); err != nil {
		t.Fatal(err)
	}
	if out := status(); !strings.Contains(out, "tried: the answers could not be separated") {
		t.Fatalf("試して引けなかった時は、分けられないと出す:\n%s", out)
	}
	if line := lineSummary(agent); !strings.Contains(line, "could not be separated") {
		t.Fatalf("分けられないことを本人に知らせる(状態が変わったのでもう 1 回): %q", line)
	}

	sp.Calibration = []spec.Calibration{{Model: "m1", SpecHash: sp.Hash(), Method: "platt", N: sp.Eval.MinN + 5}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if out := status(); !strings.Contains(out, "calibrated: platt") {
		t.Fatalf("線が引けた: \n%s", out)
	}
	drawn := lineSummary(agent)
	if !strings.Contains(drawn, "a line was drawn") || !strings.Contains(drawn, "act") {
		t.Fatalf("線が引けたことと、これから act で進むことを本人に知らせる: %q", drawn)
	}
	if again := lineSummary(agent); again != "" {
		t.Fatalf("引けた線は二度知らせない: %q", again)
	}
}

// 画面(/v1/status)も同じ区別で、問いの鍵ごとの state に one_answer / not_separable を返す(件数だけで ready にしない)。
func TestStatusAPIShowsWhyNoLineWasDrawn(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "wiki-place.yaml"))
	st := store.Store{Dir: root}
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN; i++ {
		labels = append(labels, store.Label{TraceID: fmt.Sprintf("p%03d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: fmt.Sprintf("p%03d", i), Truth: "no", Weight: 0.5, Source: "outcome", At: time.Now()})
	}
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	state := func() string {
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
		for _, s := range body.Specs {
			if s.ID == "wiki-place" && len(s.Keys) == 1 {
				return s.Keys[0]["state"].(string)
			}
		}
		return ""
	}
	if got := state(); got != "one_answer" {
		t.Fatalf("片方の答えしか無い時は one_answer: %q", got)
	}
	st.AppendLabels([]store.Label{{TraceID: "y1", SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: "y1", Truth: "yes", Weight: 1, Source: "outcome", At: time.Now()}})
	guard := filepath.Join(root, "calibrating-wiki-place")
	os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 m1 %d %s\n", sp.Eval.MinN+1, sp.Hash())), 0o600)
	old := time.Now().Add(-2 * hookCalibrateGap)
	os.Chtimes(guard, old, old)
	if got := state(); got != "not_separable" {
		t.Fatalf("試して引けなかった時は not_separable: %q", got)
	}
}
