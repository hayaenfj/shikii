package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// uninstallBox is a sandbox HOME where shikii was installed (hook install --yes and init in one project) next to a person's own settings.
type uninstallBox struct {
	home, agent, repo, exe, readout string
	stopped                         []int
	person                          map[string]string // path → the person's content before shikii was installed
}

func newUninstallBox(t *testing.T) *uninstallBox {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	b := &uninstallBox{home: home, agent: filepath.Join(home, ".shikii", "agent"), person: map[string]string{}}
	oldLocal, oldPage, oldStop, oldCmd, oldExe := hookLocalURL, hookPageURL, stopProcess, processCommand, executablePath
	hookLocalURL, hookPageURL = "http://127.0.0.1:1", "http://127.0.0.1:1"
	stopProcess = func(pid int) error { b.stopped = append(b.stopped, pid); return nil } // never a real signal
	processCommand = func(pid int) string { return "" }
	bin := filepath.Join(t.TempDir(), "my bin")
	b.exe, b.readout = filepath.Join(bin, "shikii"), filepath.Join(bin, "shikii-readout")
	executablePath = func() (string, error) { return b.exe, nil }
	t.Cleanup(func() {
		hookLocalURL, hookPageURL, stopProcess, processCommand, executablePath = oldLocal, oldPage, oldStop, oldCmd, oldExe
	})
	for _, p := range []string{b.exe, b.readout} {
		writeFileT(t, p, "binary")
	}
	b.repo = gitRepo(t)
	b.person[filepath.Join(home, ".claude", "settings.json")] = `{"language":"Japanese","hooks":{"Stop":[{"hooks":[{"type":"command","command":"node other.mjs"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`
	b.person[filepath.Join(home, ".codex", "hooks.json")] = `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"my-codex-hook"}]}]}}`
	b.person[filepath.Join(home, ".codex", "config.toml")] = "model = \"gpt-5\"\n\n[projects.\"/x\"]\ntrust_level = \"trusted\"\n\n[tools]\nlist = [\"a\",\n  \"b\"]\n" // init appends shikii's table only when no mcp_servers is there
	b.person[filepath.Join(home, ".claude", "skills", "mine", "SKILL.md")] = "---\nname: mine\n---\n"
	b.person[filepath.Join(b.repo, ".mcp.json")] = `{"mcpServers":{"other":{"command":"x","args":["1"]}}}`
	b.person[filepath.Join(b.repo, "opencode.json")] = `{"$schema":"https://opencode.ai/config.json","mcp":{"other":{"type":"local","command":["y"]}}}`
	b.person[filepath.Join(b.repo, "AGENTS.md")] = "# My rules\n\nKeep it short.\n"
	b.person[filepath.Join(b.repo, "CLAUDE.md")] = "@AGENTS.md\n"
	for p, body := range b.person {
		writeFileT(t, p, body)
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	initTestProject(t, b.repo)
	// the project's page is up (its serve), and a stale mark of another project now points at a pid that runs something else
	if err := writePageRecord(b.agent, b.repo, 40001, os.Getppid()); err != nil {
		t.Fatal(err)
	}
	if err := writePageRecord(b.agent, "/somewhere/else", 40002, os.Getppid()); err != nil {
		t.Fatal(err)
	}
	processCommand = func(pid int) string {
		if pid == os.Getppid() {
			return b.exe + " serve --store " + filepath.Join(b.repo, ".shikii") + " --port 0 --local http://127.0.0.1:8080"
		}
		return ""
	}
	// what install put in place, so the test starts from a real install
	for _, p := range []string{filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md"), filepath.Join(home, ".agents", "skills", "shikii", "SKILL.md")} {
		if !fileExists(p) {
			t.Fatalf("install must place the skill: %s", p)
		}
	}
	for _, p := range []string{filepath.Join(home, ".codex", "config.toml"), filepath.Join(b.repo, ".mcp.json"), filepath.Join(b.repo, "opencode.json"), filepath.Join(home, ".claude", "settings.json")} {
		if body, _ := os.ReadFile(p); !strings.Contains(string(body), "shikii") {
			t.Fatalf("install and init must write shikii into %s:\n%s", p, body)
		}
	}
	return b
}

func writeFileT(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshotTree is every file, folder and link under the given folders with its mode and content (bytes), to compare before and after.
func snapshotTree(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := os.Lstat(p)
			if err != nil {
				return nil
			}
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, _ := os.Readlink(p)
				out[p] = "link " + target
			case info.IsDir():
				out[p] = "dir " + info.Mode().String()
			default:
				body, _ := os.ReadFile(p)
				out[p] = info.Mode().String() + " " + string(body)
			}
			return nil
		})
	}
	return out
}

