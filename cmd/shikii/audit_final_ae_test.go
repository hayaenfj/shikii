package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 0.9.7 final audit (A1 to A5, E1 to E2). The checks use hand-written expected values, not the functions under test.
// Codex 0.164.0 reads every config.toml below (checked with `CODEX_HOME=<temp folder> codex mcp list`): its TOML reader accepts
// TOML 1.1 forms (multi-line inline tables, trailing commas, the \x escape, times without seconds).

// A5: a file Codex reads, shikii reads too.
func TestFinalAEA5CodexReadableTOML11(t *testing.T) {
	other := func(server map[string]any) map[string]any {
		return map[string]any{"mcp_servers": map[string]any{"other": server}}
	}
	cases := []struct {
		name string
		text string
		want map[string]any // nil: only that it is read
	}{
		{"\u8907\u6570\u884c\u306e\u30a4\u30f3\u30e9\u30a4\u30f3\u30c6\u30fc\u30d6\u30eb\u3068\u672b\u5c3e\u306e\u30ab\u30f3\u30de", "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  A = \"1\",\n}\n",
			other(map[string]any{"command": "x", "env": map[string]any{"A": "1"}})},
		{"1 \u884c\u306e\u30a4\u30f3\u30e9\u30a4\u30f3\u30c6\u30fc\u30d6\u30eb\u306e\u672b\u5c3e\u306e\u30ab\u30f3\u30de", "[mcp_servers.other]\ncommand = \"x\"\nenv = { A = \"1\", }\n",
			other(map[string]any{"command": "x", "env": map[string]any{"A": "1"}})},
		{"\\x \u306e\u30a8\u30b9\u30b1\u30fc\u30d7", "[mcp_servers.other]\ncommand = \"x\\x41\"\n", other(map[string]any{"command": "xA"})},
		{"\u79d2\u306e\u7121\u3044\u6642\u523b", "[mcp_servers.other]\ncommand = \"x\"\nt = 07:32\n", nil},
	}
	for _, c := range cases {
		m, err := readCodexConfig([]byte(c.text))
		if err != nil {
			t.Errorf("A5 (%s): Codex reads it but shikii does not: %v\n%s", c.name, err, c.text)
			continue
		}
		if c.want != nil && !reflect.DeepEqual(m, c.want) {
			t.Errorf("A5 (%s): the values read differ: %#v", c.name, m)
		}
	}
}

// A2: removing shikii from a file Codex reads it from leaves no shikii and the rest unchanged; found is never false while there is one.
func TestFinalAEA2UninstallFromCodexReadableTOML11(t *testing.T) {
	rest := "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  A = \"1\",\n}\n"
	text := rest + "\n[mcp_servers.shikii]\ncommand = \"/usr/local/bin/shikii\"\nargs = [\"mcp\", \"--local\", \"http://127.0.0.1:8080\"]\n"

	out, found, err := withoutCodexServer(text)
	if !found {
		t.Errorf("A2: Codex reads shikii but found=false (err=%v)", err)
	}
	if err != nil || out != rest {
		t.Errorf("A2: only the shikii table is not removed: err=%v\ngot:\n%s", err, out)
	}

	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, err := removeCodexServer(p, true); err != nil || !found {
		t.Errorf("A2: uninstall cannot remove it: found=%v err=%v", found, err)
	}
	if b, _ := os.ReadFile(p); string(b) != rest {
		t.Errorf("A2: the file after uninstall differs:\n%s", b)
	}
}

// A5 for init: shikii is added to a file Codex reads (it is not reported as unreadable).
func TestFinalAEA5InitAddsToCodexReadableTOML11(t *testing.T) {
	text := "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  A = \"1\",\n}\n"
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	did, err := addCodexServer(p, []string{"mcp"})
	b, _ := os.ReadFile(p)
	if err != nil || !did {
		t.Errorf("A5: shikii is not added to a file Codex reads: did=%v err=%v\n%s", did, err, b)
	}
	if !bytes.HasPrefix(b, []byte(text)) || strings.Count(string(b), "\n[mcp_servers.shikii]\n") != 1 {
		t.Errorf("A1: not exactly one shikii table after the original text:\n%s", b)
	}
}

// A4: broken settings stop init (nothing is rewritten). Rewriting a .mcp.json / opencode.json that neither Claude Code nor OpenCode
// can read from only the part init read drops the person's values.
func TestFinalAEA4BrokenJSONSettingsAreNotRewritten(t *testing.T) {
	cases := map[string]string{
		"mcpServers \u304c\u914d\u5217":       "{\n  \"mcpServers\": [\"keep-me\"]\n}\n",
		"mcpServers \u304c\u6587\u5b57\u5217": "{\n  \"mcpServers\": \"see ~/servers.json\"\n}\n",
		"\u6700\u521d\u306e\u5024\u306e\u5f8c\u306b\u7d9a\u304d\u304c\u3042\u308b(JSON \u3067\u306a\u3044)": "{\"mcpServers\": {}}\n{\"other\": {\"command\": \"keep-me\"}}\n",
	}
	for name, text := range cases {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		b, _ := os.ReadFile(p)
		if string(b) != text {
			t.Errorf("A4 (%s): broken settings were rewritten (did=%v err=%v)\nbefore:\n%safter:\n%s", name, did, err, text, b)
		}
		if err == nil {
			t.Errorf("A4 (%s): init does not stop on broken settings (did=%v)", name, did)
		}
	}
}

// A4 on the uninstall side: broken settings (something after the first value) are not rewritten from the part that was read.
func TestFinalAEA4UninstallDoesNotRewriteBrokenJSON(t *testing.T) {
	text := "{\"mcpServers\": {\"shikii\": {\"command\": \"shikii\"}}}\n{\"other\": {\"command\": \"keep-me\"}}\n"
	p := filepath.Join(t.TempDir(), ".mcp.json")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := removeJSONEntry(p, "mcpServers", "shikii", true)
	if b, _ := os.ReadFile(p); string(b) != text {
		t.Errorf("A4: broken settings were rewritten (found=%v err=%v)\nafter:\n%s", found, err, b)
	}
}
