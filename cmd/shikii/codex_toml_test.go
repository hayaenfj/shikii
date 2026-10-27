package main

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// Second 0.9.3 audit: Codex's shared registration (args = ["mcp"]) dropped --local, which every other registration and the hook pass.
// Without a key, shikii_decide on Codex then had no endpoint at all ("no permitted endpoint") where 0.9.2 reached the local model.
func TestAudit2CodexRegistrationKeepsTheLocalModel(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.toml")
	if did, err := addCodexServer(fresh, codexServerArgs); err != nil || !did {
		t.Fatalf("did=%v err=%v", did, err)
	}
	b, _ := os.ReadFile(fresh)
	if !strings.Contains(string(b), `"--local", "`+hookLocalURL+`"`) || strings.Contains(string(b), "--store") {
		t.Fatalf("Codex's registration must keep the local model and name no store:\n%s", b)
	}
	// older forms (the 0.9.2 one, args = ["mcp"] of 0.9.3 builds) are only told since 0.9.7: TestOlderCodexFormIsOnlyTold
}

// 0.9.6 audit, middle 1 (the message): when init leaves config.toml alone because shikii may already be there, it still shows the block to add,
// since "kept the existing MCP settings" alone was what hid the missing registration before.
func TestCodexNoteShowsTheBlockWhenLeftAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("note = \"\"\"\n[mcp_servers.shikii]\n\"\"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "[mcp_servers.shikii]") || !strings.Contains(note, "command = ") {
		t.Fatalf("the note shows the block to add: %q", note)
	}
}

