package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.6 audit, middle 1 (the message): when init leaves config.toml alone because shikii may already be there, it still shows the block to add,
// since "kept the existing MCP settings" alone was what hid the missing registration before.
func TestCodexNoteShowsTheBlockWhenLeftAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("note = \"\"\"\n[mcp_servers.shikii]\n\"\"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "[mcp_servers.shikii]") || !strings.Contains(note, "command = ") {
		t.Fatalf("the note shows the block to add: %q", note)
	}
}

// 0.9.6 audit, low 1: cmdMCP points hookDecide at its --local (useForHookDecisions); the MCP test helper must put it back, or a later test
// that reads hookLocalURL sees a closed test server's URL.
func TestMCPTestHelperRestoresTheHookURLs(t *testing.T) {
	before := hookLocalURL
	t.Run("mcp", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Chdir(t.TempDir())
		if _, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:47393"}, mcpInit(1)); err != nil {
			t.Fatal(err)
		}
	})
	if hookLocalURL != before {
		t.Fatalf("hookLocalURL is restored after an MCP test: %s (was %s)", hookLocalURL, before)
	}
}

// 0.9.6 audit, low 2: the shortened notices dropped two instructions the old text gave: restates is the <id> inside history:<id>:user
// (restateDone compares the id exactly, so the whole history key silently does nothing), and maintenance sends the latest revision after
// reading the sources and existing pages.
func TestShortNoticesKeepTheirInstructions(t *testing.T) {
	for name, text := range map[string]string{"notice": planningNotice("s", "t"), "shape": planningShape} {
		if !strings.Contains(text, "the <id> in history:<id>:user") {
			t.Errorf("the %s says restates is the <id> in history:<id>:user: %s", name, text)
		}
	}
	if r := reflectAsk("shikii"); !strings.Contains(r, "the latest page_id") || !strings.Contains(r, "Read the sources and existing pages") {
		t.Errorf("the reflection notice asks for the latest revision after reading the sources and pages: %s", r)
	}
}
