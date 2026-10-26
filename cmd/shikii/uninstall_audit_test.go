package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A1: ~/.claude/settings.json kept in dotfiles through a link stays a link; the file it points at loses only shikii's hooks and keeps its mode.
func TestUninstAuditHooksKeepLinkedSettings(t *testing.T) {
	b := newUninstallBox(t)
	settings := filepath.Join(b.home, ".claude", "settings.json")
	real := filepath.Join(b.home, "dotfiles", "claude-settings.json")
	body, _ := os.ReadFile(settings)
	writeFileT(t, real, string(body))
	if err := os.Chmod(real, 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(settings)
	if err := os.Symlink(real, settings); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	info, err := os.Lstat(settings)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("~/.claude/settings.json must stay a link to the dotfiles:\n%s", out)
	}
	got, _ := os.ReadFile(real)
	if strings.Contains(string(got), " hook user-prompt-submit") {
		t.Fatalf("the linked file must lose shikii's hooks:\n%s", got)
	}
	if st, _ := os.Stat(real); st.Mode().Perm() != 0o644 {
		t.Fatalf("the linked file must keep its mode: %v", st.Mode())
	}
}

// A2: when shikii's own MCP table cannot be removed (read-only config.toml, e.g. a home-manager link into a read-only store),
// the run must not end as a success that tells the person to rm the binary (Codex would then fail to start the missing shikii).
func TestUninstAuditLeftShikiiPartIsNotSuccess(t *testing.T) {
	b := newUninstallBox(t)
	codex := filepath.Join(b.home, ".codex", "config.toml")
	if err := os.Chmod(codex, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(codex, 0o644) })
	var runErr error
	out := capture(t, func() error { runErr = cmdUninstall([]string{"--yes"}); return nil })
	if runErr == nil {
		t.Fatalf("shikii's MCP table is still in config.toml: the run must fail, not succeed:\n%s", out)
	}
	if i, j := strings.Index(out, "still there"), strings.Index(out, "\n  rm "); i < 0 || j < 0 || i > j {
		t.Fatalf("before the rm command the person must be told what of shikii is still there:\n%s", out)
	}
}

// A3: a registered project whose .shikii/ the person removed still has shikii's entry in .mcp.json and opencode.json (init wrote them):
// uninstall must remove them too, or Claude Code keeps starting shikii there after the binary is gone.
func TestUninstAuditReachesProjectsWithoutStore(t *testing.T) {
	b := newUninstallBox(t)
	if err := os.RemoveAll(filepath.Join(b.repo, ".shikii")); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	for _, f := range []string{".mcp.json", "opencode.json"} {
		body, _ := os.ReadFile(filepath.Join(b.repo, f))
		if strings.Contains(string(body), `"shikii"`) {
			t.Fatalf("%s still has shikii's entry:\n%s\n%s", f, body, out)
		}
	}
	if _, err := os.Lstat(filepath.Join(b.repo, ".shikii")); !os.IsNotExist(err) {
		t.Fatal("uninstall must not create .shikii/ again")
	}
}

// A4: the inline-table refusal looks for a shikii key, not the word inside a path; a real inline shikii key is still refused.
func TestUninstAuditInlineTableNeedsAShikiiKey(t *testing.T) {
	in := "mcp_servers = { other = { command = \"/home/shikii/bin/x\" } }\n"
	if out, found, err := withoutCodexServer(in); err != nil || found || out != in {
		t.Fatalf("a path with the word shikii is not shikii's server: found=%v err=%v", found, err)
	}
	for _, in := range []string{"mcp_servers = { shikii = { command = \"s\" } }\n", "mcp_servers = { a = {}, \"shikii\".command = \"s\" }\n"} {
		if _, _, err := withoutCodexServer(in); err == nil {
			t.Fatalf("an inline shikii key must be refused: %q", in)
		}
	}
}

// Holes found by mutation (each kills one survivor of the e929d05 tests).
func TestUninstAuditHoles(t *testing.T) {
	// M7: a path with ' stays one shell argument
	if q := shellQuote("/Users/o'neil/bin/shikii"); q != `'/Users/o'\''neil/bin/shikii'` {
		t.Fatalf("shellQuote: %s", q)
	}
	// M9: a # in a value starts a comment, so its [ does not keep the table open
	if out, _, _ := withoutCodexServer("[mcp_servers.shikii]\nargs = [\"mcp\"] # [ open\n[x]\ny = 1\n"); out != "[x]\ny = 1\n" {
		t.Fatalf("comment after a value: %q", out)
	}
	// M13: name: "shikii" in quotes is shikii's skill
	dir := filepath.Join(t.TempDir(), "shikii")
	writeFileT(t, filepath.Join(dir, "SKILL.md"), "---\nname: \"shikii\"\n---\n")
	if ok, err := shikiiSkillDir(dir); !ok || err != nil {
		t.Fatalf("quoted name: %v %v", ok, err)
	}
	// M4: a SKILL.md that is a link to someone else's file is not read through
	other := filepath.Join(t.TempDir(), "SKILL.md")
	writeFileT(t, other, "---\nname: shikii\n---\n")
	linked := filepath.Join(t.TempDir(), "shikii")
	os.MkdirAll(linked, 0o755)
	os.Symlink(other, filepath.Join(linked, "SKILL.md"))
	if ok, _ := shikiiSkillDir(linked); ok {
		t.Fatal("a SKILL.md link must not make the folder shikii's")
	}
}

