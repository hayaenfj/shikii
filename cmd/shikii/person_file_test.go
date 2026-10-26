package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.7: every settings file of the person's that shikii changes goes through one writer. Before, each writer did it its own way, and audits
// kept finding one that replaced a link (dotfiles) with a regular file, dropped the permissions, or wrote over a file made read-only.
// Each writer is run on a link to a 0600 file, and on a read-only file.
func TestEveryWriterOfAPersonsSettingsKeepsLinksModesAndReadOnly(t *testing.T) {
	writers := map[string]struct {
		before string
		write  func(path string) error
	}{
		"addJSONEntry (.mcp.json)": {`{"mcpServers": {}}`, func(p string) error {
			_, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
			return err
		}},
		"removeJSONEntry": {`{"mcpServers": {"shikii": {}}}`, func(p string) error {
			_, err := removeJSONEntry(p, "mcpServers", "shikii", true)
			return err
		}},
		"addCodexServer": {"model = \"m\"\n", func(p string) error {
			_, err := addCodexServer(p, []string{"mcp"})
			return err
		}},
		"fixCodexServer": {"[mcp_servers.shikii]\ncommand = \"shikii\"\n" + codexServerArgsLine + "\n", func(p string) error {
			_, err := fixCodexServer(p, codexServerArgs)
			return err
		}},
		"removeCodexServer": {"model = \"m\"\n\n[mcp_servers.shikii]\ncommand = \"x\"\n", func(p string) error {
			_, err := removeCodexServer(p, true)
			return err
		}},
	}
	for name, w := range writers {
		dir := t.TempDir()
		real := filepath.Join(dir, "dotfiles", "settings")
		if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(real, []byte(w.before), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "settings")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		if err := w.write(link); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s: the link stays a link", name)
		}
		fi, _ := os.Stat(real)
		b, _ := os.ReadFile(real)
		if string(b) == w.before || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: the file the link points at is changed and keeps 0600: mode %v\n%s", name, fi.Mode().Perm(), b)
		}

		ro := filepath.Join(t.TempDir(), "settings")
		if err := os.WriteFile(ro, []byte(w.before), 0o400); err != nil {
			t.Fatal(err)
		}
		_ = w.write(ro)
		if b, _ := os.ReadFile(ro); string(b) != w.before {
			t.Fatalf("%s: a read-only file is not written over:\n%s", name, b)
		}
		entries, _ := os.ReadDir(filepath.Dir(ro))
		for _, e := range entries {
			if e.Name() != "settings" && !strings.HasPrefix(e.Name(), "settings.bak") {
				t.Fatalf("%s: a temp file was left: %s", name, e.Name())
			}
		}
	}
}
