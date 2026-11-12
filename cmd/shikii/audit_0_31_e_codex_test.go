package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// audit E: over many forms of ~/.codex/config.toml, with and without the note, the preview of hook install says the approval line
// will be added exactly when hook install --yes adds it, the install changes nothing but that line (and a command that is gone),
// and a second install changes nothing more.
func TestAuditECodexPreviewAndInstallOverManyForms(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only files")
	}
	args := codexServerArgsLine
	exe := quotedList([]string{mcpCommand()})
	block := func(command string) string { return "[mcp_servers.shikii]\ncommand = " + command + "\n" + args + "\n" }
	type form struct {
		name    string
		text    func(dir string) string
		note    bool // the note that the line was added once
		mode    os.FileMode
		link    bool // config.toml is a link to a file kept elsewhere
		wantAdd bool
	}
	forms := []form{
		{name: "shikii's three lines, this binary", text: func(string) string { return block(exe) }, wantAdd: true},
		{name: "shikii's three lines, the bare name", text: func(string) string { return block(`"shikii"`) }, wantAdd: true},
		{name: "three lines, the note is there (the person took the line out)", text: func(string) string { return block(exe) }, note: true},
		{name: "the line is there, no note", text: func(string) string { return block(exe) + codexToolsAllowedLine + "\n" }},
		{name: "another table right after", text: func(string) string { return block(exe) + "[other]\nx = 1\n" }, wantAdd: true},
		{name: "a blank line and another table", text: func(string) string { return "model = \"x\"\n\n" + block(exe) + "\n[other]\nx = 1\n" }, wantAdd: true},
		{name: "a comment of the person's after args", text: func(string) string { return block(exe) + "# keep asking\n" }},
		{name: "a key of the person's after a blank line", text: func(string) string { return block(exe) + "\nstartup_timeout_sec = 20\n" }},
		{name: "a sub-table of the person's right after", text: func(string) string { return block(exe) + "[mcp_servers.shikii.env]\nX = \"1\"\n" }},
		{name: "the person wrote prompt", text: func(string) string { return block(exe) + "default_tools_approval_mode = \"prompt\"\n" }},
		{name: "a wrapper that exists, named after shikii", text: func(dir string) string { return block(quotedList([]string{filepath.Join(dir, "shikii-wrapper.sh")})) }},
		{name: "another program that exists", text: func(dir string) string { return block(quotedList([]string{filepath.Join(dir, "node")})) }},
		{name: "a file named exactly shikii elsewhere", text: func(dir string) string { return block(quotedList([]string{filepath.Join(dir, "shikii")})) }, wantAdd: true},
		{name: "a link named shikii that points at a wrapper", text: func(dir string) string { return block(quotedList([]string{filepath.Join(dir, "linked", "shikii")})) }, wantAdd: true},
		{name: "a Windows path", text: func(string) string { return block(`"C:\\Users\\me\\bin\\shikii.exe"`) }},
		{name: "a relative path", text: func(string) string { return block(`"./shikii"`) }},
		{name: "a literal string", text: func(string) string { return "[mcp_servers.shikii]\ncommand = 'shikii'\n" + args + "\n" }},
		{name: "a comment at the end of the args line", text: func(string) string { return "[mcp_servers.shikii]\ncommand = " + exe + "\n" + args + " # mine\n" }},
		{name: "line breaks of Windows", text: func(string) string { return strings.ReplaceAll(block(exe), "\n", "\r\n") }},
		{name: "a byte order mark first", text: func(string) string { return "\xef\xbb\xbf" + block(exe) }, wantAdd: true},
		{name: "no line break at the end", text: func(string) string { return strings.TrimSuffix(block(exe), "\n") }},
		{name: "the header in a comment above another table's keys", text: func(string) string {
			return "[mcp_servers.other]\n# [mcp_servers.shikii]\ncommand = " + exe + "\n" + args + "\n"
		}},
		{name: "read-only", text: func(string) string { return block(exe) }, mode: 0o444},
		{name: "a link to a file kept elsewhere", text: func(string) string { return block(exe) }, link: true, wantAdd: true},
		{name: "not TOML", text: func(string) string { return block(exe) + "\n[broken\n" }},
		{name: "an inline table of servers", text: func(string) string { return "mcp_servers = { shikii = { command = \"shikii\", args = [\"mcp\"] } }\n" }},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			home := freshMachine(t)
			dir := t.TempDir()
			for _, name := range []string{"shikii-wrapper.sh", "node", "shikii"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(dir, "linked"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(dir, "shikii-wrapper.sh"), filepath.Join(dir, "linked", "shikii")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, ".codex", "config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			text := f.text(dir)
			mode := f.mode
			if mode == 0 {
				mode = 0o644
			}
			target := path
			if f.link {
				target = filepath.Join(dir, "dotfiles-config.toml")
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(target, []byte(text), mode); err != nil {
				t.Fatal(err)
			}
			if f.note {
				if err := noteCodexToolsAllowed(codexToolsAllowedMark()); err != nil {
					t.Fatal(err)
				}
			}
			preview := capture(t, func() error { return hookInstall(nil, true) })
			promised := strings.Contains(preview, codexPreviewPromise)
			if after, _ := os.ReadFile(target); string(after) != text {
				t.Fatalf("the preview changed the file")
			}
			out := capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
			after, _ := os.ReadFile(target)
			added := string(after) != text && strings.Contains(string(after), codexToolsAllowedLine) && strings.Count(string(after), codexToolsAllowedLine) > strings.Count(text, codexToolsAllowedLine)
			if promised != added {
				t.Errorf("the preview promised=%v, the install added=%v\n--- before\n%s--- after\n%s", promised, added, text, after)
			}
			if added != f.wantAdd {
				t.Errorf("added=%v, want %v\n--- before\n%s--- after\n%s--- out\n%.600s", added, f.wantAdd, text, after, out)
			}
			if !added && string(after) != text {
				t.Errorf("the file changed without the line being added:\n--- before\n%s--- after\n%s", text, after)
			}
			if added {
				want := strings.Replace(text, args+"\n", args+"\n"+codexToolsAllowedLine+"\n", 1)
				want = strings.Replace(want, `command = "shikii"`, "command = "+exe, 1)
				if string(after) != want {
					t.Errorf("the install changed more than the line:\n--- got\n%s--- want\n%s", after, want)
				}
				if !fileExists(codexToolsAllowedMark()) {
					t.Errorf("the line was added and the note was not written")
				}
			}
			if f.link {
				if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Errorf("the link was replaced by a file")
				}
			}
			if info, err := os.Stat(target); err != nil || info.Mode().Perm() != mode {
				t.Errorf("the mode of the file changed: %v", info.Mode())
			}
			// a second install changes nothing more
			capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
			if again, _ := os.ReadFile(target); string(again) != string(after) {
				t.Errorf("a second install changed the file again:\n%s", again)
			}
			t.Logf("promised=%v added=%v note=%v", promised, added, fileExists(codexToolsAllowedMark()))
		})
	}
}
