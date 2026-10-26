package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Confirmation audit 2 (A4): a settings file whose bytes are not UTF-8 is not JSON (RFC 8259 8.1), so it is broken settings. Go's decoder
// silently turns each bad byte into U+FFFD, and init / uninstall then rewrite the file: the person's original bytes are gone.
func TestConfirm2A4InvalidUTF8SettingsAreNotRewritten(t *testing.T) {
	text := "{\"env\": {\"TOKEN\": \"p\xe4ss\xff\"}, \"mcpServers\": {\"other\": {\"command\": \"x\"}}}\n"
	t.Run("init", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Errorf("A4: settings that are not UTF-8 were rewritten (did=%v, err=%v): %q", did, err, b)
		}
	})
	t.Run("uninstall", func(t *testing.T) {
		withShikii := "{\"env\": {\"TOKEN\": \"p\xe4ss\xff\"}, \"mcpServers\": {\"shikii\": {\"command\": \"shikii\"}}}\n"
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(withShikii), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := removeJSONEntry(p, "mcpServers", "shikii", true)
		if b, _ := os.ReadFile(p); string(b) != withShikii {
			t.Errorf("A4: settings that are not UTF-8 were rewritten (did=%v, err=%v): %q", did, err, b)
		}
	})
}

// Confirmation audit 2: a lone surrogate escape (\ud800) is valid JSON grammar and Node's JSON.parse keeps it, but Go decodes it to
// U+FFFD. Two different keys then collapse into one and the rewrite drops an entry of the person's (and changes the value text).
func TestConfirm2LoneSurrogateKeysAreNotDropped(t *testing.T) {
	text := "{\"mcp\": {\"\\ud800\": {\"command\": \"a\"}, \"\\udc00\": {\"command\": \"b\"}}}\n"
	p := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	did, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
	b, _ := os.ReadFile(p)
	if err != nil && string(b) == text {
		return // refused and left unchanged: fine
	}
	if !bytes.Contains(b, []byte("\"a\"")) || !bytes.Contains(b, []byte("\"b\"")) || !strings.Contains(strings.ToLower(string(b)), "\\ud800") {
		t.Errorf("a person's entry was dropped or changed by the rewrite (did=%v, err=%v): %q", did, err, b)
	}
}

// Control: settings that write U+FFFD themselves (as a character or as \ufffd) and other non-ASCII text are kept and shikii is added.
func TestConfirm2SettingsWritingFFFDThemselvesAreKept(t *testing.T) {
	for _, text := range []string{
		"{\"env\": {\"A\": \"\ufffd\", \"B\": \"\u65e5\u672c\"}}\n",
		"{\"env\": {\"A\": \"\\ufffd\"}}\n",
	} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"}); !did || err != nil {
			t.Fatalf("control (%q): shikii is added: %v %v", text, did, err)
		}
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), "\"shikii\"") || !strings.Contains(string(b), "\"A\"") {
			t.Fatalf("control (%q): the person's text is kept: %s", text, b)
		}
	}
}
