package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Confirmation audit 1 (A4): readJSONSettings accepts a top-level JSON null as settings. Decoding null into the map sets it to nil, and
// addJSONEntry then assigns into that nil map: init panics instead of saying the settings are broken.
func TestConfirm1A4NullJSONSettingsStopInitWithAnError(t *testing.T) {
	for _, text := range []string{"null\n", "null"} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		var did bool
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("A4 (%q): init panics on broken settings instead of stopping with an error: %v", text, r)
				}
			}()
			did, err = addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		}()
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Errorf("A4 (%q): broken settings were rewritten: %q", text, b)
		}
		if err == nil {
			t.Errorf("A4 (%q): init does not stop on broken settings with an error (did=%v)", text, did)
		}
	}
}

// Confirmation audit 1 (A5): more TOML 1.1 forms Codex 0.164.0 reads (checked with `CODEX_HOME=<temp folder> codex mcp list`).
func TestConfirm1A5MoreCodexReadableTOML11(t *testing.T) {
	for name, text := range map[string]string{
		"escape e":                             "[mcp_servers.other]\ncommand = \"x\\e\"\n",
		"offset date-time without seconds":     "[mcp_servers.other]\ncommand = \"x\"\nt = 1979-05-27 07:32Z\n",
		"comment in a multi-line inline table": "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  # comment\n  A = \"1\"\n}\n",
	} {
		if _, err := readCodexConfig([]byte(text)); err != nil {
			t.Errorf("A5 (%s): Codex reads it but shikii does not: %v", name, err)
		}
	}
}
