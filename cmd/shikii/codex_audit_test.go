package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Codex audit 0.9.2 (.shikii/handoff/codex-audit-0.9.2/report.md).

// Medium 3: Codex asks to trust hooks again whenever hooks.json changes (a new path or timeout), and codex exec skips untrusted hooks silently.
// The note shown when install changes Codex's hooks.json says so, instead of "once".
func TestCodexHookNoteSaysTrustAgainWheneverItChanges(t *testing.T) {
	var note string
	for _, h := range hookTargets {
		if h.who == "Codex" {
			note = h.note
		}
	}
	if !strings.Contains(note, "/hooks") || !strings.Contains(note, "whenever") || !strings.Contains(note, "skips") || strings.Contains(note, "once") {
		t.Fatalf("the Codex note says to trust again whenever hooks.json changes, and that Codex skips untrusted hooks: %q", note)
	}
}

// Low 1: Codex often passed the JSON as an argument first and was refused; the planning notice says it goes on standard input.
func TestPlanningNoticeSaysStandardInput(t *testing.T) {
	if n := planningNotice("s", "t"); !strings.Contains(n, "standard input") {
		t.Fatalf("the planning notice says the JSON goes on standard input: %s", n)
	}
}

// Low 3: the hooks run the binary by its absolute path, but Codex's MCP server was "shikii" (found only on PATH).
func TestCodexMCPServerUsesTheBinaryPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := addCodexServer(path, []string{"mcp"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	m := regexp.MustCompile(`(?m)^command = "([^"]+)"$`).FindSubmatch(b)
	if m == nil || !filepath.IsAbs(string(m[1])) {
		t.Fatalf("Codex's MCP command is the binary's absolute path: %s", b)
	}
}
