package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// Counterexamples for the stumbles found in the first-time user trial (2026-10-18, one full round of sorting inquiry emails from the documents alone).
//
// (1) init copies the ready-made knowledge (index.md, types/, patterns/, models/) into wiki/. Pages the person already has are not touched. Every link in the copied pages resolves inside that wiki.
func TestInitSeedsTheWikiWithSharedKnowledgeWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	mine := filepath.Join(dir, ".shikii", "wiki", "types", "choice.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "wiki/: ") || !strings.Contains(out, "built-in knowledge") {
		t.Fatalf("init must say it seeded the wiki:\n%s", out)
	}
	for _, p := range []string{"index.md", "types/noul.md", "types/score.md", "models/kev-4b.md"} {
		if !fileExists(filepath.Join(dir, ".shikii", "wiki", p)) {
			t.Fatalf("seeded wiki must have %s", p)
		}
	}
	if pats, _ := filepath.Glob(filepath.Join(dir, ".shikii", "wiki", "patterns", "*.md")); len(pats) == 0 {
		t.Fatal("seeded wiki must have the patterns (the proposer reads them)")
	}
	if b, _ := os.ReadFile(mine); string(b) != "# mine\n" {
		t.Fatalf("a page the person already has is never overwritten: %q", b)
	}
	// Relative links in the copied pages all resolve inside this wiki (fails if they point to runs/ or specs/, which exist only in the repo)
	link := regexp.MustCompile(`\]\(([^)#]+)[^)]*\)`)
	if err := filepath.Walk(filepath.Join(dir, ".shikii", "wiki"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, m := range link.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(p), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to %s, which a fresh project's wiki does not have", strings.TrimPrefix(p, dir+"/"), target)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The second time: nothing to add, so it stays quiet
	if out := capture(t, func() error { return cmdInit(nil) }); strings.Contains(out, "pages of shared knowledge added") {
		t.Fatalf("second init has nothing to seed:\n%s", out)
	}
}

// (2) When truths are below eval.min_n, calibrate and tune say "what to do": collect truths, or, for a trial, lower the spec's eval.min_n.
// tune rejected with the same text even when --labeled was passed, so --labeled is not suggested as the fix (it is added only as how files are read, when not passed).
func TestTooFewTruthsSaysWhatToDoNext(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m1", map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02})
	specPath := writeSpec(t, dir, "false") // min_n 2
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(specPath, append(b, []byte("variants:\n  - name: strict\n    question: What exactly is asked?\n")...), 0o644); err != nil {
		t.Fatal(err) // tune stops there without candidates, so put one candidate
	}
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix it"})
	})
	trace := traceRe.FindStringSubmatch(out)[1]
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, trace, "change_request"}) })

	err := cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m1", "--write"})
	if err == nil || !strings.Contains(err.Error(), "lower the spec's eval.min_n") || !strings.Contains(err.Error(), "shikii label") {
		t.Fatalf("calibrate must name both ways out (collect truths, or lower eval.min_n for a trial): %v", err)
	}
	err = cmdTune([]string{specPath, "--local", srv.URL, "--store", storeDir})
	if err == nil || !strings.Contains(err.Error(), "lower eval.min_n") || !strings.Contains(err.Error(), "--labeled FILE") {
		t.Fatalf("tune without --labeled must name the ways out and how a file is read: %v", err)
	}
	rows := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(rows, []byte(`[{"inputs":{"message":"fix it"},"truth":"change_request"}]`), 0o644); err != nil { // the same text as the store: one per input, so there is still one truth
		t.Fatal(err)
	}
	err = cmdTune([]string{specPath, "--local", srv.URL, "--store", storeDir, "--labeled", rows})
	if err == nil || strings.Contains(err.Error(), "--labeled") || !strings.Contains(err.Error(), "lower eval.min_n") {
		t.Fatalf("tune with --labeled already given must not suggest --labeled as the way out: %v", err)
	}
}

// (3) drift's ask agreement is n/a, not 0.000, when no truths answered on ask are in the window.
func TestDriftSaysNotApplicableWithoutAskLabels(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m1", map[string]float64{"change_request": 0.95, "question": 0.03, "other": 0.02})
	specPath := writeSpec(t, dir, "false")
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		tr := traceRe.FindStringSubmatch(out)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", tr, "change_request"}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m1", "--write"}) })
	out := capture(t, func() error { return cmdDrift([]string{specPath, "--store", storeDir, "--model", "m1"}) })
	if !strings.Contains(out, "ask agreement:    n/a (no ask-sourced labels") || strings.Contains(out, "ask agreement:    0.000") {
		t.Fatalf("no ask labels means n/a, not 0.000:\n%s", out)
	}
}

// (4) tune's run page links model and type pages only when they are in that wiki (so a new project's wiki gets no broken links).
func TestRunPageLinksOnlyToPagesTheWikiHas(t *testing.T) {
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	write := func() string {
		scores := []candScore{{name: "current", p: []float64{0.9, 0.8}, acc: 1}}
		cands := []candidate{{"current", sp}}
		path, err := writeRunPage(filepath.Join(dir, ".shikii", "wiki"), sp, "ggml-org/Kev-4B-GGUF:Q4_K_M", cands, scores, 0, false, "kept current", "nothing beats it", nil)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		return string(b)
	}
	page := write()
	if !strings.Contains(page, "model page: kev-4b ·") || !strings.Contains(page, "type: choice ·") || strings.Contains(page, "../models/") || strings.Contains(page, "../types/") {
		t.Fatalf("without the pages, the run names them as plain text:\n%s", page)
	}
	if !strings.Contains(page, "[intent](../specs/intent.md)") || !fileExists(filepath.Join(dir, ".shikii", "wiki", "specs", "intent.md")) {
		t.Fatalf("the spec page is always created, so it is always a link:\n%s", page)
	}
	for _, p := range []string{"models/kev-4b.md", "types/choice.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, ".shikii", "wiki", p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".shikii", "wiki", p), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	page = write()
	if !strings.Contains(page, "[kev-4b](../models/kev-4b.md)") || !strings.Contains(page, "[choice](../types/choice.md)") {
		t.Fatalf("with the pages present, the run links to them:\n%s", page)
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

// (6) bias runs on the same bank as calibrate / tune (the text of truths in the store). --labeled is not needed.
func TestBiasReadsStatesFromTheStoreLikeCalibrate(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	sp := mustLoad(t, specPath)
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	for i := 0; i < 4; i++ {
		in := map[string]string{"message": fmt.Sprintf("m%d", i)}
		l := store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Truth: "change_request", Weight: 1, Source: "ui", At: time.Now()}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	fair := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.45, "question": 0.40, "other": 0.15})
	out := capture(t, func() error {
		return cmdBias([]string{specPath, "--store", storeDir, "--local", fair.URL, "--permutations", "2"})
	})
	if !strings.Contains(out, "rows=4") || !strings.Contains(out, "\nok") {
		t.Fatalf("bias must take its states from the store's truths:\n%s", out)
	}
	err := cmdBias([]string{specPath, "--store", filepath.Join(dir, "empty"), "--local", fair.URL})
	if err == nil || !strings.Contains(err.Error(), "no states to ask") || !strings.Contains(err.Error(), "shikii label") {
		t.Fatalf("with nothing to ask, bias says where states come from: %v", err)
	}
	_ = spec.Choice
}
