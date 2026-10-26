package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.5 check: init left ~/.codex/config.toml alone whenever the word mcp_servers appeared anywhere, so a person who already had one MCP server of
// their own ([mcp_servers.mytool], or only a comment naming mcp_servers) never got shikii's server, while init said "kept the existing MCP
// settings". It now adds [mcp_servers.shikii] unless shikii is already defined or mcp_servers is an inline table (which cannot be extended); in
// that case it says so and shows the block to add by hand.
func TestCodexServerAddedNextToAPersonsOwnServers(t *testing.T) {
	for name, toml := range map[string]string{
		"own table":   "model = \"gpt-5\"\n\n[mcp_servers.mytool]\ncommand = \"mytool\"\n",
		"own section": "[mcp_servers]\n\n[mcp_servers.mytool]\ncommand = \"mytool\"\n",
		"comment":     "# mcp_servers: add later\nmodel = \"gpt-5\"\n",
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || !did || !strings.HasPrefix(string(b), toml) || strings.Count(string(b), "[mcp_servers.shikii]") != 1 {
			t.Fatalf("%s: shikii is added after the person's settings, which stay as they were: did=%v err=%v\n%s", name, did, err, b)
		}
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	inline := "mcp_servers = { mytool = { command = \"mytool\" } }\n"
	if err := os.WriteFile(p, []byte(inline), 0o644); err != nil {
		t.Fatal(err)
	}
	if did, err := addCodexServer(p, []string{"mcp"}); err != nil || did {
		t.Fatalf("an inline mcp_servers table cannot be extended, so the file is left alone: did=%v err=%v", did, err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "[mcp_servers.shikii]") || strings.Contains(note, "kept the existing") {
		t.Fatalf("the person is told shikii was not added and what to add: %q", note)
	}
}
