package main

import (
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
