package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

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

// A4: broken settings stop init (nothing is rewritten). Rewriting a .mcp.json / opencode.json that neither Claude Code nor OpenCode
// can read from only the part init read drops the person's values.
func TestFinalAEA4BrokenJSONSettingsAreNotRewritten(t *testing.T) {
	cases := map[string]string{
		"mcpServers \u304c\u914d\u5217":       "{\n  \"mcpServers\": [\"keep-me\"]\n}\n",
		"mcpServers \u304c\u6587\u5b57\u5217": "{\n  \"mcpServers\": \"see ~/servers.json\"\n}\n",
		"\u6700\u521d\u306e\u5024\u306e\u5f8c\u306b\u7d9a\u304d\u304c\u3042\u308b(JSON \u3067\u306a\u3044)": "{\"mcpServers\": {}}\n{\"other\": {\"command\": \"keep-me\"}}\n",
	}
	for name, text := range cases {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		b, _ := os.ReadFile(p)
		if string(b) != text {
			t.Errorf("A4 (%s): broken settings were rewritten (did=%v err=%v)\nbefore:\n%safter:\n%s", name, did, err, text, b)
		}
		if err == nil {
			t.Errorf("A4 (%s): init does not stop on broken settings (did=%v)", name, did)
		}
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

// R6 (A1/A4): a hard-linked config.toml is rewritten in place (O_TRUNC, then write). When the write fails after the truncation (here the
// disk fills up), the person's file is left cut short: neither the old content nor the new.
func TestRe2HardLinkInPlaceFailureKeepsTheFile(t *testing.T) {
	mnt := os.Getenv("RE2_MNT")
	if mnt == "" {
		t.Skip("RE2_MNT")
	}
	const bs = 4096
	for k := 8; k <= 14; k++ {
		dir := filepath.Join(mnt, fmt.Sprintf("k%d", k))
		os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// old content: 8 blocks less 10 bytes, so the new one needs a 9th block
		old := "model = \"m\"\n" + strings.Repeat("# x\n", (8*bs-10-12)/4)
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, filepath.Join(dir, "dotfiles.toml")); err != nil {
			t.Fatal(err)
		}
		filler := filepath.Join(dir, "filler")
		f, _ := os.Create(filler)
		chunk := make([]byte, bs)
		n := 0
		for {
			if _, err := f.Write(chunk); err != nil {
				break
			}
			n++
		}
		f.Close()
		var st syscall.Statfs_t
		syscall.Statfs(mnt, &st)
		_ = os.Truncate(filler, int64((n-k)*bs))
		syscall.Statfs(mnt, &st)
		did, err := addCodexServer(path, []string{"mcp"})
		got, _ := os.ReadFile(path)
		t.Logf("k=%d free blocks=%d did=%v err=%v len(old)=%d len(got)=%d", k, st.Bavail, did, err, len(old), len(got))
		os.Remove(filler)
		if err != nil && string(got) != old {
			t.Fatalf("A1/A4: a failed write changed the file: %d bytes left of %d, has shikii=%v, err=%v", len(got), len(old), strings.Contains(string(got), "mcp_servers.shikii"), err)
		}
		os.RemoveAll(dir)
	}
}

// Audit 0.8.1 item 2: CI runs with read-only permissions, cancels older runs on the same branch, and caches npm downloads (CI cost).
func TestCIWorkflowIsReadOnlyAndCached(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	if strings.Contains(ci, "docker build") || strings.Contains(ci, "docker/build-push-action") {
		t.Error("Docker image publication is retired; CI must not require an image build")
	}
	// Audit 0.8.2-1 item 7: no write permissions mixed in, and concurrency has a group per ref
	if strings.Contains(ci, ": write") || strings.Contains(ci, "write-all") {
		t.Error("test.yml has a write permission")
	}
	for _, want := range []string{
		"permissions:\n  contents: read\nconcurrency:",
		"group: test-${{ github.ref }}",
		"cancel-in-progress: true",
		"cache: npm",
		"cache-dependency-path: web/package-lock.json",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("test.yml does not have %q", want)
		}
	}
}

