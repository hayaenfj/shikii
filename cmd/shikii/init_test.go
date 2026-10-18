package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shikii init は 1 回でエージェントの準備を済ませる(オプション無し): 基本構成のフォルダ、.mcp.json の shikii の項(他の項は残す)、
// skill を ~/.claude/skills/shikii に配る。2 回目は何も壊さない。
func TestInitPreparesTheProjectAndTheAgentInOneGo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	for _, d := range []string{"decisions", "wiki", ".shikii"} {
		if fi, err := os.Stat(filepath.Join(dir, d)); err != nil || !fi.IsDir() {
			t.Fatalf("%s must exist", d)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var m map[string]map[string]map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["mcpServers"]["other"] == nil || m["mcpServers"]["shikii"] == nil || m["mcpServers"]["shikii"]["command"] != "shikii" {
		t.Fatalf("the shikii server must be added without dropping others: %s", b)
	}
	skill := filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md")
	sb, err := os.ReadFile(skill)
	if err != nil || !strings.Contains(string(sb), "name: shikii") {
		t.Fatalf("the skill must be installed for the agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "references", "bands.md")); err != nil {
		t.Fatalf("references must come along: %v", err)
	}
	for _, must := range []string{".mcp.json", "skills/shikii", "shikii up"} {
		if !strings.Contains(out, must) {
			t.Fatalf("init must say what it did and what comes next; missing %q:\n%s", must, out)
		}
	}
	// 2 回目: 同じ結果、重複や誤り無し
	capture(t, func() error { return cmdInit(nil) })
	b2, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var m2 map[string]map[string]any
	if err := json.Unmarshal(b2, &m2); err != nil || len(m2["mcpServers"]) != 2 {
		t.Fatalf("init must be idempotent (other + shikii, nothing duplicated): %v %s", err, b2)
	}
}
