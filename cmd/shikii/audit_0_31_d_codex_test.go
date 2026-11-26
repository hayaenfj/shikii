package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Tests of audit D (0.31.0, second round) for what the fixes of Codex's registration and of init claim. Each failed on a copy where
// the named line was taken out (the report lists the lines: mutations M15, M16, M17, M18, M19, M21; M16, M17 and M21 are caught by
// TestAuditCCodexNoteAndLineCases and TestAuditCInitSaysWhoGotTheHooks in audit_0_31_c_codex_test.go, where their tests were dropped).

// codexPreviewPromise is what hook install shows before it adds the approval line.
const codexPreviewPromise = "Codex will run shikii's MCP tools without asking each time"

// M18 (hook_install.go, the preview's check that the file can be written taken out), M19 (codex_tools_allowed.go, the line that
// says the file was left taken out), M15 (codex_tools_allowed.go, the note kept although nothing was written): a config.toml that
// cannot be written.
func TestAuditDGap_ReadOnlyCodexConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only files")
	}
	home := freshMachine(t)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	own := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(path, []byte(own), 0o444); err != nil {
		t.Fatal(err)
	}
	if preview := capture(t, func() error { return hookInstall(nil, true) }); strings.Contains(preview, codexPreviewPromise) {
		t.Errorf("M18: the preview says the line will be added to a file that is read-only")
	}
	out := capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	if after, _ := os.ReadFile(path); string(after) != own {
		t.Fatalf("a read-only config.toml was changed:\n%s", after)
	}
	if !strings.Contains(out, "config.toml: left as it is (it cannot be written") || !strings.Contains(out, "add "+codexToolsAllowedLine+" to the shikii server by hand") {
		t.Errorf("M19: the install does not say that it left the file and which line to add:\n%s", out)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	if after, _ := os.ReadFile(path); !strings.Contains(string(after), codexToolsAllowedLine) {
		t.Errorf("M15: nothing was added while the file was read-only, yet once it can be written the line is not added:\n%s", after)
	}
}

// readOnlyDisk makes shikii see the person's files whose path ends in suffix as on a disk mounted read-only: access(2) and the open
// for writing both answer EROFS, whatever the mode bits say. It returns the function that makes the disk writable again.
func readOnlyDisk(t *testing.T, suffix string) (writable func()) {
	t.Helper()
	oldOpen, oldAccess := openPersonFile, accessPersonFile
	openPersonFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if strings.HasSuffix(name, suffix) {
			return nil, &os.PathError{Op: "open", Path: name, Err: syscall.EROFS}
		}
		return oldOpen(name, flag, perm)
	}
	accessPersonFile = func(path string, mode uint32) error {
		if strings.HasSuffix(path, suffix) {
			return syscall.EROFS
		}
		return oldAccess(path, mode)
	}
	writable = func() { openPersonFile, accessPersonFile = oldOpen, oldAccess }
	t.Cleanup(writable)
	return writable
}

// audit D, finding U-3: the preview looked at the mode bits, and the write is decided by whether the file opens. On a disk mounted
// read-only (an ACL and an immutable flag act the same) the mode bits say the file can be written: the preview said the line will
// be added, and the install then left the file as it is. Both now ask the same thing (personFileWritable).
func TestAuditDCodexPreviewAgreesWithTheInstallOnAReadOnlyDisk(t *testing.T) {
	home := freshMachine(t)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	own := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(path, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	writable := readOnlyDisk(t, "config.toml")
	if preview := capture(t, func() error { return hookInstall(nil, true) }); strings.Contains(preview, codexPreviewPromise) {
		t.Errorf("the preview says the line will be added to a file on a disk mounted read-only (its mode bits say it can be written)")
	}
	out := capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	after, _ := os.ReadFile(path)
	if string(after) != own || strings.Contains(out, codexPreviewPromise) || !strings.Contains(out, "config.toml: left as it is (it cannot be written") || fileExists(codexToolsAllowedMark()) {
		t.Errorf("the install on a disk mounted read-only: file changed=%v, promised=%v, note left=%v\n%s", string(after) != own, strings.Contains(out, codexPreviewPromise), fileExists(codexToolsAllowedMark()), out)
	}
	// control: the same file once the disk can be written. The preview says it, and the install does it.
	writable()
	if preview := capture(t, func() error { return hookInstall(nil, true) }); !strings.Contains(preview, codexPreviewPromise) {
		t.Errorf("control: the preview of a file that can be written says the line will be added:\n%s", preview)
	}
	out = capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	if after, _ = os.ReadFile(path); string(after) != own+codexToolsAllowedLine+"\n" || !strings.Contains(out, "without Codex asking") {
		t.Errorf("control: the install adds the line to a file that can be written:\n%s", after)
	}
}

// The preview reads the registration as the install leaves it. A command that is gone (an earlier binary, whatever its name) is
// pointed at this binary first, and that registration gets the line: the preview says so, and the install does it. A command of
// the person's that exists is not shikii's: the preview says nothing, and the install leaves the file as it is.
func TestAuditDCodexPreviewFollowsTheInstallForACommandThatIsGone(t *testing.T) {
	t.Run("a command that is gone", func(t *testing.T) {
		home := freshMachine(t)
		path := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		gone := "[mcp_servers.shikii]\ncommand = \"/nowhere/shikii-0.30.8\"\n" + codexServerArgsLine + "\n"
		if err := os.WriteFile(path, []byte(gone), 0o644); err != nil {
			t.Fatal(err)
		}
		if preview := capture(t, func() error { return hookInstall(nil, true) }); !strings.Contains(preview, codexPreviewPromise) {
			t.Errorf("the install points this registration at the binary and adds the line, and the preview does not say so")
		}
		if after, _ := os.ReadFile(path); string(after) != gone {
			t.Fatalf("the preview changed the file:\n%s", after)
		}
		capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
		want := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n"
		if after, _ := os.ReadFile(path); string(after) != want {
			t.Fatalf("precondition: the install points the registration at this binary and adds the line:\n%s", after)
		}
	})
	t.Run("a wrapper of the person's that exists", func(t *testing.T) {
		home := freshMachine(t)
		path := filepath.Join(home, ".codex", "config.toml")
		wrapper := filepath.Join(home, "bin", "shikii-wrapper.sh")
		for _, dir := range []string{filepath.Dir(path), filepath.Dir(wrapper)} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeUnrunExecutable(t, wrapper, "#!/bin/sh\nexec shikii \"$@\"\n")
		theirs := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{wrapper}) + "\n" + codexServerArgsLine + "\n"
		if err := os.WriteFile(path, []byte(theirs), 0o644); err != nil {
			t.Fatal(err)
		}
		if preview := capture(t, func() error { return hookInstall(nil, true) }); strings.Contains(preview, codexPreviewPromise) {
			t.Errorf("the preview says the line will be added to a registration whose command is the person's wrapper")
		}
		capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
		if after, _ := os.ReadFile(path); string(after) != theirs {
			t.Errorf("the install changed a registration whose command is the person's wrapper:\n%s", after)
		}
	})
}
