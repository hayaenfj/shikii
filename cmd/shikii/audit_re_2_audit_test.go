package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

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

// R2 (A4): a read-only AGENTS.md that still holds the section an earlier version wrote stops init (the skill is not placed).
func TestRe2InitGoesOnPastReadOnlyAgentsMD(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	repo := gitRepo(t)
	agents := filepath.Join(repo, "AGENTS.md")
	text := "# rules\n\n" + agentsMarker + "\n## shikii (old)\nbody\n" + agentsMarker + "\n"
	if err := os.WriteFile(agents, []byte(text), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	var runErr error
	out := capture(t, func() error { runErr = cmdInit(nil); return nil })
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md")); err != nil || runErr != nil {
		t.Fatalf("A4: init goes on past a read-only AGENTS.md: init err=%v skill=%v\n%s", runErr, err, out)
	}
}

// R3 (A4): an old shikii block in a config.toml whose folder shikii cannot write: fixCodexServer's write error stops init.
func TestRe2InitGoesOnPastUnwritableCodexFolderOnFix(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := "[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexMCPArgs) + "]\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(codexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(codexDir, 0o755) })
	repo := gitRepo(t)
	t.Chdir(repo)
	var runErr error
	out := capture(t, func() error { runErr = cmdInit(nil); return nil })
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md")); err != nil || runErr != nil {
		t.Fatalf("A4: init goes on when the old Codex block cannot be rewritten: init err=%v skill=%v\n%s", runErr, err, out)
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

// R5 (E): trackedWriter hides http.Flusher and io.ReaderFrom of the connection's writer.
func TestRe2TrackedWriterKeepsFlusher(t *testing.T) {
	var flusher, readerFrom bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flusher = w.(http.Flusher)
		_, readerFrom = w.(io.ReaderFrom)
		io.WriteString(w, "ok")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !flusher || !readerFrom {
		t.Fatalf("E: the writer handed to handlers lost http.Flusher=%v io.ReaderFrom=%v", flusher, readerFrom)
	}
}
