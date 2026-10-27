package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

// Holes found in the second audit (tune / propose / adopt). Each was confirmed to fail before the fix.

// Tainted text never goes into the "examples it got wrong" sent to the proposer unless the proposer is local (loopback).
func TestProposerPromptDropsTaintedExamplesUnlessLocal(t *testing.T) {
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	bank := []bankRow{
		{inputs: map[string]string{"message": "why PUBLIC-ROW"}, truth: "question", weight: 1},
		{inputs: map[string]string{"message": "why SECRET-PROD-ROW"}, truth: "question", weight: 1, tainted: true},
	}
	cur := candScore{name: "current", correct: []bool{false, false}, weights: []float64{1, 1}, confusion: map[string]map[string]float64{"question": {"change_request": 2}}}
	_, remote := buildProposerPrompt(sp, cur, bank, "", 2, false)
	if strings.Contains(remote, "SECRET-PROD-ROW") || !strings.Contains(remote, "PUBLIC-ROW") {
		t.Fatalf("a remote proposer must see the public example but never the tainted one:\n%s", remote)
	}
	_, local := buildProposerPrompt(sp, cur, bank, "", 2, true)
	if !strings.Contains(local, "SECRET-PROD-ROW") {
		t.Fatalf("a local proposer may see tainted examples:\n%s", local)
	}
}

// The hosted key (TypeSafe / OpenRouter) is never sent to the proposer. The proposer's key is only SHIKII_PROPOSER_KEY.
func TestProposerNeverGetsTheHostedKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "sk-typesafe-SECRET")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_PROPOSER_KEY", "")
	if a := proposerAuth("https://api.example.invalid/v1"); a != "" {
		t.Fatalf("the hosted key must not be sent to a proposer, got %q", a)
	}
	t.Setenv("SHIKII_PROPOSER_KEY", "pk-proposer")
	if a := proposerAuth("https://api.example.invalid/v1"); a != "pk-proposer" {
		t.Fatalf("SHIKII_PROPOSER_KEY is the proposer's key, got %q", a)
	}
}

// Every local form is loopback. Remote ones are not.
func TestIsLoopbackForms(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:8087", "http://localhost:8087", "http://localhost.:8087", "http://[::1]:8087", "http://127.0.0.2:1", "http://0.0.0.0:8087", "http://[::ffff:127.0.0.1]:1", "localhost:8087", "127.0.0.1:8087/v1"} {
		if !isLoopback(u) {
			t.Fatalf("%s must count as local", u)
		}
	}
	for _, u := range []string{"https://api.example.invalid/v1", "http://10.0.0.1:8087", "http://127.0.0.1.evil.example", "http://localhost.evil.example"} {
		if isLoopback(u) {
			t.Fatalf("%s must not count as local", u)
		}
	}
}

// --propose N --write in the terminal does not adopt AI-made wording no person has seen. The candidates stay in variants and can be adopted with tune --write after looking.
func TestProposeWithWriteDoesNotAdoptUnseenWording(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	model := wordingSensitiveModel(t)
	proposer, _ := fakeProposer(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	rows := bankRows()
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir, "--propose", "2", "--proposer", proposer.URL, "--write"})
	})
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatalf("a proposed wording must not be adopted unseen:\n%s", out)
	}
	if !strings.Contains(out, "winner: proposed-strict") || !strings.Contains(out, "not adopted") {
		t.Fatalf("the person must be told the winner and that it waits for their check:\n%s", out)
	}
	// The candidates remain, so they can be adopted after looking
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir, "--write"})
	})
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err != nil {
		t.Fatal("after a person has seen the variants, tune --write adopts the winner")
	}
	res, err := latestTuneResult(filepath.Join(".shikii", "wiki"), "intent")
	if err != nil || len(res.MeaningChecks) != 1 || res.MeaningChecks[0].By != "terminal --write" {
		t.Fatalf("the terminal adoption must be recorded as the person's meaning check: %v %+v", err, res)
	}
}

// Find the JSON array even if the proposer's preamble has a [.
func TestParseProposalsSkipsBracketsInProse(t *testing.T) {
	props, err := parseProposals("Here are [two] ideas:\n```json\n[{\"name\":\"a\",\"question\":\"Q?\"}]\n```")
	if err != nil || len(props) != 1 || props[0].Name != "a" {
		t.Fatalf("got %v %+v", err, props)
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

// The proposer can also propose a split (steps). A badly shaped split is rejected and the reason is kept.
func TestProposalsMayCarrySteps(t *testing.T) {
	sp := mustLoad(t, writeSpec(t, t.TempDir(), "false"))
	props := []proposal{
		{Name: "two-step", Steps: []spec.Step{{Ask: "Any work?", No: "other"}, {Ask: "Change files?", Yes: "change_request", No: "question"}}},
		{Name: "broken", Steps: []spec.Step{{Ask: "Any work?", No: "other"}}},
	}
	added, skipped, notes := adoptProposals(sp, props)
	if added != 1 || skipped != 1 || len(sp.Variants) != 1 || len(sp.Variants[0].Steps) != 2 {
		t.Fatalf("one valid steps proposal must land in variants, the broken one must be skipped: added=%d skipped=%d %+v\n%v", added, skipped, sp.Variants, notes)
	}
	system, _ := buildProposerPrompt(sp, candScore{confusion: map[string]map[string]float64{}}, nil, "", 2, true)
	if !strings.Contains(system, "steps") {
		t.Fatalf("the proposer must be told it may decompose a choice into yes/no steps:\n%s", system)
	}
}
