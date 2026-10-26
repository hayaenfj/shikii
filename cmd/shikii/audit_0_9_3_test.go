package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

func probeBlock(cmd string) string {
	return "[mcp_servers.shikii]\ncommand = " + quotedList([]string{cmd}) + "\nargs = [" + quotedList(codexServerArgs) + "]\n"
}

// symlinked config.toml (dotfiles): the link must stay a link and the target gets the fix
func TestAuditProbeSymlinkKept(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "config.toml")
	os.MkdirAll(filepath.Dir(target), 0o700)
	os.WriteFile(target, []byte("model = \"o3\"\n\n"+probeBlock("shikii")), 0o600)
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	did, err := fixCodexServer(link, codexServerArgs)
	if err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink == 0 {
		b, _ := os.ReadFile(target)
		t.Fatalf("the symlink was replaced by a regular file; the dotfiles target still says:\n%s", b)
	}
}

// the header occurs once but not followed by "\ncommand = " (here in a comment before a CRLF block): must not panic
func TestAuditProbeNoHeadNoPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	text := "[mcp_servers.shikii]\r\n"
	os.WriteFile(path, []byte(text), 0o600)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("fixCodexServer panicked: %v", r)
		}
	}()
	did, err := fixCodexServer(path, codexServerArgs)
	b, _ := os.ReadFile(path)
	if did || err != nil || string(b) != text {
		t.Fatalf("a file without shikii's block must stay: did=%v err=%v\n%q", did, err, b)
	}
}

// a config.toml the person made read-only is not replaced
func TestAuditProbeReadOnlyKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	text := probeBlock("shikii")
	os.WriteFile(path, []byte(text), 0o400)
	did, err := fixCodexServer(path, codexServerArgs)
	b, _ := os.ReadFile(path)
	if did || err != nil || string(b) != text {
		t.Fatalf("a read-only config is left alone: did=%v err=%v\n%s", did, err, b)
	}
}

// init (not only the update) fixes the shikii-written block
func TestAuditProbeInitFixesCodexServer(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	t.Chdir(gitRepo(t))
	path := filepath.Join(home, ".codex", "config.toml")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(probeBlock("shikii")), 0o600)
	out := capture(t, func() error { return cmdInit(nil) })
	b, _ := os.ReadFile(path)
	exe, _ := os.Executable()
	if !strings.Contains(string(b), "command = "+quotedList([]string{exe})) || !strings.Contains(out, "pointed the shikii MCP server") {
		t.Fatalf("init points the shikii-written Codex MCP server at this binary:\n%s\n%s", b, out)
	}
}

// English notice for a project copy from 0.9.1 (display.ja only): name it like the page does (the title in any language), not "this question"
func TestAuditProbeTitleFallsBackLikeThePage(t *testing.T) {
	sp := &spec.Spec{ID: "needs-plan", Display: map[string]spec.Display{"ja": {Title: "始める前に聞くべきだったか"}}}
	if got := questionTitle(sp, "en"); got != "始める前に聞くべきだったか" {
		t.Fatalf("the English notice names the question as the page does: %q", got)
	}
	if got := questionTitle(&spec.Spec{ID: "my-question"}, "ja"); got != "my-question" {
		t.Fatalf("with no display at all the page shows the id; the notice must name something the person can find: %q", got)
	}
}

// jaCounts uses the page's answer names
func TestAuditProbeJaCounts(t *testing.T) {
	sp := &spec.Spec{Display: map[string]spec.Display{"ja": {Options: map[string]string{"edit": "変更"}}}}
	if got := jaCounts(sp, map[string]int{"edit": 2}); got != "変更 2 件" {
		t.Fatalf("display.ja.options name: %q", got)
	}
	if got := jaCounts(&spec.Spec{}, map[string]int{"no": 10, "yes": 1}); got != "いいえ 10 件・はい 1 件" {
		t.Fatalf("yes/no in plain Japanese: %q", got)
	}
}
