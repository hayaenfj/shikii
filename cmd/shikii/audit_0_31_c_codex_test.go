package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// audit C (0.31.0, second round), finding A-7 (INVARIANT 12): commands a person wrote that start with the word shikii (a wrapper that
// sets the environment and starts shikii is most naturally named after it). Only three forms are a shikii binary: the bare name,
// the path shikii writes, and an absolute path to a file named exactly shikii.
func TestAuditCCodexCommandsNamedAfterShikii(t *testing.T) {
	for _, command := range []string{"/Users/me/bin/shikii-wrapper.sh", "/Users/me/bin/shikii.sh", "/opt/tools/shikii_with_key", "/usr/local/bin/shikiiX", "shikii-wrapper.sh", "./shikii", "bin/shikii"} {
		text := "[mcp_servers.shikii]\ncommand = \"" + command + "\"\n" + codexServerArgsLine + "\n"
		_, changed := codexToolsAllowed([]byte(text), codexServerArgs)
		t.Logf("%-40s changed=%v", command, changed)
		if changed {
			t.Errorf("INVARIANT 12: %s is a command the person wrote (not the shikii binary), and the registration got the approval line", command)
		}
	}
	// control: the names shikii itself writes
	for _, command := range []string{"shikii", "/opt/shikii", mcpCommand()} {
		text := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{command}) + "\n" + codexServerArgsLine + "\n"
		if _, changed := codexToolsAllowed([]byte(text), codexServerArgs); !changed {
			t.Errorf("control: %s is a shikii binary and gets the line", command)
		}
	}
}

// The same line where it is used: a wrapper of the person's that exists is their registration. Neither init nor hook install says
// the line will be added, adds it, or notes it.
func TestAuditCCodexWrapperOfThePersonIsLeftByInitAndHookInstall(t *testing.T) {
	home := freshMachine(t)
	wrapper := filepath.Join(home, "bin", "shikii-wrapper.sh")
	if err := os.MkdirAll(filepath.Dir(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec shikii \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	theirs := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{wrapper}) + "\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(path, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"init", func() error { return cmdInit(nil) }},
		{"hook install", func() error { return hookInstall(nil, true) }},
		{"hook install --yes", func() error { return hookInstall([]string{"--yes"}, true) }},
	} {
		out := capture(t, c.run)
		after, _ := os.ReadFile(path)
		if string(after) != theirs || strings.Contains(out, "without asking each time") || strings.Contains(out, "without Codex asking") || fileExists(codexToolsAllowedMark()) {
			t.Errorf("%s: a registration whose command is the person's wrapper is changed, announced or noted (note=%v):\n%s\n%s", c.name, fileExists(codexToolsAllowedMark()), after, out)
		}
	}
}

func auditCCodexMachine(t *testing.T) (home, path, project string) {
	t.Helper()
	home = freshMachine(t)
	path = filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	project = filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	capture(t, func() error { return cmdInit(nil) })
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), codexToolsAllowedLine) || !fileExists(codexToolsAllowedMark()) {
		t.Fatalf("precondition: init adds the line and notes it:\n%s", b)
	}
	return home, path, project
}

// audit C, finding U-3: the note is kept under ~/.shikii/agent. The person empties ~/.shikii (to start shikii over) and keeps
// ~/.codex; the line is there, the note is not. Later the person deletes the line.
func TestAuditCCodexLineWithoutItsNote(t *testing.T) {
	home, path, _ := auditCCodexMachine(t)
	if err := os.RemoveAll(filepath.Join(home, ".shikii")); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return cmdInit(nil) }) // shikii is set up again; the registration has its four lines
	b, _ := os.ReadFile(path)
	t.Logf("after the second setup: line=%v note=%v", strings.Contains(string(b), codexToolsAllowedLine), fileExists(codexToolsAllowedMark()))
	if !strings.Contains(string(b), codexToolsAllowedLine) {
		t.Fatalf("precondition: the line is still there:\n%s", b)
	}
	if !fileExists(codexToolsAllowedMark()) {
		t.Errorf("the registration has the line shikii adds and the note is gone: the second setup writes the note again")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), codexToolsAllowedLine+"\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return cmdInit(nil) })
	after, _ := os.ReadFile(path)
	if strings.Contains(string(after), codexToolsAllowedLine) {
		t.Errorf("the line shikii added once was deleted by the person, and init added it again (the registration had the line and no note, so nothing remembered it):\n%s", after)
	}
}