// M11, M16, M2, M3 through the whole command.
func TestUninstAuditHolesCommand(t *testing.T) {
	b := newUninstallBox(t)
	// M11: .mcp.json kept elsewhere through a link: the link stays, the file it points at loses shikii's entry
	mcp := filepath.Join(b.repo, ".mcp.json")
	real := filepath.Join(b.home, "dotfiles", "mcp.json")
	body, _ := os.ReadFile(mcp)
	writeFileT(t, real, string(body))
	os.Remove(mcp)
	os.Symlink(real, mcp)
	// M16: a broken opencode.json is reported and left as it is
	oc := filepath.Join(b.repo, "opencode.json")
	writeFileT(t, oc, "{ broken shikii")
	// M2 / M3: a mark under the wrong name, and a mark whose pid is gone, are never stopped
	good, _ := os.ReadFile(pagePath(b.agent, b.repo))
	writeFileT(t, filepath.Join(b.agent, "pages", "not-its-id.json"), string(good))
	dead := 99999999
	writePageRecord(b.agent, filepath.Join(b.home, "gone"), 40003, dead)
	cmd := processCommand
	processCommand = func(pid int) string {
		if pid == dead {
			return b.exe + " serve --store " + filepath.Join(b.home, "gone", ".shikii") + " --port 0"
		}
		return cmd(pid)
	}
	var runErr error
	out := capture(t, func() error { runErr = cmdUninstall([]string{"--yes"}); return nil })
	if info, err := os.Lstat(mcp); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf(".mcp.json must stay a link:\n%s", out)
	}
	if got, _ := os.ReadFile(real); strings.Contains(string(got), `"shikii"`) {
		t.Fatalf("the linked .mcp.json must lose shikii's entry:\n%s", got)
	}
	if got, _ := os.ReadFile(oc); string(got) != "{ broken shikii" || runErr == nil || !strings.Contains(out, "could not check or change: "+oc) {
		t.Fatalf("a broken opencode.json is left, named, and the run fails: err=%v\n%s", runErr, out)
	}
	if len(b.stopped) != 1 || b.stopped[0] != os.Getppid() {
		t.Fatalf("only the project's own page is stopped once: %v", b.stopped)
	}
}

// A5: a binary path with a space. 0.9.3 and earlier wrote `/a b/shikii hook stop` unquoted (a shell runs `/a`, so the hook never worked),
// and uninstall did not see it as shikii's. The hook command is now quoted, both forms are shikii's, and uninstall removes both.
func TestUninstAuditHooksWithASpaceInThePath(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/a b/shikii hook stop":                 true,
		"'/a b/shikii' hook user-prompt-submit": true,
		"'/o'\\''neil/shikii' hook stop":        true,
		"/opt/bin/shikii hook stop":             true,
		"shikii hook pre-tool-use":              true,
		"rtk hook claude":                       false,
		"shikii-tool run hook x":                false,
		"node /x/shikii.mjs":                    false,
		"/usr/bin/env shikii hook stop":         false,
		"bash -c 'cd /x && shikii hook stop'":   false,
	} {
		if got := isShikiiHook(cmd); got != want {
			t.Fatalf("isShikiiHook(%q) = %v, want %v", cmd, got, want)
		}
	}
	// the command install writes runs the binary through a shell even with a space and a quote in its path
	bin := filepath.Join(t.TempDir(), "my o'bin", "shikii")
	writeFileT(t, bin, "#!/bin/sh\necho \"ran $1 $2\"\n")
	os.Chmod(bin, 0o755)
	out, err := exec.Command("/bin/sh", "-c", hookCommand(bin, "stop")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ran hook stop" {
		t.Fatalf("the hook command must run through sh: %v %q (%s)", err, out, hookCommand(bin, "stop"))
	}
	b := newUninstallBox(t)
	settings := filepath.Join(b.home, ".claude", "settings.json")
	writeFileT(t, settings, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/a b/shikii hook stop"},{"type":"command","command":"'/a b/shikii' hook stop"},{"type":"command","command":"rtk hook claude"}]}]}}`)
	out2 := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	got, _ := os.ReadFile(settings)
	if strings.Contains(string(got), "shikii") || !strings.Contains(string(got), "rtk hook claude") {
		t.Fatalf("both forms of shikii's hook go, the person's stays:\n%s\n%s", got, out2)
	}
}

// A6: run through a link (~/.local/bin/shikii -> the real binary), the rm command names the link, the real binary and the readout next to it.
func TestUninstAuditRmNamesTheRealBinaryBehindALink(t *testing.T) {
	b := newUninstallBox(t)
	link := filepath.Join(b.home, ".local", "bin", "shikii")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(b.exe, link); err != nil {
		t.Fatal(err)
	}
	executablePath = func() (string, error) { return link, nil }
	out := capture(t, func() error { return cmdUninstall(nil) })
	realExe, _ := filepath.EvalSymlinks(b.exe)
	realReadout := filepath.Join(filepath.Dir(realExe), "shikii-readout")
	if want := "rm " + shellQuote(link) + " " + shellQuote(realExe) + " " + shellQuote(realReadout); !strings.Contains(out, want) {
		t.Fatalf("the rm command must name the link and what it points at (%s):\n%s", want, out)
	}
}

// A6b: a binary in a folder reached through a linked folder (/tmp -> /private/tmp) is not a link itself: it is named once.
func TestUninstAuditRmNamesABinaryOnce(t *testing.T) {
	b := newUninstallBox(t)
	via := filepath.Join(t.TempDir(), "via")
	if err := os.Symlink(filepath.Dir(b.exe), via); err != nil {
		t.Fatal(err)
	}
	executablePath = func() (string, error) { return filepath.Join(via, "shikii"), nil }
	out := capture(t, func() error { return cmdUninstall(nil) })
	want := "  rm " + shellQuote(filepath.Join(via, "shikii")) + " " + shellQuote(filepath.Join(via, "shikii-readout")) + "\n"
	if !strings.Contains(out, want) {
		t.Fatalf("the binary and readout are named once each (%q):\n%s", want, out)
	}
}