func diffTrees(a, b map[string]string) []string {
	var d []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			d = append(d, "gone: "+k)
		} else if v != w {
			d = append(d, "changed: "+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			d = append(d, "new: "+k)
		}
	}
	sort.Strings(d)
	return d
}

func decodeJSONFile(t *testing.T, p string) any {
	t.Helper()
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("%s: %v\n%s", p, err, body)
	}
	return v
}

func decodeJSONText(t *testing.T, s string) any {
	t.Helper()
	var v any
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Without --yes, uninstall lists what it would remove and changes nothing (not a byte, no process is stopped).
func TestUninstallWithoutYesListsAndChangesNothing(t *testing.T) {
	b := newUninstallBox(t)
	before := snapshotTree(t, b.home, b.repo)
	out := capture(t, func() error { return cmdUninstall(nil) })
	if d := diffTrees(before, snapshotTree(t, b.home, b.repo)); len(d) > 0 {
		t.Fatalf("without --yes nothing may change: %v", d)
	}
	if len(b.stopped) > 0 {
		t.Fatalf("without --yes no process may be stopped: %v", b.stopped)
	}
	for _, must := range []string{
		filepath.Join("~", ".claude", "settings.json"), filepath.Join("~", ".codex", "hooks.json"), filepath.Join("~", ".codex", "config.toml"),
		filepath.Join(b.repo, ".mcp.json"), filepath.Join(b.repo, "opencode.json"),
		filepath.Join("~", ".claude", "skills", "shikii"), filepath.Join("~", ".agents", "skills", "shikii"),
		"stop the page", "shown only; pass --yes",
	} {
		if !strings.Contains(out, must) {
			t.Fatalf("the list must name %q:\n%s", must, out)
		}
	}
}

// --yes removes exactly shikii's parts: a person's hooks, MCP servers, Codex lines, project files and every .shikii/ byte stay. A second run changes nothing.
func TestUninstallRemovesOnlyShikiisPartsAndIsIdempotent(t *testing.T) {
	b := newUninstallBox(t)
	stores := []string{filepath.Join(b.repo, ".shikii"), b.agent}
	keep := snapshotTree(t, stores...)
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })

	for _, p := range []string{filepath.Join(b.home, ".claude", "settings.json"), filepath.Join(b.home, ".codex", "hooks.json"), filepath.Join(b.repo, ".mcp.json"), filepath.Join(b.repo, "opencode.json")} {
		if got, want := decodeJSONFile(t, p), decodeJSONText(t, b.person[p]); !reflect.DeepEqual(got, want) {
			body, _ := os.ReadFile(p)
			t.Fatalf("%s must hold only the person's settings again:\n%s\n%s", p, body, out)
		}
	}
	for _, p := range []string{filepath.Join(b.home, ".codex", "config.toml"), filepath.Join(b.repo, "AGENTS.md"), filepath.Join(b.repo, "CLAUDE.md"), filepath.Join(b.home, ".claude", "skills", "mine", "SKILL.md")} {
		if body, _ := os.ReadFile(p); string(body) != b.person[p] {
			t.Fatalf("%s must be byte for byte what the person had:\n%q\nwant\n%q\n%s", p, body, b.person[p], out)
		}
	}
	for _, p := range []string{filepath.Join(b.home, ".claude", "skills", "shikii"), filepath.Join(b.home, ".agents", "skills", "shikii")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("the skill folder must be gone: %s (%v)", p, err)
		}
	}
	// the record of the page uninstall stopped is shikii's, not data, and goes with it (0.9.9); everything else stays byte for byte
	stoppedRecord := "gone: " + pagePath(b.agent, b.repo)
	var d []string
	for _, x := range diffTrees(keep, snapshotTree(t, stores...)) {
		if x != stoppedRecord {
			d = append(d, x)
		}
	}
	if len(d) > 0 {
		t.Fatalf("the data (.shikii/ and ~/.shikii/agent) must stay byte for byte: %v", d)
	}
	if !reflect.DeepEqual(b.stopped, []int{os.Getppid()}) {
		t.Fatalf("only the project's own page is stopped (the mark whose pid runs something else is not): %v", b.stopped)
	}
	if !fileExists(b.exe) || !fileExists(b.readout) {
		t.Fatal("the binaries are never removed")
	}

	// a second run: nothing left to remove, nothing changes
	b.stopped = nil
	processCommand = func(int) string { return "" } // the page is down now
	again := snapshotTree(t, b.home, b.repo)
	out2 := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if d := diffTrees(again, snapshotTree(t, b.home, b.repo)); len(d) > 0 || len(b.stopped) > 0 {
		t.Fatalf("a second run must change nothing: %v %v\n%s", d, b.stopped, out2)
	}
	if !strings.Contains(out2, "nothing of shikii's is left to remove") {
		t.Fatalf("a second run must say there is nothing to remove:\n%s", out2)
	}
}

