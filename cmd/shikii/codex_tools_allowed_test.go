package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex asks the person before every call of an MCP tool, and codex exec, which has nobody to ask, refuses the call ("MCP tool
// call requires approval, but approval policy is never", 2026-11-12): shikii's server was connected and its tools could not be
// used without a click each time. shikii now adds default_tools_approval_mode = "approve" to the server block it wrote itself, so
// Codex runs shikii's tools like its shell commands. Only shikii's own block gets the line: a block a person edited (their own
// approval choice included), an older form, or a block that already has the line is left as it is, and every other setting stays.
func TestCodexRunsShikiisToolsWithoutAsking(t *testing.T) {
	own := "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n"
	for name, c := range map[string]struct {
		text string
		want string // "" when the file is left as it is
	}{
		"shikii's own block":                {text: own, want: own + codexToolsAllowedLine + "\n"},
		"next to a person's own settings":   {text: "model = \"x\"\n\n" + own + "\n[mcp_servers.other]\ncommand = \"other\"\n", want: "model = \"x\"\n\n" + own + codexToolsAllowedLine + "\n\n[mcp_servers.other]\ncommand = \"other\"\n"},
		"a table right after the block":     {text: own + "[features]\nhooks = true\n", want: own + codexToolsAllowedLine + "\n[features]\nhooks = true\n"},
		"already allowed":                   {text: own + codexToolsAllowedLine + "\n"},
		"the person chose to be asked":      {text: own + "default_tools_approval_mode = \"prompt\"\n"},
		"a line the person added":           {text: own + "startup_timeout_sec = 20\n"},
		"a sub-table the person added":      {text: own + "\n[mcp_servers.shikii.env]\nSHIKII_MODEL = \"local\"\n"},
		"args the person changed":           {text: "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\nargs = [\"mcp\", \"--store\", \"/p/.shikii\"]\n"},
		"no shikii server":                  {text: "model = \"x\"\n"},
		"shikii written as an inline table": {text: "mcp_servers = { shikii = { command = \"shikii\", args = [\"mcp\"] } }\n"},
	} {
		got, changed := codexToolsAllowed([]byte(c.text), codexServerArgs)
		if changed != (c.want != "") || changed && string(got) != c.want {
			t.Errorf("%s: changed=%v\n%s", name, changed, got)
		}
		if !changed {
			continue
		}
		m, err := readCodexConfig(got)
		server, _ := codexShikiiServer(m)
		if table, _ := server.(map[string]any); err != nil || table["default_tools_approval_mode"] != "approve" || table["command"] != "/opt/shikii" {
			t.Errorf("%s: Codex reads the server with its command and the approval: %v %v", name, err, server)
		}
	}

	// the block with the line is still shikii's own: a command that no longer exists is pointed at this binary, and the line stays
	moved := "[mcp_servers.shikii]\ncommand = \"shikii\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n"
	fixed, ok := codexServerFixed([]byte(moved), codexServerArgs, "/opt/shikii")
	if want := "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n"; !ok || string(fixed) != want {
		t.Errorf("a block with the approval line is still fixed as shikii's own: %v\n%s", ok, fixed)
	}

	// init writes it: a new registration comes with the line, says so, and a second init changes nothing
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := addCodexServer(path, codexServerArgs); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return allowCodexTools(path) })
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n") || !strings.Contains(out, "without Codex asking") || !strings.Contains(out, "prompt") {
		t.Errorf("the registration gets the line and the person is told how to be asked again:\n%s\n%s", b, out)
	}
	if out := capture(t, func() error { return allowCodexTools(path) }); out != "" {
		t.Errorf("a second run changes and says nothing: %s", out)
	}
	if after, _ := os.ReadFile(path); string(after) != string(b) {
		t.Errorf("a second run leaves the file as it is:\n%s", after)
	}
	if note := codexServerNote(path); note != "shikii is already registered (Codex)" {
		t.Errorf("a registration with the line is shikii's registration: %s", note)
	}
}

// shikii init and hook install both add the line: a new registration gets it at init, and the registration an earlier version
// wrote gets it at the next hook install.
func TestInitAndHookInstallLetCodexRunShikiisTools(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	out := capture(t, func() error { return cmdInit(nil) })
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n") || !strings.Contains(out, "without Codex asking") {
		t.Fatalf("init registers shikii in Codex with the approval line and says so:\n%s\n%s", b, out)
	}
	// the registration as an earlier version left it
	earlier := strings.Replace(string(b), codexToolsAllowedLine+"\n", "", 1)
	if err := os.WriteFile(path, []byte(earlier), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	if b, _ = os.ReadFile(path); !strings.Contains(string(b), codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n") || !strings.Contains(out, "without Codex asking") || !strings.Contains(out, "Codex will run shikii's MCP tools without asking each time (adds") {
		t.Errorf("hook install adds the line to an earlier registration and says so:\n%s\n%s", b, out)
	}
	// a second project on a machine that has the hooks: init itself adds the line (no hook install runs there)
	if err := os.WriteFile(path, []byte(earlier), 0o644); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "second")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(second)
	out = capture(t, func() error { return cmdInit(nil) })
	if b, _ = os.ReadFile(path); !strings.Contains(string(b), codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n") || !strings.Contains(out, "without Codex asking") || strings.Contains(out, "shikii installs them now") {
		t.Errorf("init of a later project adds the line itself:\n%s\n%s", b, out)
	}
}