// Medium 3: Codex asks to trust hooks again whenever hooks.json changes (a new path or timeout), and codex exec skips untrusted hooks silently.
// The note shown when install changes Codex's hooks.json says so, instead of "once".
func TestCodexHookNoteSaysTrustAgainWheneverItChanges(t *testing.T) {
	var note string
	for _, h := range hookTargets {
		if h.who == "Codex" {
			note = h.note
		}
	}
	if !strings.Contains(note, "/hooks") || !strings.Contains(note, "whenever") || !strings.Contains(note, "skips") || strings.Contains(note, "once") {
		t.Fatalf("the Codex note says to trust again whenever hooks.json changes, and that Codex skips untrusted hooks: %q", note)
	}
}

// 0.9.3: Codex reads shikii's MCP server only from the shared ~/.codex/config.toml and starts it in the folder its daemon started in, so the shared
// registration names no store: `command = <this binary>`, `args = ["mcp", "--local", ...]`, and each tool call passes project. The update and init
// point a bare name or a path that no longer exists in that form at this binary. A block a person edited, or in an older version's form (0.9.1:
// command "shikii" with mcp --store .shikii --local ...; 0.9.2: an absolute command with the same args), is left alone (0.9.7: only told).
func TestCodexServerCommandIsFixedOnlyInShikiisOwnBlock(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := []string{"mcp", "--store", ".shikii", "--local", "http://127.0.0.1:8080"}
	block := func(cmd string, args []string) string {
		return "[mcp_servers.shikii]\ncommand = " + quotedList([]string{cmd}) + "\nargs = [" + quotedList(args) + "]\n"
	}
	other := "model = \"o3\"\n\n[mcp_servers.other]\ncommand = \"x\"\n\n"
	gone := filepath.Join(t.TempDir(), "old", "shikii")
	existing := filepath.Join(t.TempDir(), "shikii")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinned := []string{"mcp", "--store", "/Users/someone/project/.shikii", "--local", "http://127.0.0.1:8080"}
	for name, c := range map[string]struct {
		before string
		fixed  bool
	}{
		"0.9.1 form (only told)":                      {other + block("shikii", old), false},
		"0.9.2 form (only told)":                      {other + block(exe, old), false},
		"0.9.2 form, another binary (only told)":      {other + block(existing, old), false},
		"0.9.2 form, a path that is gone (only told)": {other + block(gone, old), false},
		"new form with a bare name":                   {other + block("shikii", codexServerArgs), true},
		"new form with a path that no longer exists":  {other + block(gone, codexServerArgs), true},
		"new form, another existing binary (kept)":    {other + block(existing, codexServerArgs), false},
		"new form, already this binary":               {other + block(exe, codexServerArgs), false},
		"an absolute --store a person wrote (kept)":   {other + block(exe, pinned), false},
		"args a person changed (kept)":                {other + block("shikii", append([]string{"mcp", "--proposer", "http://127.0.0.1:8083"}, old[1:]...)), false},
		"a line a person added (kept)":                {other + block("shikii", old) + "env = { A = \"1\" }\n", false},
		"a relative command (kept)":                   {other + block("./bin/shikii", old), false},
		"no shikii block (nothing to do)":             {other, false},
		"two shikii blocks (unclear, kept)":           {block("shikii", old) + "\n" + block("shikii", old), false},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(c.before), 0o600); err != nil {
			t.Fatal(err)
		}
		did, err := fixCodexServer(path, codexServerArgs)
		after, _ := os.ReadFile(path)
		if err != nil || did != c.fixed {
			t.Fatalf("%s: fixed=%v err=%v\n%s", name, did, err, after)
		}
		want := c.before
		if c.fixed {
			want = other + block(exe, codexServerArgs)
		}
		if string(after) != want {
			t.Fatalf("%s: the file must become\n%s\ngot\n%s", name, want, after)
		}
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: the file keeps its permissions: %v", name, fi.Mode())
		}
	}
}