// The end of the run says what stays: each project's .shikii/ and ~/.shikii/agent (one more install continues from them), and the exact rm command for the binaries.
func TestUninstallFinalMessageNamesDataAndBinaries(t *testing.T) {
	b := newUninstallBox(t)
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	for _, must := range []string{
		"rm '" + b.exe + "' '" + b.readout + "'",
		filepath.Join(b.repo, ".shikii") + string(filepath.Separator),
		b.agent,
		"shikii hook install --yes",
		"reopen",
	} {
		if !strings.Contains(out, must) {
			t.Fatalf("the final message must hold %q:\n%s", must, out)
		}
	}
	if q := shellQuote("/usr/local/bin/shikii"); q != "/usr/local/bin/shikii" {
		t.Fatalf("a plain path is printed as it is: %s", q)
	}
}

// A skill folder that is a link to somewhere else, or a folder that is not shikii's skill, is left alone and named.
func TestUninstallRefusesLinkedOrForeignSkillFolders(t *testing.T) {
	b := newUninstallBox(t)
	claude := filepath.Join(b.home, ".claude", "skills", "shikii")
	agents := filepath.Join(b.home, ".agents", "skills", "shikii")
	elsewhere := filepath.Join(t.TempDir(), "dotfiles-skill")
	writeFileT(t, filepath.Join(elsewhere, "SKILL.md"), "---\nname: shikii\n---\nmine\n")
	if err := os.RemoveAll(claude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, claude); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(agents); err != nil {
		t.Fatal(err)
	}
	writeFileT(t, filepath.Join(agents, "SKILL.md"), "---\nname: someone-else\n---\n")
	before := snapshotTree(t, claude, elsewhere, agents)
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if d := diffTrees(before, snapshotTree(t, claude, elsewhere, agents)); len(d) > 0 {
		t.Fatalf("a linked or foreign skill folder must stay as it is: %v\n%s", d, out)
	}
	if info, err := os.Lstat(claude); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link must stay a link: %v", err)
	}
	for _, must := range []string{"left alone: " + filepath.Join("~", ".claude", "skills", "shikii") + " is a link", "left alone: " + filepath.Join("~", ".agents", "skills", "shikii") + " is not shikii's skill"} {
		if !strings.Contains(out, must) {
			t.Fatalf("the refusal must be named (%q):\n%s", must, out)
		}
	}
}

