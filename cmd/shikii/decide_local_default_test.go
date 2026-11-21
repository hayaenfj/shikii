package main

import (
	"os"
	"strings"
	"testing"
)

// A tool that asks shikii decide without --local (chiten) reaches the local model the hooks use, so a question whose send_to is
// local only is decided there instead of abstaining for lack of an endpoint. --local still chooses another server.
func TestDecideUsesTheHooksLocalModelWhenNoLocalIsGiven(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("DECISIONS_API_KEY", "")
	dir := t.TempDir()
	specPath := writeNoulSpec(t, dir)
	hooks := rawAnswerModel(t, `{"type":"noul","noul":0.9}`)
	old := hookLocalURL
	hookLocalURL = hooks.URL
	t.Cleanup(func() { hookLocalURL = old })
	out := capture(t, func() error { return cmdDecide([]string{specPath, "--no-record", "-i", "a=x"}) })
	if !strings.Contains(out, "endpoint=local") || !strings.Contains(out, "p=0.900") {
		t.Fatalf("without --local the hooks' local model answers:\n%s", out)
	}
	other := rawAnswerModel(t, `{"type":"noul","noul":0.2}`)
	out = capture(t, func() error { return cmdDecide([]string{specPath, "--no-record", "--local", other.URL, "-i", "a=x"}) })
	if !strings.Contains(out, "p=0.200") {
		t.Fatalf("--local chooses the server:\n%s", out)
	}
}

// The default only fills the case where no endpoint was permitted. With a key set, a spec that lists local before hosted still goes
// to hosted as before, instead of stopping at a local model that is not running (decide does not fall back after a failed call).
func TestDecideKeepsHostedWhenTheDefaultLocalModelIsNotNeeded(t *testing.T) {
	hosted := rawAnswerModel(t, `{"type":"noul","noul":0.6}`)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_MODEL", "")
	dir := t.TempDir()
	specPath := writeNoulSpec(t, dir)
	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(strings.Replace(string(b), "send_to: [local]", "send_to: [local, hosted]", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	old := hookLocalURL
	hookLocalURL = "http://127.0.0.1:1" // nothing listens here
	t.Cleanup(func() { hookLocalURL = old })
	out := capture(t, func() error { return cmdDecide([]string{specPath, "--no-record", "--hosted", hosted.URL, "-i", "a=x"}) })
	if !strings.Contains(out, "endpoint=hosted") || !strings.Contains(out, "p=0.600") {
		t.Fatalf("with a key, [local, hosted] goes to hosted when no --local is given:\n%s", out)
	}
	// a spec that allows only local still reaches the default local model with a key set
	local := rawAnswerModel(t, `{"type":"noul","noul":0.3}`)
	hookLocalURL = local.URL
	if err := os.WriteFile(specPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdDecide([]string{specPath, "--no-record", "--hosted", hosted.URL, "-i", "a=x"}) })
	if !strings.Contains(out, "endpoint=local") || !strings.Contains(out, "p=0.300") {
		t.Fatalf("a local-only spec reaches the default local model even with a key:\n%s", out)
	}
}

// A tainted session never sends to hosted, so with a key set a [local, hosted] spec has no permitted endpoint until the default
// local model is added: the pre-check must see the session as tainted, or the decision abstains.
func TestDecideAddsTheDefaultLocalModelForATaintedSession(t *testing.T) {
	hosted := rawAnswerModel(t, `{"type":"noul","noul":0.6}`)
	local := rawAnswerModel(t, `{"type":"noul","noul":0.3}`)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_MODEL", "")
	dir := t.TempDir()
	specPath := writeNoulSpec(t, dir)
	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(strings.Replace(string(b), "send_to: [local]", "send_to: [local, hosted]", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	old := hookLocalURL
	hookLocalURL = local.URL
	t.Cleanup(func() { hookLocalURL = old })
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--no-record", "--tainted", "--hosted", hosted.URL, "-i", "a=x"})
	})
	if !strings.Contains(out, "endpoint=local") || !strings.Contains(out, "p=0.300") {
		t.Fatalf("a tainted session reaches the default local model instead of abstaining:\n%s", out)
	}
}