// G3: Codex's shikii server in an older version's form is left as it is; init and the update say what to write instead. Only the current
// form whose binary is gone is pointed at this binary.
func TestOlderCodexFormIsOnlyTold(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	t.Chdir(gitRepo(t))
	capture(t, func() error { return cmdInit(nil) })
	path := filepath.Join(home, ".codex", "config.toml")
	exe, _ := os.Executable()
	for name, before := range map[string]string{
		"0.9.1":       "[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexMCPArgs) + "]\n",
		"0.9.2":       "[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\nargs = [" + quotedList(codexMCPArgs) + "]\n",
		"0.9.3 build": "[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\nargs = [\"mcp\"]\n",
	} {
		for _, run := range []func() error{func() error { return cmdInit(nil) }, func() error { return cmdHook([]string{"install", "--yes"}) }} {
			if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
				t.Fatal(err)
			}
			out := capture(t, run)
			if b, _ := os.ReadFile(path); string(b) != before {
				t.Fatalf("G3 %s: the older form is left as it is, got\n%s\n%s", name, b, out)
			}
			if !strings.Contains(out, "an older shikii wrote") || !strings.Contains(out, codexServerArgsLine) {
				t.Fatalf("G3 %s: says what to write instead:\n%s", name, out)
			}
		}
	}
	gone := "[mcp_servers.shikii]\ncommand = \"/nowhere/shikii\"\n" + codexServerArgsLine + "\n"
	os.WriteFile(path, []byte(gone), 0o644)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if b, _ := os.ReadFile(path); string(b) != "[mcp_servers.shikii]\ncommand = "+quotedList([]string{exe})+"\n"+codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n" {
		t.Fatalf("control: the current form on a binary that is gone is pointed at this one: %s", b)
	}
}

// Confirmation audit 1 (A4): readJSONSettings accepts a top-level JSON null as settings. Decoding null into the map sets it to nil, and
// addJSONEntry then assigns into that nil map: init panics instead of saying the settings are broken.
func TestConfirm1A4NullJSONSettingsStopInitWithAnError(t *testing.T) {
	for _, text := range []string{"null\n", "null"} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		var did bool
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("A4 (%q): init panics on broken settings instead of stopping with an error: %v", text, r)
				}
			}()
			did, err = addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		}()
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Errorf("A4 (%q): broken settings were rewritten: %q", text, b)
		}
		if err == nil {
			t.Errorf("A4 (%q): init does not stop on broken settings with an error (did=%v)", text, did)
		}
	}
}

// Confirmation audit 2 (A4): a settings file whose bytes are not UTF-8 is not JSON (RFC 8259 8.1), so it is broken settings. Go's decoder
// silently turns each bad byte into U+FFFD, and init / uninstall then rewrite the file: the person's original bytes are gone.
func TestConfirm2A4InvalidUTF8SettingsAreNotRewritten(t *testing.T) {
	text := "{\"env\": {\"TOKEN\": \"p\xe4ss\xff\"}, \"mcpServers\": {\"other\": {\"command\": \"x\"}}}\n"
	t.Run("init", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Errorf("A4: settings that are not UTF-8 were rewritten (did=%v, err=%v): %q", did, err, b)
		}
	})
	t.Run("uninstall", func(t *testing.T) {
		withShikii := "{\"env\": {\"TOKEN\": \"p\xe4ss\xff\"}, \"mcpServers\": {\"shikii\": {\"command\": \"shikii\"}}}\n"
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(withShikii), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := removeJSONEntry(p, "mcpServers", "shikii", true)
		if b, _ := os.ReadFile(p); string(b) != withShikii {
			t.Errorf("A4: settings that are not UTF-8 were rewritten (did=%v, err=%v): %q", did, err, b)
		}
	})
}

// Control: settings that write U+FFFD themselves (as a character or as \ufffd) and other non-ASCII text are kept and shikii is added.
func TestConfirm2SettingsWritingFFFDThemselvesAreKept(t *testing.T) {
	for _, text := range []string{
		"{\"env\": {\"A\": \"\ufffd\", \"B\": \"\u65e5\u672c\"}}\n",
		"{\"env\": {\"A\": \"\\ufffd\"}}\n",
	} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"}); !did || err != nil {
			t.Fatalf("control (%q): shikii is added: %v %v", text, did, err)
		}
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), "\"shikii\"") || !strings.Contains(string(b), "\"A\"") {
			t.Fatalf("control (%q): the person's text is kept: %s", text, b)
		}
	}
}

