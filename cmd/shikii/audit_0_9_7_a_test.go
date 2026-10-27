package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 0.9.7 audit A-3: a probe (pageLockHeld) must not make a starting page fail to take the lock when no page holds it (it would log
// 'another page is running' and exit). Checked in one run: the start's first try meets a peek's shared lock, taken right then in
// beforePageLockTry with the same flock a peek uses, and the peek ends before the next try.
func TestAuditProbeDoesNotMakeAStartingPageExit(t *testing.T) {
	common, scope := t.TempDir(), "/tmp/audit-project"
	f, ok := holdPageLock(common, scope) // create the lock file, then release (the earlier page ended)
	if !ok {
		t.Fatal("precondition")
	}
	f.Close()
	peek, err := os.OpenFile(pageLockPath(common, scope), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer peek.Close()
	oldRetry, oldTry := pageLockRetry, beforePageLockTry
	t.Cleanup(func() { pageLockRetry, beforePageLockTry = oldRetry, oldTry })
	pageLockRetry = time.Hour // the result must not depend on how fast this machine is
	var tries []int
	beforePageLockTry = func(attempt int) {
		tries = append(tries, attempt)
		switch attempt {
		case 0:
			if err := syscall.Flock(int(peek.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
				t.Errorf("the peek takes the shared lock: %v", err)
			}
		case 1:
			_ = syscall.Flock(int(peek.Fd()), syscall.LOCK_UN) // the peek ends
		}
	}
	got, ok := holdPageLock(common, scope)
	if !ok {
		t.Fatalf("no page held the lock, but a start that met a peek could not take it (tries %v)", tries)
	}
	got.Close()
	if len(tries) != 2 {
		t.Fatalf("the first try meets the peek and the second takes the lock: tries %v", tries)
	}
}

// B: a link whose target does not exist yet stays a link.
func TestAuditDanglingLinkStaysALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "mcp.json")
	os.MkdirAll(filepath.Dir(real), 0o755)
	link := filepath.Join(dir, ".mcp.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := addJSONEntry(link, "mcpServers", "shikii", map[string]any{"command": "shikii"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the dangling link was replaced by a regular file (mode %v)", fi.Mode())
	}
}

// C: the read-only refusal names the file.
func TestAuditReadOnlyRefusalNamesTheFile(t *testing.T) {
	for name, w := range map[string]func(string) error{
		"addJSONEntry": func(p string) error {
			_, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
			return err
		},
		"addCodexServer": func(p string) error { _, err := addCodexServer(p, []string{"mcp"}); return err },
	} {
		p := filepath.Join(t.TempDir(), "settings-file")
		before := "{}"
		if name == "addCodexServer" {
			before = "model = \"m\"\n"
		}
		os.WriteFile(p, []byte(before), 0o400)
		err := w(p)
		if err == nil || !strings.Contains(err.Error(), "settings-file") {
			t.Errorf("%s: init stops with an error that does not name the file: %v", name, err)
		}
	}
}

// D: a valid config.toml holding nan (NaN != NaN under reflect.DeepEqual).
func TestAuditCodexConfigWithNaN(t *testing.T) {
	b := []byte("x = nan\n")
	if got := codexServerState(b, "/bin/shikii", []string{"mcp"}); got != "add" {
		t.Errorf("valid TOML with nan: state %q, want add", got)
	}
	out, found, err := withoutCodexServer("x = nan\n\n[mcp_servers.shikii]\ncommand = \"x\"\n")
	if err != nil || !found || strings.Contains(out, "shikii") {
		t.Errorf("uninstall cannot remove shikii next to nan: found=%v err=%v", found, err)
	}
}

// F: a read-only .mcp.json in the project: init goes on with the rest (Codex, the skill), as it does for a read-only AGENTS.md / fixCodexServer.
func TestAuditInitGoesOnPastAReadOnlyMCPJSON(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	os.WriteFile(filepath.Join(repo, ".mcp.json"), []byte("{}"), 0o400)
	t.Chdir(repo)
	err := cmdInit(nil)
	home, _ := os.UserHomeDir()
	_, cerr := os.Stat(filepath.Join(home, ".codex", "config.toml"))
	if err != nil || cerr != nil {
		t.Fatalf("init stopped at a read-only .mcp.json: err=%v; Codex config written: %v", err, cerr == nil)
	}
}

// G: the note for a valid file holding nan.
func TestAuditNaNNote(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("x = nan\n"), 0o644)
	if note := codexServerNote(p); strings.Contains(note, "<nil>") {
		t.Fatalf("the note says the file is not TOML with no reason: %s", note)
	}
}