// The other side of U-3: the note is written for shikii's own block with the line, and for no other block that has the line. A
// command or a line of the person's makes the block theirs: nothing is noted, nothing is said, and the file stays as it is.
func TestAuditCCodexNoteOnlyForShikiisOwnBlockWithTheLine(t *testing.T) {
	own := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n"
	other := "[mcp_servers.other]\ncommand = \"other\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n"
	for _, c := range []struct {
		name, text string
		note       bool
	}{
		{"shikii's own block with the line", own + codexToolsAllowedLine + "\n", true},
		{"the same between the person's settings and another server with the same lines", "model = \"x\"\n\n" + other + "\n" + own + codexToolsAllowedLine + "\n\n[features]\nhooks = true\n", true},
		{"a wrapper of the person's as the command", "[mcp_servers.shikii]\ncommand = \"/Users/me/bin/shikii-wrapper.sh\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n", false},
		{"a line of the person's after the approval line", own + codexToolsAllowedLine + "\nstartup_timeout_sec = 20\n", false},
		{"a comment of the person's after the approval line", own + codexToolsAllowedLine + "\n# keep it\n", false},
		{"the person's own choice in its place", own + "default_tools_approval_mode = \"prompt\"\n", false},
		{"the line commented out", own + "# " + codexToolsAllowedLine + "\n", false},
		{"args the person changed", "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\nargs = [\"mcp\", \"--store\", \"/p/.shikii\"]\n" + codexToolsAllowedLine + "\n", false},
		{"a file that is not TOML", "model = \n\n" + own + codexToolsAllowedLine + "\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := freshMachine(t)
			path := filepath.Join(home, ".codex", "config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(c.text), 0o644); err != nil {
				t.Fatal(err)
			}
			out := capture(t, func() error { return allowCodexTools(path) })
			after, _ := os.ReadFile(path)
			if string(after) != c.text || out != "" {
				t.Errorf("a registration that has the line is changed or announced:\n%s\n%s", after, out)
			}
			if got := fileExists(codexToolsAllowedMark()); got != c.note {
				t.Errorf("note written=%v, want %v", got, c.note)
			}
		})
	}
}

// audit C, finding U-4 (GREEN in the candidate): the note and the line in the cases the fix names. Each case fails when its line
// of the fix is taken out (the preview's check of the note, the note taken back after a failed write, the note removed for a
// registration written anew).
func TestAuditCCodexNoteAndLineCases(t *testing.T) {
	t.Run("the note is there and the line is not: nothing is added, nothing is announced", func(t *testing.T) {
		_, path, _ := auditCCodexMachine(t)
		b, _ := os.ReadFile(path)
		removed := strings.Replace(string(b), codexToolsAllowedLine+"\n", "", 1)
		if err := os.WriteFile(path, []byte(removed), 0o644); err != nil {
			t.Fatal(err)
		}
		for name, run := range map[string]func() error{
			"init":               func() error { return cmdInit(nil) },
			"hook install":       func() error { return hookInstall(nil, true) },
			"hook install --yes": func() error { return hookInstall([]string{"--yes"}, true) },
		} {
			out := capture(t, run)
			after, _ := os.ReadFile(path)
			if string(after) != removed || strings.Contains(out, "without asking each time") || strings.Contains(out, "without Codex asking") {
				t.Errorf("%s: the registration the person took the line out of is changed or announced:\n%s\n%s", name, after, out)
			}
		}
	})
	t.Run("the note was written and config.toml cannot be written: the note is taken back and the person is told", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes read-only files")
		}
		home := freshMachine(t)
		path := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		own := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n"
		if err := os.WriteFile(path, []byte(own), 0o444); err != nil {
			t.Fatal(err)
		}
		out := capture(t, func() error { return allowCodexTools(path) })
		after, _ := os.ReadFile(path)
		if string(after) != own || fileExists(codexToolsAllowedMark()) || !strings.Contains(out, "left as it is") || !strings.Contains(out, codexToolsAllowedLine) {
			t.Errorf("a read-only config.toml: file changed=%v, note left=%v, said=%q", string(after) != own, fileExists(codexToolsAllowedMark()), out)
		}
	})
	t.Run("after uninstall, a new init writes the registration with the line", func(t *testing.T) {
		_, path, _ := auditCCodexMachine(t)
		b, _ := os.ReadFile(path)
		if err := os.WriteFile(path, []byte(strings.Replace(string(b), codexToolsAllowedLine+"\n", "", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
		if b, _ = os.ReadFile(path); strings.Contains(string(b), "shikii") {
			t.Fatalf("precondition: uninstall removes the registration:\n%s", b)
		}
		capture(t, func() error { return cmdInit(nil) })
		if b, _ = os.ReadFile(path); !strings.Contains(string(b), codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n") || !fileExists(codexToolsAllowedMark()) {
			t.Errorf("a registration shikii writes anew gets the line and the note: note=%v\n%s", fileExists(codexToolsAllowedMark()), b)
		}
	})
}

// audit C, finding U-4 (GREEN in the candidate; fails with init.go's "installed for ..., and the rest failed" branch taken out):
// the install of the first project's hooks stops half-way, and init says who has the hooks and goes on.
func TestAuditCInitSaysWhoGotTheHooks(t *testing.T) {
	home := freshMachine(t)
	t.Chdir(gitRepo(t))
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(`{"hooks":{"Stop":"not a list"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	got := agentsWithShikiiHook(home)
	if len(got) != 1 || got[0] != "Claude Code" {
		t.Fatalf("precondition: the hooks are installed for Claude Code only: %v", got)
	}
	if !strings.Contains(out, "Hooks: installed for Claude Code, and the rest failed") || strings.Contains(out, "Hooks: could not be installed") || !strings.Contains(out, "Page: the hook starts it") {
		t.Errorf("init says the hooks are installed for Claude Code, that the rest failed, and goes on to the page:\n%s", out)
	}
}
