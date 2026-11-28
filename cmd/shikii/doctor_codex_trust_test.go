package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installedCodexHooks writes the hooks of shikii as hook install --yes writes them and returns the home, the hooks file and the keys
// Codex would name the shikii hooks by.
func installedCodexHooks(t *testing.T) (home, file string, keys []string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	file = filepath.Join(home, ".codex", "hooks.json")
	for _, event := range []string{"PreToolUse", "SessionStart", "Stop", "SubagentStart", "SubagentStop", "UserPromptSubmit"} {
		keys = append(keys, codexHookKey(file, event, 0, 0))
	}
	return home, file, keys
}

func writeCodexConfig(t *testing.T, home string, tables ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[features]\nhooks = true\n\n"+strings.Join(tables, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func trustTable(key string, extra ...string) string {
	return "[hooks.state.\"" + key + "\"]\ntrusted_hash = \"sha256:abcdef\"\n" + strings.Join(extra, "\n") + "\n"
}

// A real Codex 0.172.0 with shikii's hooks installed and no record that it trusts them ran none of them, and codex exec said nothing
// (2026-11-28). doctor says so, names the events, and says what to do; with a record for every hook it says ok; a hook turned off in
// /hooks (enabled = false, as a person's herdr hook is on the author's machine) is named too.
func TestDoctorSaysWhenCodexHasNoTrustRecordForShikiisHooks(t *testing.T) {
	home, _, keys := installedCodexHooks(t)

	// the config of the real run: only the MCP server, no hooks.state
	writeCodexConfig(t, home, "[mcp_servers.shikii]\ncommand = \"/x/shikii\"\nargs = [\"mcp\"]\n")
	lines := strings.Join(codexTrustLines(home), "\n")
	for _, event := range []string{"PreToolUse", "SessionStart", "Stop", "SubagentStart", "SubagentStop", "UserPromptSubmit"} {
		if !strings.Contains(lines, event) {
			t.Fatalf("doctor names the event %s that Codex has no record for:\n%s", event, lines)
		}
	}
	if !strings.HasPrefix(lines, "WARN Codex has no trust record") || !strings.Contains(lines, "/hooks") || !strings.Contains(lines, "codex exec prints nothing") {
		t.Fatalf("doctor says the hooks are skipped and what to do:\n%s", lines)
	}
	if got := codexTrustLines(t.TempDir()); got != nil {
		t.Fatalf("a home with no Codex hooks says nothing: %v", got)
	}

	// a record for each but one
	var tables []string
	for _, key := range keys[1:] {
		tables = append(tables, trustTable(key))
	}
	writeCodexConfig(t, home, tables...)
	lines = strings.Join(codexTrustLines(home), "\n")
	if !strings.Contains(lines, "hooks of PreToolUse in") || strings.Contains(lines, "SessionStart") {
		t.Fatalf("only the hook without a record is named:\n%s", lines)
	}

	// a record for every hook
	writeCodexConfig(t, home, append(tables, trustTable(keys[0]))...)
	got := codexTrustLines(home)
	if len(got) != 1 || !strings.HasPrefix(got[0], "ok   Codex has a trust record for the 6 shikii hooks") {
		t.Fatalf("every hook has a record: %v", got)
	}

	// one turned off in /hooks
	writeCodexConfig(t, home, append(tables, trustTable(keys[0], "enabled = false"))...)
	got = codexTrustLines(home)
	if len(got) != 1 || !strings.HasPrefix(got[0], "WARN shikii's hooks of PreToolUse are turned off") {
		t.Fatalf("a hook turned off is named: %v", got)
	}

	// an empty trusted_hash is no record, and a table of another hook says nothing about these
	writeCodexConfig(t, home, "[hooks.state.\""+keys[0]+"\"]\ntrusted_hash = \"\"\n", trustTable(filepath.Join(home, "other", "hooks.json")+":stop:0:0"))
	if got = codexTrustLines(home); len(got) == 0 || !strings.HasPrefix(got[0], "WARN Codex has no trust record") {
		t.Fatalf("an empty hash and another file's record are no trust: %v", got)
	}
}

// doctor prints the line among the others.
func TestDoctorPrintsTheCodexTrustLine(t *testing.T) {
	home, _, _ := installedCodexHooks(t)
	writeCodexConfig(t, home)
	out := capture(t, func() error {
		return cmdDoctor([]string{"--specs", home, "--store", home + "/.shikii", "--local", "", "--hosted", ""})
	})
	if !strings.Contains(out, "WARN Codex has no trust record for shikii's hooks of ") {
		t.Fatalf("doctor prints the line for the hooks Codex has not trusted:\n%s", out)
	}
}
