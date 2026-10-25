package main

import (
	"encoding/json"
	"errors"
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
	// AGENTS.md: the section is added
	ab, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(ab), "# My project") || strings.Count(string(ab), "## shikii") != 1 {
		t.Fatalf("AGENTS.md must keep its content and gain one shikii section:\n%s", ab)
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
	for _, must := range []string{".mcp.json", "opencode.json", "Codex", "AGENTS.md", "skills/shikii", "shikii up"} {
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
	if strings.Count(string(cb2), "[mcp_servers.shikii]") != 1 || strings.Count(string(ab2), "## shikii") != 1 {
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
	if _, err := os.Stat("AGENTS.md"); !os.IsNotExist(err) || !strings.Contains(out, "AGENTS.md: not created") {
		t.Fatalf("with only CLAUDE.md, AGENTS.md must not be created and init must say why:\n%s", out)
	}
	if !strings.Contains(out, "No HOME") || !fileExists(".mcp.json") || !fileExists("opencode.json") {
		t.Fatalf("without HOME, the project side is still prepared and init succeeds:\n%s", out)
	}
}

// 0.9.0: Codex skims the guidance in tool replies and reads AGENTS.md as instructions (measured). shikii's section in AGENTS.md says to give lessons a kind and a scene,
// and to classify the unclassified ones in a reply within the same turn. A section from an earlier version (between the markers) is replaced with the new content, and what a person wrote
// stays as is. The second run does nothing.
func TestAgentsSectionCarriesKindSceneAndUpdatesOldSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	old := "# 人の規則\n\n- 人が書いた行\n\n" + agentsMarker + "\n## shikii(判定の作法)\n\n- 古い作法\n" + agentsMarker + "\n\n## 後ろの人の節\n\n- 残る\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	did, err := addAgentsSection(path)
	if err != nil || !did {
		t.Fatalf("must replace the old section: %v %v", did, err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Contains(s, "古い作法") || !strings.Contains(s, "unclassified") || !strings.Contains(s, "wiki classify") || !strings.Contains(s, "kind") ||
		!strings.Contains(s, "- 人が書いた行") || !strings.Contains(s, "## 後ろの人の節\n\n- 残る") || strings.Count(s, agentsMarker) != 2 {
		t.Fatalf("only the part between the markers must get the new content: %s", s)
	}
	if did, err := addAgentsSection(path); err != nil || did {
		t.Fatalf("the second run must do nothing: %v %v", did, err)
	}
	if b2, _ := os.ReadFile(path); string(b2) != s {
		t.Fatal("the content changed on the second run")
	}
}

// 0.9.0: the update puts the skill in the home folder back, but each project's AGENTS.md kept the earlier version's section. The hook install run at the end
// of the update replaces it with the new content only when a registered project's AGENTS.md has a shikii section (it does not create one where there is none).
func TestHookInstallRefreshesAgentsSectionOfRegisteredProjects(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	withSection, without := gitRepo(t), gitRepo(t)
	for _, d := range []string{withSection, without} {
		t.Chdir(d)
		capture(t, func() error { return cmdInit(nil) })
	}
	oldText := agentsMarker + "\n## shikii(判定の作法)\n\n- 古い作法\n" + agentsMarker + "\n"
	os.WriteFile(filepath.Join(withSection, "AGENTS.md"), []byte("# 人の規則\n\n"+oldText), 0o644)
	os.WriteFile(filepath.Join(without, "AGENTS.md"), []byte("# 人の規則だけ\n"), 0o644)
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	got, _ := os.ReadFile(filepath.Join(withSection, "AGENTS.md"))
	if strings.Contains(string(got), "古い作法") || !strings.Contains(string(got), "wiki classify") || !strings.HasPrefix(string(got), "# 人の規則\n") {
		t.Fatalf("must replace the old section of a registered project: %s", got)
	}
	if !strings.Contains(out, "AGENTS.md") {
		t.Fatalf("must say it replaced it: %s", out)
	}
	if !strings.Contains(out, "reopen") || !strings.Contains(out, "0.9.0 or later") { // read from 0.9.1, it must not sound like a downgrade (final audit 0.9.1 low-F2)
		t.Fatalf("must say to reopen open sessions and bring every machine using the same wiki to 0.9.0 or later (audit 0.9.0 B3): %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(without, "AGENTS.md")); string(b) != "# 人の規則だけ\n" {
		t.Fatalf("must not create a section in an AGENTS.md without one: %s", b)
	}
}

// Audit 0.9.0 A1 and A2: an AGENTS.md that is a link pointing outside the repo, or that has only one marker-only line, is left alone and the reason returned (stopping neither init nor the update).
func TestAgentsSectionSkipsOutsideLinkAndLoneMarker(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "shared.md")
	os.WriteFile(outside, []byte("# 共有の規則\n\n"+agentsMarker+"\n## shikii(判定の作法)\n\n- 古い\n"+agentsMarker+"\n"), 0o644)
	repo := t.TempDir()
	link := filepath.Join(repo, "AGENTS.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	var skipped agentsSkipped
	if did, err := addAgentsSection(link); did || !errors.As(err, &skipped) {
		t.Fatalf("a link pointing outside must be left alone with a reason: %v %v", did, err)
	}
	if b, _ := os.ReadFile(outside); !strings.Contains(string(b), "- 古い") {
		t.Fatal("rewrote the file outside")
	}
	lone := filepath.Join(t.TempDir(), "AGENTS.md")
	os.WriteFile(lone, []byte("# 規則\n\n"+agentsMarker+"\n## shikii(判定の作法)\n\n- 閉じの印が無い\n"), 0o644)
	if did, err := addAgentsSection(lone); did || !errors.As(err, &skipped) {
		t.Fatalf("with only one marker it must be left alone with a reason: %v %v", did, err)
	}
}
