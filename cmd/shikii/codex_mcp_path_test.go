package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.3: an earlier init wrote Codex's MCP server as `command = "shikii"` (or as a path that no longer exists). The update and init
// rewrite the command to this binary's path only while the block is still exactly what shikii wrote; a block a person edited is left alone.
func TestCodexServerCommandIsFixedOnlyInShikiisOwnBlock(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}
	block := func(cmd string) string {
		return "[mcp_servers.shikii]\ncommand = " + quotedList([]string{cmd}) + "\nargs = [" + quotedList(args) + "]\n"
	}
	other := "model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"x\"\n\n"
	gone := filepath.Join(t.TempDir(), "old", "shikii")
	existing := filepath.Join(t.TempDir(), "shikii")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		before string
		fixed  bool
	}{
		"bare name written by 0.9.1":        {other + block("shikii"), true},
		"a path that no longer exists":      {other + block(gone), true},
		"another existing binary (kept)":    {other + block(existing), false},
		"already this binary":               {other + block(exe), false},
		"args a person changed (kept)":      {other + strings.Replace(block("shikii"), `"mcp", `, `"mcp", "--proposer", "http://127.0.0.1:8083", `, 1), false},
		"a line a person added (kept)":      {other + block("shikii") + "env = { A = \"1\" }\n", false},
		"no shikii block (nothing to do)":   {other, false},
		"two shikii blocks (unclear, kept)": {block("shikii") + "\n" + block("shikii"), false},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(c.before), 0o600); err != nil {
			t.Fatal(err)
		}
		did, err := fixCodexServer(path, args)
		after, _ := os.ReadFile(path)
		if err != nil || did != c.fixed {
			t.Fatalf("%s: fixed=%v err=%v\n%s", name, did, err, after)
		}
		want := c.before
		if c.fixed {
			want = other + block(exe)
		}
		if string(after) != want {
			t.Fatalf("%s: the file must become\n%s\ngot\n%s", name, want, after)
		}
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: the file keeps its permissions: %v", name, fi.Mode())
		}
	}
}

// The update (hook install) fixes the block too, so a machine updated from 0.9.1 gets a working Codex MCP server without init.
func TestUpdateFixesCodexServerCommand(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	t.Chdir(gitRepo(t))
	capture(t, func() error { return cmdInit(nil) })
	path := filepath.Join(home, ".codex", "config.toml")
	old := "[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexMCPArgs) + "]\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	b, _ := os.ReadFile(path)
	exe, _ := os.Executable()
	if !strings.Contains(string(b), "command = "+quotedList([]string{exe})) || !strings.Contains(out, "Codex") {
		t.Fatalf("the update points the shikii-written Codex MCP server at this binary:\n%s\n%s", b, out)
	}
}
