package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// A layout-sensitive fake model: with the default layout it says change_request for everything (0.6); with plain it reads the text and gets it right (0.9). It adds the layout to the response model name (as the converter does).
func layoutSensitiveModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := "m"
		truth := "change_request"
		if strings.Contains(req.State, "why") {
			truth = "question"
		}
		p := 0.6
		if req.Layout == "plain" {
			name, p = "m+plain", 0.9
		} else {
			truth = "change_request"
		}
		resp := systemone.Response{Model: name, Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			ans := truth
			if strings.Contains(q.Instructions, "Anything goes") {
				ans = "other" // answer other to everything for deliberately bad wording (to make a counterexample)
			}
			probs := map[string]float64{"change_request": (1 - p) / 2, "question": (1 - p) / 2, "other": 0}
			probs[ans] = p
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: ans, Probabilities: probs, Confidence: p}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// tune can score candidates that differ only in layout; if one wins, the adopted version gets the layout, and calibration is drawn with that layout's model name.
func TestTuneScoresAndAdoptsALayout(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := layoutSensitiveModel(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	b, _ := os.ReadFile(specPath)
	src := string(b) + "variants:\n  - name: plain\n    layout: plain\n"
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(bankRows()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii"), "--write"})
	})
	if !strings.Contains(out, "winner: plain") || !strings.Contains(out, "layout: plain") {
		t.Fatalf("the layout candidate must be scored, described and win:\n%s", out)
	}
	v2 := mustLoad(t, filepath.Join(dir, "decisions", "intent-v2.yaml"))
	if v2.Layout != "plain" || v2.CalibrationFor("m+plain") == nil {
		t.Fatalf("the adopted version carries the layout and is calibrated under the layout's model name: %+v", v2.Calibration)
	}
}

// Store defaults: everything shikii makes is under the store (.shikii/). Omitting --specs means <store>/decisions; the wiki is <store>/wiki.
// serve fills the defaults before newServer (loading specs): without that it cannot start with `no spec (*.yaml) in ` (happened on a real machine on 2026-10-18).
func TestLayoutDefaultsLiveUnderTheStore(t *testing.T) {
	o := serveOpts{storeDir: filepath.Join("x", ".shikii")}
	o.resolve()
	if o.specsDir != filepath.Join("x", ".shikii", "decisions") || o.wikiDir != filepath.Join("x", ".shikii", "wiki") {
		t.Fatalf("defaults must live under the store: %+v", o)
	}
	o = serveOpts{storeDir: ".shikii", specsDir: "decisions", wikiDir: "w"}
	o.resolve()
	if o.specsDir != "decisions" || o.wikiDir != "w" {
		t.Fatalf("explicit folders must stay: %+v", o)
	}
	// The store init creates and new's defaults are the same
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	capture(t, func() error { return cmdInit(nil) })
	for _, p := range []string{"decisions", "wiki", "records", "labels", filepath.Join("wiki", "index.md")} {
		if !fileExists(filepath.Join(dir, ".shikii", p)) {
			t.Fatalf("init must create .shikii/%s", p)
		}
	}
	for _, p := range []string{"decisions", "wiki"} {
		if fileExists(filepath.Join(dir, p)) {
			t.Fatalf("init must not add %s/ to the project root", p)
		}
	}
	capture(t, func() error {
		return cmdNew([]string{"my-id", "--type", "noul", "--question", "Is it?", "--input", "text", "--pii", "true", "--send-to", "local"})
	})
	if !fileExists(filepath.Join(dir, ".shikii", "decisions", "my-id.yaml")) {
		t.Fatal("new must write under .shikii/decisions")
	}
}