// A Japanese document (*.ja.md, docs/readme/README_ja.md, docs/guide.ja.md) links the Japanese version of a document when there is one,
// and the English guide does not call an English reference "(Japanese)" (audit 0.9.2 translation, low 6).
func TestJapaneseDocumentsLinkJapaneseVersions(t *testing.T) {
	repo := filepath.Join("..", "..")
	link := regexp.MustCompile(`\]\(([^)#\s]+\.md)(#[^)]*)?\)`)
	var docs []string
	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && path != repo {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".ja.md") || strings.HasSuffix(path, "README_ja.md") {
			docs = append(docs, path)
		}
		return nil
	})
	if len(docs) < 10 {
		t.Fatalf("found only %d Japanese documents", len(docs))
	}
	for _, doc := range docs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range link.FindAllStringSubmatch(line, -1) {
				target := m[1]
				if strings.Contains(target, "://") || strings.HasSuffix(target, ".ja.md") || strings.HasSuffix(target, "README_ja.md") {
					continue
				}
				ja := strings.TrimSuffix(target, ".md") + ".ja.md"
				if _, err := os.Stat(filepath.Join(filepath.Dir(doc), ja)); err == nil {
					if strings.Contains(line, "English") || strings.Contains(line, "\u82f1\u8a9e") { // the language switcher names the English version on purpose
						continue
					}
					t.Errorf("%s:%d links %s although %s exists", doc, i+1, target, ja)
				}
			}
		}
	}
	guide, err := os.ReadFile(filepath.Join(repo, "docs", "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(guide), "references/review.md) (Japanese)") {
		t.Error("docs/guide.md calls references/review.md Japanese, but it is English")
	}
}

// The whole path on a disk without hard links: init stopped while writing a built-in file leaves nothing at its path, so the next init
// places it in full (0.9.9 audit: it left a part, which every later init kept).
func TestPatch0910StoppedSeedWithoutHardLinksIsPlacedNextTime(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	write := store.WriteTemp
	store.WriteTemp = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, syscall.EINTR
	}
	t.Cleanup(func() { store.Link, store.WriteTemp = os.Link, write })
	path := filepath.Join(t.TempDir(), "needs-plan.yaml")
	if _, err := seedFile(path, builtInSpec); err == nil {
		t.Fatal("the stopped write is an error")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("nothing is left at the path: %v", err)
	}
	store.WriteTemp = write
	if created, err := seedFile(path, builtInSpec); err != nil || !created {
		t.Fatalf("the next init places it: %v %v", created, err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(builtInSpec) {
		t.Fatalf("got %q", b)
	}
}

// finding 2: a broken JSON with "//" outside a string (a URL without quotes) was called JSONC, and init went on as if the file were fine.
// Only a file that is valid JSON once its comments are taken out is JSONC; a broken one is refused as broken.
// Confirmation i-2: OpenCode's JSONC also allows trailing commas; with comments and a trailing comma init stopped as if it were broken.
// A JSONC file is left as written with an error that names the comments (init goes on with the entry to add by hand).
func TestPatch099AuditBrokenJSONIsNotCalledJSONC(t *testing.T) {
	for _, c := range []struct {
		name, text string
		jsonc      bool
	}{
		{"a URL without quotes is broken", `{"$schema": https://opencode.ai/config.json, "mcp": {}}`, false},
		{"comments and a trailing comma are JSONC", "{\n // mine\n \"mcp\": {\"other\": {\"type\": \"local\"},},\n}\n", true},
	} {
		p := filepath.Join(t.TempDir(), "opencode.json")
		if err := os.WriteFile(p, []byte(c.text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
		if c.jsonc && (!errors.As(err, new(*personFileError)) || !strings.Contains(err.Error(), "comments")) {
			t.Fatalf("%s: JSONC is left as written and init goes on: %v", c.name, err)
		}
		if !c.jsonc && (err == nil || strings.Contains(err.Error(), "comments") || !strings.Contains(err.Error(), "broken settings")) {
			t.Fatalf("%s: a broken JSON is refused as broken, not as comments: %v", c.name, err)
		}
		if b, _ := os.ReadFile(p); string(b) != c.text {
			t.Fatalf("%s: the file is left as written: %q", c.name, b)
		}
	}
}

// finding 3: the person added shikii by hand to an opencode.json with comments, as init told them, and init kept telling them to add it.
func TestPatch099AuditJSONCWithShikiiAlreadyThereIsKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	text := "{\n // mine\n \"mcp\": {\"shikii\": {\"type\": \"local\", \"command\": [\"shikii\", \"mcp\"]}}\n}\n"
	os.WriteFile(p, []byte(text), 0o644)
	did, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
	if err != nil || did {
		t.Fatalf("shikii is already there: kept, nothing to do: %v %v", did, err)
	}
	if b, _ := os.ReadFile(p); string(b) != text {
		t.Fatalf("and the file is left as written: %q", b)
	}
}

// C confirm1-6: OpenCode reads comments in opencode.json (JSONC). shikii cannot keep them when it rewrites the file, so it leaves the file
// as written, and init goes on with the entry to add by hand instead of stopping as if the settings were broken.
func TestPatch099CommentsInOpencodeJSONLetInitGoOn(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	text := "{\n  // my servers\n  \"mcp\": {\"other\": {\"type\": \"local\", \"command\": [\"x\"]}} /* end */\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "opencode.json"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	out := capture(t, func() error { return cmdInit(nil) })
	if b, _ := os.ReadFile(filepath.Join(repo, "opencode.json")); string(b) != text {
		t.Fatalf("opencode.json with comments is left as written: %q", b)
	}
	if !strings.Contains(out, "opencode.json: shikii did not add itself") || !strings.Contains(out, "comments") || !strings.Contains(out, "Ready:") {
		t.Fatalf("init says why and goes on:\n%s", out)
	}
	// control: a "//" inside a string is not a comment
	p := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(p, []byte(`{"$schema": "https://opencode.ai/config.json", "mcp": {}}`), 0o644)
	if did, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"}); err != nil || !did {
		t.Fatalf("control: a URL in a string is not a comment: %v %v", did, err)
	}
}

// C confirm2-1: an empty settings file (touch .mcp.json) is refused with only "EOF". It is still refused (not JSON), but the message says
// the file is empty.
func TestPatch099EmptySettingsFileSaysItIsEmpty(t *testing.T) {
	for _, text := range []string{"", "  \n\t\n"} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		os.WriteFile(p, []byte(text), 0o644)
		_, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("an empty file is refused and the message says it is empty: %v", err)
		}
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Fatalf("and left as it is: %q", b)
		}
	}
}

