package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

// A4: a chain of links whose last file does not exist yet: every link stays a link.
func TestAuditA4DanglingLinkChainStaysLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	mid := filepath.Join(dir, "mid.toml")
	if err := os.Symlink("mid.toml", path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.toml", mid); err != nil {
		t.Fatal(err)
	}
	did, err := addCodexServer(path, []string{"mcp"})
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	for _, p := range []string{path, mid} {
		fi, _ := os.Lstat(p)
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("A4: %s was a link and is now %v", filepath.Base(p), fi.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "real.toml")); err != nil {
		t.Errorf("A4: the file at the end of the chain was not created: %v", err)
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

// 0.9.7 full audit 2, 7: a config.toml shikii cannot write for a reason other than its mode (here immutable) stopped init before the skill
// was placed. init says what to add by hand and goes on.
func TestInitGoesOnPastAnUnwritableCodexConfig(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("model = \"m\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("chflags", "uchg", path).CombinedOutput(); err != nil {
		t.Skip(string(out))
	}
	defer exec.Command("chflags", "nouchg", path).Run()
	repo := gitRepo(t)
	t.Chdir(repo)
	out := capture(t, func() error { return cmdInit(nil) })
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md")); err != nil {
		t.Fatalf("init goes on and places the skill: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[mcp_servers.shikii]") {
		t.Fatalf("init shows what to add by hand: %s", out)
	}
}