// After the 0.9.7 release (init in a second project): when config.toml already registers shikii exactly as init would write it, init said
// "kept the existing MCP settings; if they do not register shikii, add" with the block, as if it might be missing. It now says shikii is
// already registered. A shikii entry in another form still shows the block.
func TestCodexNoteSaysShikiiIsAlreadyRegistered(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	current := "[mcp_servers.other]\ncommand = \"x\"\n\n[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(p, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "already registered") || strings.Contains(note, "if they do not register") {
		t.Fatalf("the note says shikii is already registered: %q", note)
	}
	other := "[mcp_servers.shikii]\ncommand = \"/somewhere/else\"\nargs = [\"mcp\"]\n"
	if err := os.WriteFile(p, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	if note := codexServerNote(p); strings.Contains(note, "already registered") || !strings.Contains(note, codexServerArgsLine) {
		t.Fatalf("control: a shikii entry in another form still shows the block: %q", note)
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

// 0.9.7 final audit (A1 to A5, E1 to E2). The checks use hand-written expected values, not the functions under test.
// Codex 0.164.0 reads every config.toml below (checked with `CODEX_HOME=<temp folder> codex mcp list`): its TOML reader accepts
// TOML 1.1 forms (multi-line inline tables, trailing commas, the \x escape, times without seconds).

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

func TestAuditArrayOfTablesNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[[mcp_servers]]\nname = \"a\"\n"), 0o600)
	note := codexServerNote(path)
	if strings.Contains(note, "inline table") {
		t.Errorf("an array of tables is reported as an inline table: %s", note)
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

// Low 3: the hooks run the binary by its absolute path, but Codex's MCP server was "shikii" (found only on PATH).
func TestCodexMCPServerUsesTheBinaryPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := addCodexServer(path, []string{"mcp"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	m := regexp.MustCompile(`(?m)^command = "([^"]+)"$`).FindSubmatch(b)
	if m == nil || !filepath.IsAbs(string(m[1])) {
		t.Fatalf("Codex's MCP command is the binary's absolute path: %s", b)
	}
}

// The update (hook install) leaves an older version's block (0.9.1, 0.9.2) and says what to write (TestOlderCodexFormIsOnlyTold).
// A block a person edited to name one project's store is left alone, and the update says that every Codex session uses that project.
func TestUpdateFixesCodexServerCommand(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	home, _ := os.UserHomeDir()
	t.Chdir(gitRepo(t))
	capture(t, func() error { return cmdInit(nil) })
	path := filepath.Join(home, ".codex", "config.toml")
	exe, _ := os.Executable()
	for name, c := range map[string]struct {
		before         string
		fixed, warning bool
	}{
		"0.9.1":                       {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexMCPArgs) + "]\n", false, false},
		"0.9.2":                       {"[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\nargs = [" + quotedList(codexMCPArgs) + "]\n", false, false},
		"current form, bare name":     {"[mcp_servers.shikii]\ncommand = \"shikii\"\n" + codexServerArgsLine + "\n", true, false},
		"absolute store by hand":      {"[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\nargs = [\"mcp\", \"--store\", \"/w/a/.shikii\", \"--specs\", \"/w/a/.shikii/decisions\"]\n", false, true},
		"absolute store, = form":      {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\"mcp\", \"--store=/w/a/.shikii\"]\nenv = { A = \"1\" }\n", false, true},
		"edited, no absolute store":   {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\"mcp\", \"--proposer\", \"http://127.0.0.1:8083\"]\n", false, false},
		"relative store kept by hand": {"[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\"mcp\", \"--store\", \".shikii\"]\nenv = { A = \"1\" }\n", false, false},
	} {
		if err := os.WriteFile(path, []byte(c.before), 0o644); err != nil {
			t.Fatal(err)
		}
		out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
		b, _ := os.ReadFile(path)
		want := c.before
		if c.fixed {
			want = "[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\n" + codexServerArgsLine + "\n"
		}
		if string(b) != want {
			t.Fatalf("%s: the file must become\n%s\ngot\n%s\n%s", name, want, b, out)
		}
		if c.fixed != strings.Contains(out, "pointed the shikii MCP server") {
			t.Fatalf("%s: the update says it fixed the block only when it did:\n%s", name, out)
		}
		if c.warning != strings.Contains(out, "every Codex session uses that one project") || c.warning != strings.Contains(out, "set "+codexServerArgsLine) {
			t.Fatalf("%s: the update warns about a hand-edited block that pins one project (and only then):\n%s", name, out)
		}
	}
}

// 0.9.5 check: init left ~/.codex/config.toml alone whenever the word mcp_servers appeared anywhere, so a person who already had one MCP server of
// their own ([mcp_servers.mytool], or only a comment naming mcp_servers) never got shikii's server, while init said "kept the existing MCP
// settings". It now adds [mcp_servers.shikii] unless shikii is already defined or mcp_servers is an inline table (which cannot be extended); in
// that case it says so and shows the block to add by hand.
func TestCodexServerAddedNextToAPersonsOwnServers(t *testing.T) {
	for name, toml := range map[string]string{
		"own table":   "model = \"gpt-5\"\n\n[mcp_servers.mytool]\ncommand = \"mytool\"\n",
		"own section": "[mcp_servers]\n\n[mcp_servers.mytool]\ncommand = \"mytool\"\n",
		"comment":     "# mcp_servers: add later\nmodel = \"gpt-5\"\n",
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || !did || !strings.HasPrefix(string(b), toml) || strings.Count(string(b), "[mcp_servers.shikii]") != 1 {
			t.Fatalf("%s: shikii is added after the person's settings, which stay as they were: did=%v err=%v\n%s", name, did, err, b)
		}
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	inline := "mcp_servers = { mytool = { command = \"mytool\" } }\n"
	if err := os.WriteFile(p, []byte(inline), 0o644); err != nil {
		t.Fatal(err)
	}
	if did, err := addCodexServer(p, []string{"mcp"}); err != nil || did {
		t.Fatalf("an inline mcp_servers table cannot be extended, so the file is left alone: did=%v err=%v", did, err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "[mcp_servers.shikii]") || strings.Contains(note, "kept the existing") {
		t.Fatalf("the person is told shikii was not added and what to add: %q", note)
	}
}

// 0.9.6 audit, middle 1: shapes where shikii is already registered but the first codexServerState missed it and appended a second
// [mcp_servers.shikii] (TOML then refuses the file: "Cannot declare ('mcp_servers', 'shikii') twice"). None of them may be appended to.
func TestCodexServerNotAppendedWhenShikiiIsWrittenAnotherWay(t *testing.T) {
	for name, toml := range map[string]string{
		"comment after the header": "[mcp_servers] # mine\nshikii = { command = \"x\" }\n",
		"CRLF lines":               "[mcp_servers]\r\nshikii = { command = \"x\" }\r\n",
		"single-quoted table":      "[mcp_servers.'shikii']\ncommand = \"x\"\n",
		"single-quoted parent":     "['mcp_servers'.shikii]\ncommand = \"x\"\n",
		"single-quoted key":        "[mcp_servers]\n'shikii' = { command = \"x\" }\n",
		"single-quoted inline":     "'mcp_servers' = { }\n",
		"multi-line string first":  "[mcp_servers]\nnote = '''\n[x]\n'''\nshikii = { command = \"x\" }\n",
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || did || string(b) != toml {
			t.Errorf("%s: a file that already has shikii (or an inline mcp_servers) is left alone: did=%v err=%v\n%s", name, did, err, b)
		}
	}
}

// 0.9.6 recheck, low 1: a key written with \u escapes ([mcp_servers."\u0073hikii"], "shik\u0069i" = ..., "mcp\u005fservers" = {...}) is shikii
// or an inline mcp_servers once unescaped, but the checks read the raw text and appended a second [mcp_servers.shikii]. Any \u or \U outside
// comments now leaves the file alone (the note shows what to add).
func TestCodexServerNotAppendedWithEscapedKeys(t *testing.T) {
	for name, toml := range map[string]string{
		"escaped table":  "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n",
		"escaped key":    "[mcp_servers]\n\"shik\\u0069i\" = { command = \"x\" }\n",
		"escaped inline": "\"mcp\\u005fservers\" = { other = { command = \"x\" } }\n",
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || did || string(b) != toml {
			t.Errorf("%s: left alone: did=%v err=%v\n%s", name, did, err, b)
		}
	}
}

func TestCodexServerIsAddedOnlyWhenATOMLReaderAgrees(t *testing.T) {
	add := map[string]string{
		"words in a value":           "description = \"mcp_servers.shikii is set up by shikii init\"\n",
		"a table name in a string":   "notes = '''\n[mcp_servers.shikii]\n'''\n",
		"another server, odd quotes": "[mcp_servers.'my-tool']\ncommand = 'my-tool' # mcp_servers.shikii later\n",
	}
	for name, text := range add {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || !did {
			t.Fatalf("%s: there is no shikii server, so it is added: did=%v err=%v\n%s", name, did, err, b)
		}
		got := readTOML(t, b)
		server, _ := got["mcp_servers"].(map[string]any)["shikii"].(map[string]any)
		if !reflect.DeepEqual(server["args"], []any{"mcp"}) || server["command"] != mcpCommand() {
			t.Fatalf("%s: shikii's server as written: %v", name, server)
		}
		if !reflect.DeepEqual(withoutShikiiServer(got), readTOML(t, []byte(text))) {
			t.Fatalf("%s: the person's settings stay as they were:\n%s", name, b)
		}
	}
	keep := map[string]string{
		"escaped name":      "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n",
		"dotted keys":       "mcp_servers.shikii.command = \"x\"\n",
		"inline table":      "mcp_servers = { mytool = { command = \"mytool\" } }\n",
		"not TOML":          "[mcp_servers\ncommand = \"x\"\n",
		"key under servers": "[mcp_servers]\n\"shikii\" = { command = \"x\" }\n",
	}
	for name, text := range keep {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || did || string(b) != text {
			t.Fatalf("%s: the file is left as it was: did=%v err=%v\n%s", name, did, err, b)
		}
		if note := codexServerNote(p); !strings.Contains(note, "[mcp_servers.shikii]") {
			t.Fatalf("%s: init shows the block to add by hand: %q", name, note)
		}
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(keep["not TOML"]), 0o644); err != nil {
		t.Fatal(err)
	}
	if note := codexServerNote(p); !strings.Contains(note, "could not be read as TOML") {
		t.Fatalf("a file Codex cannot read is named as such: %q", note)
	}
}

// A shared registration that pins one project is found however the args are quoted.
func TestCodexPinnedProjectFoundInAnyQuoting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	text := "[mcp_servers.shikii]\ncommand = 'shikii'\nargs = ['mcp', '--store', '/work/app/.shikii']\n"
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if !codexServerPinsAProject(p) {
		t.Fatal("args in literal strings still pin a project")
	}
}

// The oracle below is written here, apart from codex_toml.go, so a mistake there is not repeated by the test (go-toml is called directly).

// oracleParse reads b as Codex does: a leading byte order mark is skipped, the rest must be TOML.
func oracleParse(b []byte) (map[string]any, bool) {
	m := map[string]any{}
	return m, toml.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &m) == nil
}

func oracleEqual(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !oracleEqual(v, y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !oracleEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
	}
	return reflect.DeepEqual(a, b)
}

// oracleSplit is the settings without mcp_servers.shikii (and without an emptied mcp_servers), and the shikii server if any.
func oracleSplit(m map[string]any) (map[string]any, any, bool) {
	rest := map[string]any{}
	for k, v := range m {
		rest[k] = v
	}
	servers, ok := m["mcp_servers"].(map[string]any)
	if !ok {
		return rest, nil, false
	}
	shikii, has := servers["shikii"]
	others := map[string]any{}
	for k, v := range servers {
		if k != "shikii" {
			others[k] = v
		}
	}
	rest["mcp_servers"] = others
	if len(others) == 0 {
		delete(rest, "mcp_servers")
	}
	return rest, shikii, has
}

// oracleKeeps reports whether after is TOML holding exactly want as the shikii server (nil: none) and before's other settings unchanged.
func oracleKeeps(before, after []byte, want map[string]any) bool {
	b, ok1 := oracleParse(before)
	a, ok2 := oracleParse(after)
	if !ok1 || !ok2 {
		return false
	}
	restB, _, _ := oracleSplit(b)
	restA, shikii, has := oracleSplit(a)
	if want == nil && has || want != nil && (!has || !oracleEqual(shikii, map[string]any(want))) {
		return false
	}
	return oracleEqual(restA, restB)
}

func oracleWant(args []string) map[string]any {
	list := make([]any, len(args))
	for i, a := range args {
		list[i] = a
	}
	return map[string]any{"command": mcpCommand(), "args": list}
}

// A file Codex reads, shikii reads too (0.9.7 final audit A5): TOML 1.1 forms Codex 0.164.0 reads, checked with
// `CODEX_HOME=<temp folder> codex mcp list`. want nil: only that it is read.
func TestCodexReadableTOML11FormsAreRead(t *testing.T) {
	other := func(server map[string]any) map[string]any {
		return map[string]any{"mcp_servers": map[string]any{"other": server}}
	}
	for _, c := range []struct {
		name, text string
		want       map[string]any
	}{
		{"multi-line inline table with a trailing comma", "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  A = \"1\",\n}\n",
			other(map[string]any{"command": "x", "env": map[string]any{"A": "1"}})},
		{"one-line inline table with a trailing comma", "[mcp_servers.other]\ncommand = \"x\"\nenv = { A = \"1\", }\n",
			other(map[string]any{"command": "x", "env": map[string]any{"A": "1"}})},
		{"escape x", "[mcp_servers.other]\ncommand = \"x\\x41\"\n", other(map[string]any{"command": "xA"})},
		{"time without seconds", "[mcp_servers.other]\ncommand = \"x\"\nt = 07:32\n", nil},
		{"escape e", "[mcp_servers.other]\ncommand = \"x\\e\"\n", nil},
		{"offset date-time without seconds", "[mcp_servers.other]\ncommand = \"x\"\nt = 1979-05-27 07:32Z\n", nil},
		{"comment in a multi-line inline table", "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  # comment\n  A = \"1\"\n}\n", nil},
	} {
		m, err := readCodexConfig([]byte(c.text))
		if err != nil {
			t.Errorf("A5 (%s): Codex reads it but shikii does not: %v\n%s", c.name, err, c.text)
			continue
		}
		if c.want != nil && !reflect.DeepEqual(m, c.want) {
			t.Errorf("A5 (%s): the values read differ: %#v", c.name, m)
		}
	}
}