// [mcp_servers.shikii] is removed in any form shikii or a person wrote, with its sub-tables and multi-line values; every other line stays.
func TestRemoveCodexServerTakesOnlyShikiisTable(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"appended by shikii at the end", "model = \"x\"\n\n[mcp_servers.shikii]\ncommand = \"/b/shikii\"\nargs = [\"mcp\"]\n", "model = \"x\"\n"},
		{"in the middle, multi-line args, a sub-table and a person's comment for the next table",
			"[a]\nk = 1\n\n[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [\n  \"mcp\",\n  \"--store\", \".shikii\",\n]\n[mcp_servers.shikii.env]\nK = \"v\"\n\n# my server\n[mcp_servers.other]\ncommand = \"o\"\n",
			"[a]\nk = 1\n\n# my server\n[mcp_servers.other]\ncommand = \"o\"\n"},
		{"quoted key and spaces in the header", "[ mcp_servers . \"shikii\" ]  # mine\ncommand = \"s\"\n\n[b]\nx = 2\n", "\n[b]\nx = 2\n"},
		{"a key under [mcp_servers]", "[mcp_servers]\nshikii = { command = \"s\", args = [\"mcp\"] }\nother = { command = \"o\" }\n", "[mcp_servers]\nother = { command = \"o\" }\n"},
		{"dotted keys at the top", "mcp_servers.shikii.command = \"s\"\nmcp_servers.shikii.args = [\"mcp\"]\nmodel = \"m\"\n", "model = \"m\"\n"},
		{"an array line that starts with [ in another table is not a header", "[c]\nm = [\n[1, 2],\n]\n[mcp_servers.shikii]\ncommand = \"s\"\n", "[c]\nm = [\n[1, 2],\n]\n"},
		{"no newline at the end", "a = 1\n\n[mcp_servers.shikii]\ncommand = \"s\"", "a = 1\n"},
		{"a multi-line value in shikii's table whose line looks like a header", "[mcp_servers.shikii]\nenv_pairs = [\n[\"K\", \"V\"],\n]\nafter = 1\n[d]\ny = 3\n", "[d]\ny = 3\n"},
		{"a header inside another table's multi-line string is text, not a table", "[e]\nnote = \"\"\"\n[mcp_servers.shikii]\ncommand = 1\n\"\"\"\n", "[e]\nnote = \"\"\"\n[mcp_servers.shikii]\ncommand = 1\n\"\"\"\n"},
		{"a person's other server named like shikii stays", "[mcp_servers.shikii-dev]\ncommand = \"d\"\n", "[mcp_servers.shikii-dev]\ncommand = \"d\"\n"},
	}
	for _, c := range cases {
		got, found, err := withoutCodexServer(c.in)
		if err != nil || got != c.want || found != (c.in != c.want) {
			t.Fatalf("%s: found=%v err=%v\n%q\nwant\n%q", c.name, found, err, got, c.want)
		}
	}
	if _, _, err := withoutCodexServer("mcp_servers = { shikii = { command = \"s\" } }\n"); err == nil {
		t.Fatal("an inline mcp_servers table holding shikii cannot be cut line by line: it must be refused, not half removed")
	}
}

// A page is stopped only when its pid runs shikii serve for that very project: a pid reused by another program or another project's serve is not.
func TestIsShikiiServeMatchesOnlyThatProjectsServe(t *testing.T) {
	root := "/work/my proj/.shikii"
	for cmd, want := range map[string]bool{
		"/opt/bin/shikii serve --store /work/my proj/.shikii --port 0 --local http://127.0.0.1:8080": true,
		"shikii serve --store /work/my proj/.shikii":                                                 true,
		"/usr/bin/vim serve --store /work/my proj/.shikii":                                           false,
		"/opt/bin/shikii serve --store /work/my proj/.shikii2 --port 0":                              false,
		"/opt/bin/shikii-readout serve --store /work/my proj/.shikii":                                false,
		"": false,
	} {
		if got := isShikiiServe(cmd, root); got != want {
			t.Fatalf("%q: %v, want %v", cmd, got, want)
		}
	}
}

// config.toml kept elsewhere through a link: the file it points at is written, the link stays a link, and the permissions stay. A read-only file is left alone.
func TestRemoveCodexServerWritesThroughLinksAndKeepsPermissions(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "config.toml")
	writeFileT(t, real, "a = 1\n\n[mcp_servers.shikii]\ncommand = \"s\"\n")
	if err := os.Chmod(real, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if found, err := removeCodexServer(link, true); err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link must stay a link")
	}
	body, _ := os.ReadFile(real)
	if st, _ := os.Stat(real); string(body) != "a = 1\n" || st.Mode().Perm() != 0o600 {
		t.Fatalf("the linked file must lose only shikii's table and keep 0600: %q %v", body, st.Mode())
	}
	ro := filepath.Join(dir, "ro.toml")
	writeFileT(t, ro, "[mcp_servers.shikii]\ncommand = \"s\"\n")
	if err := os.Chmod(ro, 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := removeCodexServer(ro, false); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("a read-only config.toml must be refused: %v", err)
	}
	if body, _ := os.ReadFile(ro); string(body) != "[mcp_servers.shikii]\ncommand = \"s\"\n" {
		t.Fatal("a read-only config.toml must stay as it is")
	}
}

// Tests never send a real signal: stopProcess is a recorder during tests (like hookSpawn).
func TestUninstallNeverSignalsRealProcessesInTests(t *testing.T) {
	if reflect.ValueOf(stopProcess).Pointer() == reflect.ValueOf(signalTerm).Pointer() {
		t.Fatal("during tests, stopProcess must be a recorder, not the real signal")
	}
	if reflect.ValueOf(processCommand).Pointer() == reflect.ValueOf(psCommand).Pointer() {
		t.Fatal("during tests, processCommand must not run the real ps")
	}
}

func TestUsageListsUninstall(t *testing.T) {
	if !strings.Contains(usageText(), "shikii uninstall [--yes]") {
		t.Fatal("the usage must list shikii uninstall")
	}
}