// C confirm1-1: on a disk without hard links, init stopped at the built-in decisions with "operation not supported" (seedFile used
// os.Link). It now writes the file in place there.
func TestPatch099InitOnADiskWithoutHardLinks(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { store.Link = os.Link })
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	repo := gitRepo(t)
	t.Chdir(repo)
	if err := cmdInit(nil); err != nil {
		t.Fatalf("init works on a disk without hard links: %v", err)
	}
	for _, f := range []string{"decisions/needs-plan.yaml", "records/store-id"} {
		if b, err := os.ReadFile(filepath.Join(repo, ".shikii", f)); err != nil || len(b) == 0 {
			t.Fatalf("%s is written: %v", f, err)
		}
	}
}

// A ~/.codex/config.toml shikii cannot write, whose shikii server still points at a binary that is gone: init says what to change by hand
// and goes on, and the file is left as it is (0.9.9, AE-2). Either cause: a read-only file, or a disk mounted read-only (EROFS).
func TestUnwritableCodexConfigLetsInitGoOn(t *testing.T) {
	for _, cause := range []string{"read-only file", "read-only disk"} {
		t.Run(cause, func(t *testing.T) {
			hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
			home, _ := os.UserHomeDir()
			repo := gitRepo(t)
			t.Chdir(repo)
			path := filepath.Join(home, ".codex", "config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			text := "[mcp_servers.shikii]\ncommand = \"/nowhere/shikii\"\n" + codexServerArgsLine + "\n"
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			if cause == "read-only file" {
				if err := os.Chmod(path, 0o444); err != nil { // WriteFile keeps the mode of a file that is already there
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(path, 0o644) })
			} else {
				openPersonFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
					if strings.HasSuffix(name, "config.toml") {
						return nil, &os.PathError{Op: "open", Path: name, Err: syscall.EROFS}
					}
					return os.OpenFile(name, flag, perm)
				}
				t.Cleanup(func() { openPersonFile = os.OpenFile })
			}
			var err error
			out := capture(t, func() error { err = cmdInit(nil); return nil })
			if err != nil {
				t.Fatalf("init goes on: %v\n%s", err, out)
			}
			if b, _ := os.ReadFile(path); string(b) != text {
				t.Fatalf("the file is left as it is: %q", b)
			}
			if !strings.Contains(out, "config.toml") || !strings.Contains(out, "command = "+quotedList([]string{mcpCommand()})) || !strings.Contains(out, "Ready:") {
				t.Fatalf("init says what to change by hand:\n%s", out)
			}
		})
	}
}
