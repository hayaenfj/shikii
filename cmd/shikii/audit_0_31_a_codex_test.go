package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// audit A: forms of the registration at the edges of "the three lines shikii wrote".
func TestAuditACodexToolsAllowedForms(t *testing.T) {
	own := "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n"
	cases := []struct {
		name, text string
		leave      bool // the file must be left as it is
	}{
		{"a command the person wrote (a wrapper script)", "[mcp_servers.shikii]\ncommand = \"/Users/me/bin/my-wrapper.sh\"\n" + codexServerArgsLine + "\n", true},
		{"a command the person wrote (another program)", "[mcp_servers.shikii]\ncommand = \"node\"\n" + codexServerArgsLine + "\n", true},
		{"CRLF line ends", strings.ReplaceAll(own, "\n", "\r\n"), true},
		{"no newline at the end", strings.TrimSuffix(own, "\n"), true},
		{"a comment the person added in the block", own + "# keep asking me\n", true},
		{"a comment the person added after the args", "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + " # mine\n", true},
		{"a key after a blank line (still the shikii table)", own + "\nenabled = false\n", true},
		{"a per-tool table elsewhere", own + "\n[features]\nx = 1\n\n[mcp_servers.shikii.tools.shikii_label]\napproval_mode = \"prompt\"\n", true},
		{"the block inside a multi-line string", "notes = \"\"\"\n" + own + "\"\"\"\n", true},
		{"a second block in a comment", "# [mcp_servers.shikii]\n" + own, true},
		{"a byte order mark", "\xef\xbb\xbf" + own, false},
		{"the person's default for all servers", "[mcp_servers]\ndefault_tools_approval_mode = \"prompt\"\n\n" + own, false},
	}
	for _, c := range cases {
		got, changed := codexToolsAllowed([]byte(c.text), codexServerArgs)
		t.Logf("%-55s changed=%v", c.name, changed)
		if c.leave && changed {
			t.Errorf("%s: the registration is not the three lines shikii wrote, and it got the approval line:\n%s", c.name, got)
		}
	}
}

// audit A: the person takes the approval line out again (the plain way to undo it), and init runs once more.
func TestAuditACodexApprovalLineComesBackAfterThePersonRemovedIt(t *testing.T) {
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
	capture(t, func() error { return cmdInit(nil) })
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), codexToolsAllowedLine) {
		t.Fatalf("precondition: init adds the line:\n%s", b)
	}
	// the person removes the line shikii added
	removed := strings.Replace(string(b), codexToolsAllowedLine+"\n", "", 1)
	if err := os.WriteFile(path, []byte(removed), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) }) // init again in the same project (how the skill is refreshed)
	after, _ := os.ReadFile(path)
	if strings.Contains(string(after), codexToolsAllowedLine) {
		t.Errorf("the person took the approval line out, and the next init put it back:\n%s\n---\n%.400s", after, out)
	}
}

// audit A: a config.toml the person made read-only: what hook install --yes says and does.
func TestAuditACodexReadOnlyConfigAndThePreview(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only files")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	own := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(path, []byte(own), 0o444); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	after, _ := os.ReadFile(path)
	if string(after) != own {
		t.Errorf("a read-only config.toml was changed:\n%s", after)
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "config.toml") {
			lines = append(lines, line)
		}
	}
	t.Logf("what hook install --yes says about config.toml:\n%s", strings.Join(lines, "\n"))
	if strings.Contains(out, "Codex will run shikii's MCP tools without asking each time") && !strings.Contains(out, "read-only") {
		t.Errorf("hook install --yes says the line is added, the file is read-only and stays as it is, and nothing says so")
	}
}

// audit A: config.toml kept elsewhere through a link.
func TestAuditACodexConfigThroughALink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dot := filepath.Join(t.TempDir(), "dotfiles")
	if err := os.MkdirAll(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dot, "config.toml")
	own := "model = \"x\"\n\n[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(real, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".codex", "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return allowCodexTools(link) })
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link is no longer a link: %v %v", info.Mode(), err)
	}
	b, _ := os.ReadFile(real)
	fi, _ := os.Stat(real)
	t.Logf("mode=%v\n%s", fi.Mode(), b)
	if !strings.Contains(string(b), codexToolsAllowedLine) || fi.Mode().Perm() != 0o600 {
		t.Errorf("the file the link points at gets the line and keeps its mode: %v\n%s", fi.Mode(), b)
	}
}

// audit A: the person switches the approval off by commenting the line out, and init runs once more.
func TestAuditACodexApprovalLineCommentedOutByThePerson(t *testing.T) {
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
	capture(t, func() error { return cmdInit(nil) })
	b, _ := os.ReadFile(path)
	edited := strings.Replace(string(b), codexToolsAllowedLine+"\n", "# "+codexToolsAllowedLine+"  (I want to be asked)\n", 1)
	if edited == string(b) {
		t.Fatalf("precondition: init adds the line:\n%s", b)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"init":               func() error { return cmdInit(nil) },
		"hook install --yes": func() error { return hookInstall([]string{"--yes"}, true) },
	} {
		capture(t, run)
		after, _ := os.ReadFile(path)
		if string(after) != edited {
			t.Errorf("%s changed a registration the person edited (a line of theirs is in the block):\n%s", name, after)
		}
		if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// audit A: uninstall takes the registration out with the approval line; a TOML that cannot be read is left as it is.
func TestAuditACodexUninstallAndUnreadable(t *testing.T) {
	own := "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n"
	text := "model = \"x\"\n\n" + own + "\n[mcp_servers.other]\ncommand = \"other\"\n"
	out, found, err := withoutCodexServer(text)
	if err != nil || !found || strings.Contains(out, "shikii") || strings.Contains(out, "default_tools_approval_mode") || !strings.Contains(out, "[mcp_servers.other]") || !strings.Contains(out, "model = \"x\"") {
		t.Errorf("uninstall removes the block with its approval line and keeps the rest: %v %v\n%s", found, err, out)
	}
	broken := "model = \n\n[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n"
	if got, changed := codexToolsAllowed([]byte(broken), codexServerArgs); changed {
		t.Errorf("a config.toml that is not TOML got the line:\n%s", got)
	}
	older := "[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\nargs = [\"mcp\", \"--store\", \".shikii\", \"--local\", \"http://127.0.0.1:8080\"]\n"
	if got, changed := codexToolsAllowed([]byte(older), codexServerArgs); changed {
		t.Errorf("an older version's form got the line:\n%s", got)
	}
}
