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

// shikii init は 1 回でエージェントの準備を済ませる(オプション無し): 基本構成のフォルダ、MCP の設定(Claude Code / Cursor / OpenCode / Codex)、
// AGENTS.md の節、skill を ~/.claude/skills/shikii に置き直す。人が書いたものは触らず、2 回目も安全。
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
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"o3\""), 0o644); err != nil { // 末尾に改行が無い
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
	// Claude Code: 他の項・上位の鍵・数字・& < > がそのまま
	b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	m := readJSON(t, filepath.Join(dir, ".mcp.json"))
	servers := m["mcpServers"].(map[string]any)
	if servers["other"] == nil || servers["shikii"] == nil || servers["shikii"].(map[string]any)["command"] != "shikii" {
		t.Fatalf("the shikii server must be added without dropping others: %s", b)
	}
	if !strings.Contains(string(b), `"a&b<c>"`) || !strings.Contains(string(b), "12345678901234567890") || !strings.Contains(string(b), `"top"`) {
		t.Fatalf("other keys, numbers and & < > must survive untouched: %s", b)
	}
	// Cursor と OpenCode
	if c := readJSON(t, filepath.Join(dir, ".cursor", "mcp.json")); c["mcpServers"].(map[string]any)["shikii"] == nil {
		t.Fatalf("Cursor reads .cursor/mcp.json")
	}
	oc := readJSON(t, filepath.Join(dir, "opencode.json"))
	if e, _ := oc["mcp"].(map[string]any)["shikii"].(map[string]any); e == nil || e["type"] != "local" {
		t.Fatalf("OpenCode reads opencode.json mcp.shikii with type local: %+v", oc)
	}
	// Codex: 既存の行は残り、[mcp_servers.shikii] が末尾に足される
	cb, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.HasPrefix(string(cb), "model = \"o3\"\n\n[mcp_servers.shikii]") || !strings.Contains(string(cb), `command = "shikii"`) {
		t.Fatalf("Codex config must keep its lines and gain the shikii server:\n%s", cb)
	}
	// AGENTS.md: 節が足される
	ab, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(ab), "# My project") || strings.Count(string(ab), "## shikii") != 1 {
		t.Fatalf("AGENTS.md must keep its content and gain one shikii section:\n%s", ab)
	}
	// skill: 置き直し(古いファイルは消える)
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
	for _, must := range []string{".mcp.json", ".cursor/mcp.json", "opencode.json", "Codex", "AGENTS.md", "skills/shikii", "shikii up"} {
		if !strings.Contains(out, must) {
			t.Fatalf("init must say what it did and what comes next; missing %q:\n%s", must, out)
		}
	}
	// 2 回目: 何も重複せず、人が足した --proposer も残る
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"other":{"command":"x"},"shikii":{"command":"shikii","args":["mcp","--proposer","http://127.0.0.1:8083"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdInit(nil) })
	b2, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(b2), "--proposer") || !strings.Contains(out, "保持") {
		t.Fatalf("an existing shikii entry must be kept as the person wrote it: %s\n%s", b2, out)
	}
	cb2, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	ab2, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Count(string(cb2), "[mcp_servers.shikii]") != 1 || strings.Count(string(ab2), "## shikii") != 1 {
		t.Fatalf("init must be idempotent for Codex and AGENTS.md")
	}
	// 壊れた JSON は上書きしない
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdInit(nil); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("a broken .mcp.json must stop init: %v", err)
	}
}

// 監査 6: init が人の設定を壊す経路を閉じる。
//  1. Codex の config.toml に mcp_servers がどんな書き方でもあれば触らない(末尾に足すと TOML の二重定義で Codex が設定を読めなくなる)
//  2. 読めない .mcp.json は置き換えない
//  6. CLAUDE.md だけのプロジェクトに AGENTS.md を作らない(OpenCode が CLAUDE.md を読まなくなる)
//  9. HOME が無くてもプロジェクト側は済ませて成功で終わる
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
	if _, err := os.Stat("AGENTS.md"); !os.IsNotExist(err) || !strings.Contains(out, "AGENTS.md: 作成を省略") {
		t.Fatalf("with only CLAUDE.md, AGENTS.md must not be created and init must say why:\n%s", out)
	}
	if !strings.Contains(out, "HOMEが無い") || !fileExists(".mcp.json") || !fileExists("opencode.json") {
		t.Fatalf("without HOME, the project side is still prepared and init succeeds:\n%s", out)
	}
}
