package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shikii init on a machine where shikii hook install was never run (go install and then init, or a fresh HOME) prepared the project
// and then said "Ready", "the hook starts the page on the agent's first request" and "make requests in this project as usual",
// although no agent had a shikii hook: nothing was recorded and no page started, and nothing said why.
//
// The first project on a machine now gets the hooks from init itself (what hook install --yes does), so one command is enough for
// someone new. A machine that already has projects and no hooks belongs to someone who uses shikii without them (MCP and the skill
// only; the README says that running init again there leaves the hooks disabled): init leaves them out and says which command
// installs them. The page and the recording are promised only when a hook is there.
func TestInitInstallsTheHooksForTheFirstProjectOnAMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	first := gitRepo(t)
	t.Chdir(first)
	shikiiHooks := func() []string { return agentsWithShikiiHook(home) }

	// another tool's hook is already in the person's settings: it stays, and it is not a shikii hook
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	for _, w := range []string{"Ready: ", "Hooks: none installed and this is the first project on this machine", "hooks added (Claude Code)", "hooks added (Codex)", "hook uninstall --yes", "Page: the hook starts it on the agent's first request", "Next: make requests in this project as usual"} {
		if !strings.Contains(out, w) {
			t.Errorf("init of the first project on a machine lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "Hooks: not installed") {
		t.Errorf("init of the first project says the hooks are missing:\n%s", out)
	}
	if got := shikiiHooks(); len(got) != 2 {
		t.Errorf("init of the first project installs the hooks for Claude Code and Codex: %v", got)
	}
	if settings := string(mustRead(t, filepath.Join(home, ".claude", "settings.json"))); !strings.Contains(settings, "rtk hook claude") {
		t.Errorf("the other tool's hook stays: %s", settings)
	}

	// running init again changes nothing about the hooks
	out = capture(t, func() error { return cmdInit(nil) })
	if strings.Contains(out, "Hooks: ") || !strings.Contains(out, "Next: make requests in this project as usual") {
		t.Errorf("init with the hooks in place only promises what they do:\n%s", out)
	}

	// the person removes the hooks: this machine now uses shikii without them, and init keeps it that way, in this project and a new one
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if got := shikiiHooks(); len(got) != 0 {
		t.Fatalf("hook uninstall removes the hooks: %v", got)
	}
	second := gitRepo(t)
	for _, repo := range []string{first, second} {
		t.Chdir(repo)
		out = capture(t, func() error { return cmdInit(nil) })
		for _, w := range []string{"Ready: ", "Hooks: not installed", "~/.claude/settings.json", "~/.codex/hooks.json", "requests in this project are not recorded", " hook install --yes"} {
			if !strings.Contains(out, w) {
				t.Errorf("init on a machine that has projects and no hooks lacks %q:\n%s", w, out)
			}
		}
		for _, w := range []string{"Page: the hook starts it", "Next: make requests in this project as usual", "hooks added"} {
			if strings.Contains(out, w) {
				t.Errorf("init on a machine that has projects and no hooks still says %q:\n%s", w, out)
			}
		}
		if got := shikiiHooks(); len(got) != 0 {
			t.Errorf("init on a machine that has projects and no hooks leaves them out: %v", got)
		}
	}

	// a shikii hook a person wrote in another form counts: the request reaches shikii
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"~/bin/shikii hook user-prompt-submit"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := capture(t, func() error { return cmdInit(nil) }); strings.Contains(out, "Hooks: not installed") || !strings.Contains(out, "Next: make requests in this project as usual") {
		t.Errorf("a shikii hook written by hand is a hook:\n%s", out)
	}
}

// A settings file shikii cannot read as JSON stops hook install; init says so and still finishes the project.
func TestInitFinishesWhenTheHooksCannotBeInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(gitRepo(t))
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"hooks":{"Stop":"not a list"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	for _, w := range []string{"Ready: ", "Hooks: could not be installed", "hook install --yes"} {
		if !strings.Contains(out, w) {
			t.Errorf("init with settings shikii cannot edit lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "Next: make requests in this project as usual") {
		t.Errorf("init without the hooks does not promise the recording:\n%s", out)
	}
}
