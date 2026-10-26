package main

import "testing"

// R7 (A2): "found=false only when there is no shikii server". An inline table Codex reads as a shikii server is reported as not found.
func TestRe2FoundFalseOnlyWithoutShikii(t *testing.T) {
	for _, text := range []string{
		"mcp_servers = { shikii = { command = \"x\" } }\n",
		"[mcp_servers]\nshikii = { command = \"x\", args = [\n\"a\"] }\nother = 1\n",
	} {
		_, found, err := withoutCodexServer(text)
		m, _ := readCodexConfig([]byte(text))
		if _, has := codexShikiiServer(m); has && !found {
			t.Errorf("A2: Codex reads a shikii server, yet found=false (err=%v):\n%s", err, text)
		}
	}
}
