package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// A4: a hard-linked config.toml (dotfiles kept by hard link) must stay one file: both names see shikii's change.
func TestAuditA4HardLinkStaysOneFile(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "dotfiles-config.toml")
	if err := os.WriteFile(keep, []byte("model = \"m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.Link(keep, path); err != nil {
		t.Fatal(err)
	}
	did, err := addCodexServer(path, []string{"mcp"})
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	a, _ := os.ReadFile(path)
	b, _ := os.ReadFile(keep)
	if string(a) != string(b) {
		t.Fatalf("A4: the hard link was split: config.toml=%q, the other name=%q", a, b)
	}
}

// A4: a file the person cannot write (macOS ACL deny write; the mode bits still say 0644) is not replaced.
func TestAuditA4ACLDenyWriteIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	user := os.Getenv("USER")
	if out, err := exec.Command("chmod", "+a", user+" deny write", path).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	defer exec.Command("chmod", "-N", path).Run()
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Skip("the ACL did not take")
	}
	did, err := addCodexServer(path, []string{"mcp"})
	b, _ := os.ReadFile(path)
	out, _ := exec.Command("ls", "-le", path).CombinedOutput()
	if did || strings.Contains(string(b), "shikii") {
		t.Fatalf("A4: a file the person cannot write was replaced: did=%v err=%v\n%s\n%s", did, err, b, out)
	}
}

// A4: an immutable (chflags uchg) config.toml: init should treat it as read-only, not stop init with an error.
func TestAuditA4ImmutableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("chflags", "uchg", path).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	defer exec.Command("chflags", "nouchg", path).Run()
	did, err := addCodexServer(path, []string{"mcp"})
	ents, _ := os.ReadDir(dir)
	t.Logf("did=%v err=%v readOnly=%v entries=%d", did, err, err != nil && strings.Contains(err.Error(), "read-only"), len(ents))
	if len(ents) != 1 {
		t.Errorf("A4: a temp file was left: %v", ents)
	}
	if did && err != nil {
		t.Errorf("A4: a failed write is not reported as added: did=%v err=%v", did, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "model = \"m\"\n" {
		t.Errorf("A4: an immutable file is not changed: %q", b)
	}
}

// R1 (A4): a relative link whose ".." crosses a linked folder. The kernel resolves ".." from the real folder; endOfLinks joins it lexically.
func TestRe2RelativeLinkThroughLinkedFolder(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real", "codex")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(dir, "real", "shared.toml")
	if err := os.WriteFile(shared, []byte("model = \"m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../shared.toml", filepath.Join(realDir, "config.toml")); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(home, ".codex")); err != nil {
		t.Fatal(err)
	}
	// an unrelated file of the person's at the place the lexical join lands on
	stray := filepath.Join(home, "shared.toml")
	if err := os.WriteFile(stray, []byte("# my other notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	did, err := addCodexServer(path, []string{"mcp"})
	got, _ := os.ReadFile(shared)
	other, _ := os.ReadFile(stray)
	viaLink, _ := os.ReadFile(path)
	if !strings.Contains(string(viaLink), "[mcp_servers.shikii]") || string(other) != "# my other notes\n" {
		t.Fatalf("A4: did=%v err=%v\nthe file Codex reads (%s):\n%s\nan unrelated file (%s):\n%s", did, err, shared, got, stray, other)
	}
}

// R4 (A4 "\u6a29\u9650\u306f\u4fdd\u3064"): the group, an ACL entry and an extended attribute of the file.
func TestRe2KeepsGroupACLAndXattr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"m\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("chgrp", "admin", path).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	if out, err := exec.Command("chmod", "+a", "group:admin allow write", path).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	if out, err := exec.Command("xattr", "-w", "com.example.keep", "yes", path).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	before, _ := exec.Command("ls", "-le", path).CombinedOutput()
	did, err := addCodexServer(path, []string{"mcp"})
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	after, _ := exec.Command("ls", "-le", path).CombinedOutput()
	x, xerr := exec.Command("xattr", "-p", "com.example.keep", path).CombinedOutput()
	var bad []string
	if !strings.Contains(string(after), "admin") || !strings.Contains(string(after), " ") {
	}
	fi, _ := os.Stat(path)
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Gid != 80 {
		bad = append(bad, "group changed")
	}
	if !strings.Contains(string(after), "allow write") {
		bad = append(bad, "ACL dropped")
	}
	if xerr != nil {
		bad = append(bad, "xattr dropped: "+strings.TrimSpace(string(x)))
	}
	if len(bad) > 0 {
		t.Fatalf("A4: %v\nbefore:\n%s\nafter:\n%s", bad, before, after)
	}
}
