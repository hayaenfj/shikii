package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shikii init on a machine where shikii hook install was never run (go install and then init, or a fresh HOME) prepared the project
// and then said "Ready", "the hook starts the page on the agent's first request" and "make requests in this project as usual",
// although no agent had a shikii hook: nothing was recorded and no page started, and nothing said why. init does not install the
// hooks itself (hook install shows first what it changes in the person's settings, and takes --yes), so it now says that no hook
// is installed and which command installs them, and promises the page and the recording only when a hook is there.
func TestInitSaysWhenNoHookIsInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := gitRepo(t)
	t.Chdir(repo)
	out := capture(t, func() error { return cmdInit(nil) })
	for _, w := range []string{"Ready: ", "Hooks: not installed", "~/.claude/settings.json", "~/.codex/hooks.json", "requests in this project are not recorded", " hook install --yes"} {
		if !strings.Contains(out, w) {
			t.Errorf("init without hooks lacks %q:\n%s", w, out)
		}
	}
	for _, w := range []string{"Page: the hook starts it", "Next: make requests in this project as usual"} {
		if strings.Contains(out, w) {
			t.Errorf("init without hooks still promises %q:\n%s", w, out)
		}
	}
	// a settings file with other tools' hooks only is still no shikii hook
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := capture(t, func() error { return cmdInit(nil) }); !strings.Contains(out, "Hooks: not installed") {
		t.Errorf("another tool's hook is not a shikii hook:\n%s", out)
	}
	// a shikii hook a person wrote in another form counts: the request reaches shikii
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"~/bin/shikii hook user-prompt-submit"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := capture(t, func() error { return cmdInit(nil) }); strings.Contains(out, "Hooks: not installed") || !strings.Contains(out, "Next: make requests in this project as usual") {
		t.Errorf("a shikii hook written by hand is a hook:\n%s", out)
	}

	// with the hooks shikii installs, init promises what the hook does
	installed := gitRepo(t)
	hookEnv(t, nil, installed)
	t.Chdir(installed)
	out = capture(t, func() error { return cmdInit(nil) })
	for _, w := range []string{"Ready: ", "Page: the hook starts it on the agent's first request", "Next: make requests in this project as usual"} {
		if !strings.Contains(out, w) {
			t.Errorf("init with hooks lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "Hooks: not installed") {
		t.Errorf("init with hooks says they are missing:\n%s", out)
	}
}
