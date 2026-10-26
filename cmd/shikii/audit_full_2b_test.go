package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex 0.x reads a config.toml that starts with a UTF-8 BOM (checked with `codex mcp list` in a sandbox CODEX_HOME).
// codex_toml.go says "unreadable (not TOML; Codex cannot read it either)".
func TestAuditBOMConfigCodexReads(t *testing.T) {
	bom := "\xef\xbb\xbf"
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(bom+"model = \"m\"\n"), 0o600)
	did, err := addCodexServer(path, []string{"mcp"})
	if !did {
		t.Errorf("A1: a file Codex reads was not given shikii: did=%v err=%v state=%s", did, err, codexServerState([]byte(bom+"model = \"m\"\n"), "/x", []string{"mcp"}))
	}
	withShikii := bom + "model = \"m\"\n\n[mcp_servers.shikii]\ncommand = \"/x\"\nargs = [\"mcp\"]\n"
	_, found, err := withoutCodexServer(withShikii)
	if !found {
		t.Errorf("A2: uninstall leaves Codex's shikii server in a BOM file: found=%v err=%v", found, err)
	}
}

func TestAuditArrayOfTablesNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[[mcp_servers]]\nname = \"a\"\n"), 0o600)
	note := codexServerNote(path)
	if strings.Contains(note, "inline table") {
		t.Errorf("an array of tables is reported as an inline table: %s", note)
	}
}
