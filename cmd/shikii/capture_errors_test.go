package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Behaviour check 0.9.2 (Claude Code): a wrong wiki capture input was answered with ingest's error and no field name,
// and the agent gave up keeping the whole log. The errors now name capture and say what is wrong.
func TestCaptureErrorsSayWhatIsWrong(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	for input, want := range map[string][]string{
		`{"kind":"test","title":"t","content":"c","reference":"r","observed_at":"2026-10-26T00:00:00Z"}`:                 {"capture", "version"},
		`{"kind":"result","title":"t","content":"c","reference":"r","version":"v","observed_at":"2026-10-26T00:00:00Z"}`: {"capture", "kind"},
		`{"kind":"test","title":"t","body":"c"}`: {"capture", "body"},
	} {
		var err error
		withStdin(t, input, func() { err = cmdNotebook([]string{"capture"}) })
		for _, w := range want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Fatalf("the capture error says %q: %v (input %s)", w, err, input)
			}
		}
		if strings.Contains(err.Error(), "ingest") {
			t.Fatalf("a capture error does not speak of ingest: %v", err)
		}
	}
}
