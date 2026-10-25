package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, b)
	}
	return m
}

// shikii init prepares the agent in one go (no options): the base layout folders, the MCP settings (Claude Code / OpenCode / Codex; Cursor is not supported from 0.9.2),
// the AGENTS.md section, and puts the skill back at ~/.claude/skills/shikii. What a person wrote is left alone, and running it again is safe.
func TestInitPreparesTheProjectAndTheAgentInOneGo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"top":{"n":12345678901234567890},"mcpServers":{"other":{"command":"x","env":{"T":"a&b<c>"}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "shikii", "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "skills", "shikii", "references", "OLD.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"o3\""), 0o644); err != nil { // no trailing newline
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	for _, d := range []string{filepath.Join(".shikii", "decisions"), filepath.Join(".shikii", "wiki"), filepath.Join(".shikii", "records"), filepath.Join(".shikii", "handoff")} {
		if fi, err := os.Stat(filepath.Join(dir, d)); err != nil || !fi.IsDir() {
			t.Fatalf("%s must exist", d)
		}
	}
	// Claude Code: other entries, top-level keys, numbers and & < > stay as they are
	b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	m := readJSON(t, filepath.Join(dir, ".mcp.json"))
	servers := m["mcpServers"].(map[string]any)
	if servers["other"] == nil || servers["shikii"] == nil || servers["shikii"].(map[string]any)["command"] != "shikii" {
		t.Fatalf("the shikii server must be added without dropping others: %s", b)
	}
	if !strings.Contains(string(b), `"a&b<c>"`) || !strings.Contains(string(b), "12345678901234567890") || !strings.Contains(string(b), `"top"`) {
		t.Fatalf("other keys, numbers and & < > must survive untouched: %s", b)
	}
	// Cursor is not supported (0.9.2): init does not write .cursor/mcp.json
	if _, err := os.Stat(filepath.Join(dir, ".cursor")); !os.IsNotExist(err) {
		t.Fatalf("init does not write Cursor's settings: %v", err)
	}
	// OpenCode
	oc := readJSON(t, filepath.Join(dir, "opencode.json"))
	if e, _ := oc["mcp"].(map[string]any)["shikii"].(map[string]any); e == nil || e["type"] != "local" {
		t.Fatalf("OpenCode reads opencode.json mcp.shikii with type local: %+v", oc)
	}
	// Codex: existing lines stay and [mcp_servers.shikii] is appended
	cb, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.HasPrefix(string(cb), "model = \"o3\"\n\n[mcp_servers.shikii]") || !strings.Contains(string(cb), `command = "shikii"`) {
		t.Fatalf("Codex config must keep its lines and gain the shikii server:\n%s", cb)
	}
	// AGENTS.md: the person's file, shikii adds nothing (0.9.2)
	ab, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(ab), "# My project") || strings.Contains(string(ab), "## shikii") || strings.Contains(string(ab), agentsMarker) {
		t.Fatalf("AGENTS.md must keep its content and gain nothing:\n%s", ab)
	}
	// skill: put back (old files are deleted)
	sb, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md"))
	if err != nil || !strings.Contains(string(sb), "name: shikii") {
		t.Fatalf("the skill must be installed for the agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "references", "bands.md")); err != nil {
		t.Fatalf("references must come along: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "references", "OLD.md")); !os.IsNotExist(err) {
		t.Fatalf("stale files in the skill folder must be removed (the skill is replaced, not merged)")
	}
	for _, must := range []string{".mcp.json", "opencode.json", "Codex", "skills/shikii", ".agents/skills/shikii", "shikii up"} {
		if !strings.Contains(out, must) {
			t.Fatalf("init must say what it did and what comes next; missing %q:\n%s", must, out)
		}
	}
	// Second run: nothing is duplicated and the --proposer a person added stays
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"other":{"command":"x"},"shikii":{"command":"shikii","args":["mcp","--proposer","http://127.0.0.1:8083"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdInit(nil) })
	b2, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(b2), "--proposer") || !strings.Contains(out, "kept") {
		t.Fatalf("an existing shikii entry must be kept as the person wrote it: %s\n%s", b2, out)
	}
	cb2, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	ab2, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Count(string(cb2), "[mcp_servers.shikii]") != 1 || string(ab2) != string(ab) {
		t.Fatalf("init must be idempotent for Codex and AGENTS.md")
	}
	// Broken JSON is not overwritten
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdInit(nil); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("a broken .mcp.json must stop init: %v", err)
	}
}

// Audit 6: close the paths by which init breaks a person's settings.
//  1. If Codex's config.toml has mcp_servers written in any form, leave it alone (appending would define it twice in TOML and Codex could not read its settings)
//  2. An unreadable .mcp.json is not replaced
//  6. Do not create AGENTS.md in a project with only CLAUDE.md (OpenCode would stop reading CLAUDE.md)
//  9. Even without HOME, the project side is done and it ends in success
func TestInitNeverBreaksWhatPeopleWrote(t *testing.T) {
	for name, toml := range map[string]string{
		"inline":  "mcp_servers = { shikii = { command = \"x\" } }\n",
		"table":   "[mcp_servers]\nshikii = { command = \"x\" }\n",
		"quoted":  "[mcp_servers.\"shikii\"]\ncommand = \"x\"\n",
		"heading": "[mcp_servers.shikii]\ncommand = \"x\"\n",
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || did || string(b) != toml {
			t.Fatalf("%s: a config that already has mcp_servers must be left alone: did=%v err=%v\n%s", name, did, err, b)
		}
	}
	if os.Geteuid() != 0 {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(`{"mcpServers":{"mine":{"command":"x"}}}`), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"}); err == nil {
			t.Fatalf("an unreadable config must stop init, not be replaced")
		}
		_ = os.Chmod(p, 0o644)
		if b, _ := os.ReadFile(p); !strings.Contains(string(b), "mine") {
			t.Fatalf("the unreadable file must keep its content: %s", b)
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", "")
	if err := os.WriteFile("CLAUDE.md", []byte("# rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	if _, err := os.Stat("AGENTS.md"); !os.IsNotExist(err) {
		t.Fatalf("with only CLAUDE.md, AGENTS.md must not be created:\n%s", out)
	}
	if !strings.Contains(out, "No HOME") || !fileExists(".mcp.json") || !fileExists("opencode.json") {
		t.Fatalf("without HOME, the project side is still prepared and init succeeds:\n%s", out)
	}
}
