package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.3: Codex reads shikii's MCP server only from the shared ~/.codex/config.toml and starts it in the folder its daemon started in, so the shared
// registration names no store: `command = <this binary>`, `args = ["mcp", "--local", ...]`, and each tool call passes project. The update and init
// point a bare name or a path that no longer exists in that form at this binary. A block a person edited, or in an older version's form (0.9.1:
// command "shikii" with mcp --store .shikii --local ...; 0.9.2: an absolute command with the same args), is left alone (0.9.7: only told).
func TestCodexServerCommandIsFixedOnlyInShikiisOwnBlock(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := []string{"mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}
	block := func(cmd string, args []string) string {
		return "[mcp_servers.shikii]\ncommand = " + quotedList([]string{cmd}) + "\nargs = [" + quotedList(args) + "]\n"
	}
	other := "model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"x\"\n\n"
	gone := filepath.Join(t.TempDir(), "old", "shikii")
	existing := filepath.Join(t.TempDir(), "shikii")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinned := []string{"mcp", "--store", "/Users/someone/project/.shikii", "--local", "http://127.0.0.1:8080"}
	for name, c := range map[string]struct {
		before string
		fixed  bool
	}{
		"0.9.1 form (only told)":                      {other + block("shikii", old), false},
		"0.9.2 form (only told)":                      {other + block(exe, old), false},
		"0.9.2 form, another binary (only told)":      {other + block(existing, old), false},
		"0.9.2 form, a path that is gone (only told)": {other + block(gone, old), false},
		"new form with a bare name":                   {other + block("shikii", codexServerArgs), true},
		"new form with a path that no longer exists":  {other + block(gone, codexServerArgs), true},
		"new form, another existing binary (kept)":    {other + block(existing, codexServerArgs), false},
		"new form, already this binary":               {other + block(exe, codexServerArgs), false},
		"an absolute --store a person wrote (kept)":   {other + block(exe, pinned), false},
		"args a person changed (kept)":                {other + block("shikii", append([]string{"mcp", "--proposer", "http://127.0.0.1:8083"}, old[1:]...)), false},
		"a line a person added (kept)":                {other + block("shikii", old) + "env = { A = \"1\" }\n", false},
		"a relative command (kept)":                   {other + block("./bin/shikii", old), false},
		"no shikii block (nothing to do)":             {other, false},
		"two shikii blocks (unclear, kept)":           {block("shikii", old) + "\n" + block("shikii", old), false},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(c.before), 0o600); err != nil {
			t.Fatal(err)
		}
		did, err := fixCodexServer(path, codexServerArgs)
		after, _ := os.ReadFile(path)
		if err != nil || did != c.fixed {
			t.Fatalf("%s: fixed=%v err=%v\n%s", name, did, err, after)
		}
		want := c.before
		if c.fixed {
			want = other + block(exe, codexServerArgs)
		}
		if string(after) != want {
			t.Fatalf("%s: the file must become\n%s\ngot\n%s", name, want, after)
		}
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: the file keeps its permissions: %v", name, fi.Mode())
		}
	}
}

// The update (hook install) leaves an older version's block (0.9.1, 0.9.2) and says what to write (TestOlderCodexFormIsOnlyTold).
// A block a person edited to name one project's store is left alone, and the update says that every Codex session uses that project.
func TestUpdateFixesCodexServerCommand(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	t.Chdir(gitRepo(t))
	capture(t, func() error { return cmdInit(nil) })
	path := filepath.Join(home, ".codex", "config.toml")
	exe, _ := os.Executable()
	for name, c := range map[string]struct {
		before         string
		fixed, warning bool
	}{
		"0.9.1":                       {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexMCPArgs) + "]\n", false, false},
		"0.9.2":                       {"[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\nargs = [" + quotedList(codexMCPArgs) + "]\n", false, false},
		"current form, bare name":     {"[mcp_servers.shikii]\ncommand = \"shikii\"\n" + codexServerArgsLine + "\n", true, false},
		"absolute store by hand":      {"[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\nargs = [\"mcp\", \"--store\", \"/w/a/.shikii\", \"--specs\", \"/w/a/.shikii/decisions\"]\n", false, true},
		"absolute store, = form":      {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\"mcp\", \"--store=/w/a/.shikii\"]\nenv = { A = \"1\" }\n", false, true},
		"edited, no absolute store":   {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\"mcp\", \"--proposer\", \"http://127.0.0.1:8083\"]\n", false, false},
		"relative store kept by hand": {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\"mcp\", \"--store\", \".shikii\"]\nenv = { A = \"1\" }\n", false, false},
	} {
		if err := os.WriteFile(path, []byte(c.before), 0o644); err != nil {
			t.Fatal(err)
		}
		out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
		b, _ := os.ReadFile(path)
		want := c.before
		if c.fixed {
			want = "[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\n" + codexServerArgsLine + "\n"
		}
		if string(b) != want {
			t.Fatalf("%s: the file must become\n%s\ngot\n%s\n%s", name, want, b, out)
		}
		if c.fixed != strings.Contains(out, "pointed the shikii MCP server") {
			t.Fatalf("%s: the update says it fixed the block only when it did:\n%s", name, out)
		}
		if c.warning != strings.Contains(out, "every Codex session uses that one project") || c.warning != strings.Contains(out, "set "+codexServerArgsLine) {
			t.Fatalf("%s: the update warns about a hand-edited block that pins one project (and only then):\n%s", name, out)
		}
	}
}
