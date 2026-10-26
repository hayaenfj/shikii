package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// 0.9.7 audit A-3: a probe (pageLockHeld) must not make a starting page fail to take the lock when no page holds it.
func TestAuditProbeDoesNotMakeAStartingPageExit(t *testing.T) {
	common, scope := t.TempDir(), "/tmp/audit-project"
	f, ok := holdPageLock(common, scope) // create the lock file, then release (the earlier page ended)
	if !ok {
		t.Fatal("precondition")
	}
	f.Close()
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		for !stop.Load() {
			pageLockHeld(common, scope) // another hook / the hub / shikii page probing
		}
		close(done)
	}()
	fails := 0
	for i := 0; i < 5000; i++ {
		if f, ok := holdPageLock(common, scope); ok {
			f.Close()
		} else {
			fails++
		}
	}
	stop.Store(true)
	<-done
	if fails > 0 {
		t.Fatalf("no page held the lock, but a starting page could not take it %d of 5000 times (it would log 'another page is running' and exit)", fails)
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

// E: loopholes in the nil-writer rule.
func TestAuditNilWriterLoopholes(t *testing.T) {
	for name, src := range map[string]string{
		"nil variable": "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\nfunc (s *server) handleA(w http.ResponseWriter) { var none http.ResponseWriter; defer s.lockCalibration(none)() }\n",
		"go then wait": "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\nfunc (s *server) handleA(w http.ResponseWriter) { done := make(chan bool); go func() { s.lockCalibration(nil)(); done <- true }(); <-done }\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": src}); len(got) == 0 {
			t.Errorf("%s: a reply waits for the lock without an extended deadline, and the rule does not see it", name)
		}
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
