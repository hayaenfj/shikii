package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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

// A2: removing shikii from a file Codex reads it from leaves no shikii and the rest unchanged; found is never false while there is one.
func TestFinalAEA2UninstallFromCodexReadableTOML11(t *testing.T) {
	rest := "[mcp_servers.other]\ncommand = \"x\"\nenv = {\n  A = \"1\",\n}\n"
	text := rest + "\n[mcp_servers.shikii]\ncommand = \"/usr/local/bin/shikii\"\nargs = [\"mcp\", \"--local\", \"http://127.0.0.1:8080\"]\n"

	out, found, err := withoutCodexServer(text)
	if !found {
		t.Errorf("A2: Codex reads shikii but found=false (err=%v)", err)
	}
	if err != nil || out != rest {
		t.Errorf("A2: only the shikii table is not removed: err=%v\ngot:\n%s", err, out)
	}

	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, err := removeCodexServer(p, true); err != nil || !found {
		t.Errorf("A2: uninstall cannot remove it: found=%v err=%v", found, err)
	}
	if b, _ := os.ReadFile(p); string(b) != rest {
		t.Errorf("A2: the file after uninstall differs:\n%s", b)
	}
}

// A4 on the uninstall side: broken settings (something after the first value) are not rewritten from the part that was read.
func TestFinalAEA4UninstallDoesNotRewriteBrokenJSON(t *testing.T) {
	text := "{\"mcpServers\": {\"shikii\": {\"command\": \"shikii\"}}}\n{\"other\": {\"command\": \"keep-me\"}}\n"
	p := filepath.Join(t.TempDir(), ".mcp.json")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := removeJSONEntry(p, "mcpServers", "shikii", true)
	if b, _ := os.ReadFile(p); string(b) != text {
		t.Errorf("A4: broken settings were rewritten (found=%v err=%v)\nafter:\n%s", found, err, b)
	}
}

// Codex 0.x reads a config.toml that starts with a UTF-8 BOM (checked with `codex mcp list` in a sandbox CODEX_HOME).
// codex_toml.go says "unreadable (not TOML; Codex cannot read it either)".
func TestAuditBOMConfigCodexReads(t *testing.T) {
	bom := "\xef\xbb\xbf"
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(bom+"model = \"m\"\n"), 0o600)
	did, err := addCodexServer(path, []string{"mcp"})
	if !did {
		t.Errorf("A1: a file Codex reads was not given shikii: did=%v err=%v state=%s", did, err, codexServerState([]byte(bom+"model = \"m\"\n"), "/x", []string{"mcp"}))
	}
	withShikii := bom + "model = \"m\"\n\n[mcp_servers.shikii]\ncommand = \"/x\"\nargs = [\"mcp\"]\n"
	_, found, err := withoutCodexServer(withShikii)
	if !found {
		t.Errorf("A2: uninstall leaves Codex's shikii server in a BOM file: found=%v err=%v", found, err)
	}
}

// R7 (A2): "found=false only when there is no shikii server". An inline table Codex reads as a shikii server is reported as not found.
func TestRe2FoundFalseOnlyWithoutShikii(t *testing.T) {
	for _, text := range []string{
		"mcp_servers = { shikii = { command = \"x\" } }\n",
		"[mcp_servers]\nshikii = { command = \"x\", args = [\n\"a\"] }\nother = 1\n",
	} {
		_, found, err := withoutCodexServer(text)
		m, _ := readCodexConfig([]byte(text))
		if _, has := codexShikiiServer(m); has && !found {
			t.Errorf("A2: Codex reads a shikii server, yet found=false (err=%v):\n%s", err, text)
		}
	}
}

// checkCodexConfigProperties checks A1 to A3 on one config.toml text with the oracle above and reports what happened.
func checkCodexConfigProperties(t *testing.T, path, text string) codexOutcome {
	t.Helper()
	var got codexOutcome
	before, parses := oracleParse([]byte(text))
	_, _, hadShikii := oracleSplit(before)
	// addable: appending shikii's table to the text keeps every other setting (checked here, not by codex_toml.go)
	appended := text
	if appended != "" && !strings.HasSuffix(appended, "\n") {
		appended += "\n"
	}
	appended += "\n[mcp_servers.shikii]\ncommand = " + quotedList([]string{mcpCommand()}) + "\nargs = [\"mcp\"]\n"
	addable := parses && !hadShikii && oracleKeeps([]byte(text), []byte(appended), oracleWant([]string{"mcp"}))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	// A1: add
	did, err := addCodexServer(path, []string{"mcp"})
	after, _ := os.ReadFile(path)
	switch {
	case err != nil:
		if string(after) != text {
			t.Fatalf("A1: an error changed the file:\n%q\n->\n%q", text, after)
		}
	case did:
		if !addable || !oracleKeeps([]byte(text), after, oracleWant([]string{"mcp"})) {
			t.Fatalf("A1: the added file is not the person's settings plus one shikii:\n%q\n->\n%q", text, after)
		}
	default:
		if string(after) != text {
			t.Fatalf("A1: not added, yet the file changed:\n%q\n->\n%q", text, after)
		}
		if addable {
			t.Fatalf("A1: addable but not added:\n%q", text)
		}
	}
	got.added = did && err == nil
	// A2: remove (from what is there now)
	current, _ := os.ReadFile(path)
	out, found, rerr := withoutCodexServer(string(current))
	parsed, currentParses := oracleParse(current)
	_, _, has := oracleSplit(parsed)
	switch {
	case rerr != nil:
		if out != string(current) {
			t.Fatalf("A2: a refusal changed the text:\n%q", current)
		}
		got.removal = "refused"
	case found:
		if !oracleKeeps(current, []byte(out), nil) {
			t.Fatalf("A2: the removal changed other settings or left shikii:\n%q\n->\n%q", current, out)
		}
		got.removal = "cut"
	default:
		if currentParses && has {
			t.Fatalf("A2: reported missing while Codex reads a shikii server:\n%q", current)
		}
		if out != string(current) {
			t.Fatalf("A2: not found, yet the text changed:\n%q", current)
		}
		got.removal = "none"
	}
	got.removed = out
	// A3: fix the form shikii wrote (command "shikii"), appended after the person's settings
	got.fix = "skipped"
	if !parses || hadShikii {
		return got
	}
	written := text
	if written != "" && !strings.HasSuffix(written, "\n") {
		written += "\n"
	}
	written += "\n[mcp_servers.shikii]\ncommand = \"shikii\"\nargs = [" + quotedList(codexServerArgs) + "]\n"
	if _, ok := oracleParse([]byte(written)); !ok {
		return got
	}
	if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	fixed, err := fixCodexServer(path, codexServerArgs)
	fixedText, _ := os.ReadFile(path)
	if err != nil || (fixed && !oracleKeeps([]byte(written), fixedText, oracleWant(codexServerArgs))) || (!fixed && string(fixedText) != written) {
		t.Fatalf("A3: fixing shikii's own form: fixed=%v err=%v\n%q\n->\n%q", fixed, err, written, fixedText)
	}
	got.fix = "left"
	if fixed {
		got.fix = "fixed"
	}
	return got
}

// TestCodexConfigShapes checks properties A1 to A3 of .shikii/handoff/audit-0.9.7/invariants.md (whatever a person wrote, adding, fixing
// or removing shikii leaves TOML Codex can read, with exactly the intended shikii server or none, and every other setting as it was; when
// that cannot be done, the file is not changed) on one fixed config.toml per way of writing it that an audit found to matter, and pins what
// shikii does with each (added or not, cut / none / refused, fixed / left / skipped). A text shikii added to and then removed from comes back
// as it was (only a missing last newline is added). Every case runs on every run, so a broken shape fails on the first run.
// It replaces 3000 random texts: those missed a byte order mark, a \u escape, multi-line values a cut must follow, dotted keys under
// [mcp_servers] and the blank line shikii adds, because a refusal always passed and the generator never wrote the first two.
func TestCodexConfigShapes(t *testing.T) {
	dir := t.TempDir()
	bom := "\xef\xbb\xbf"
	cases := []struct {
		name, text string
		want       codexOutcome // removed is not compared here; the round trip below covers it
	}{
		{"empty file", "", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"plain setting", "model = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"no newline at the end", "model = \"x\"", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"comment without newline at the end", "model = \"x\" # note", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"CRLF line ends", "model = \"x\"\r\n[profiles.p0]\r\nmodel = 'x'\r\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"byte order mark", bom + "model = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"nan value", "model = nan\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"nan in another server", "[mcp_servers.mytool]\ncommand = nan\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"comment naming shikii's table", "model = \"x\" # mcp_servers.shikii\n# [mcp_servers.shikii]\n", codexOutcome{added: true, removal: "cut", fix: "left"}},
		{"string naming shikii's table", "notes = \"mcp_servers.shikii # not a comment\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"multi-line string holding shikii's header", "notes = \"\"\"\n[mcp_servers.shikii]\n\"\"\"\n", codexOutcome{added: true, removal: "cut", fix: "left"}},
		{"array value", "notes = [\"a\", 'b']\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"not TOML", "[broken\n", codexOutcome{removal: "refused", fix: "skipped"}},
		{"another server as a table", "[mcp_servers.mytool]\ncommand = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"another server under [mcp_servers]", "[mcp_servers]\nmytool = { command = \"x\" }\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"another server as dotted keys", "mcp_servers.mytool.command = \"x\"\n", codexOutcome{added: true, removal: "cut", fix: "fixed"}},
		{"another server inline", "mcp_servers = { mytool = { command = \"x\" } }\n", codexOutcome{removal: "none", fix: "skipped"}},
		{"mcp_servers not a table", "mcp_servers = 1\n", codexOutcome{removal: "none", fix: "skipped"}},
		{"shikii as a table", "[mcp_servers.shikii]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with a basic quoted key", "[mcp_servers.\"shikii\"]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with literal quoted keys", "['mcp_servers'.'shikii']\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with a \\u escape", "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii as dotted keys", "mcp_servers.shikii.command = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii as dotted keys with a multi-line value", "mcp_servers.shikii.args = [\n  \"a\",\n]\nmodel = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii under [mcp_servers]", "[mcp_servers]\nshikii = { command = \"x\" }\nmytool = { command = \"y\" }\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii only by its sub-table", "[mcp_servers.shikii.env]\nX = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii with multi-line values", "[mcp_servers.shikii]\ncommand = \"\"\"\nline [mcp_servers.other]\n\"\"\"\nargs = [\n  \"a\",\n]\n[profiles.p0]\nmodel = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii in an inline mcp_servers", "mcp_servers = { shikii = { command = \"x\" } }\n", codexOutcome{removal: "refused", fix: "skipped"}},
		{"shikii after a byte order mark", bom + "[mcp_servers.shikii]\ncommand = \"x\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"shikii between other tables", "[profiles.p0]\nmodel = 1\n['mcp_servers'.\"shikii\"] # mcp_servers.shikii\ncommand = 'x'\n[mcp_servers.shikii.env]\nX = nan\n\r\n[mcp_servers.mytool]\ncommand = \"y\"\n", codexOutcome{removal: "cut", fix: "skipped"}},
		{"not TOML after a table", "[mcp_servers.mytool]\ncommand = \"x\"\n[broken\n", codexOutcome{removal: "refused", fix: "skipped"}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkCodexConfigProperties(t, filepath.Join(dir, fmt.Sprintf("config-%d.toml", i)), c.text)
			if got.added != c.want.added || got.removal != c.want.removal || got.fix != c.want.fix {
				t.Fatalf("added=%v removal=%s fix=%s, want added=%v removal=%s fix=%s\n%q", got.added, got.removal, got.fix, c.want.added, c.want.removal, c.want.fix, c.text)
			}
			restored := c.text
			if restored != "" && !strings.HasSuffix(restored, "\n") {
				restored += "\n"
			}
			if got.added && got.removed != restored {
				t.Fatalf("adding and then removing shikii must give the text back:\n%q\n->\n%q", c.text, got.removed)
			}
		})
	}
}

// Uninstall removes shikii however it is written, or refuses; it never reports "not there" while Codex still reads a shikii server,
// and what it leaves is the person's settings exactly.
func TestUninstallRemovesShikiiOnlyWhenATOMLReaderAgrees(t *testing.T) {
	for name, text := range map[string]string{
		"escaped name": "[mcp_servers.\"\\u0073hikii\"]\ncommand = \"x\"\n\n[other]\na = 1\n",
		"plain":        "model = \"m\"\n\n[mcp_servers.shikii]\ncommand = \"x\"\nargs = [\"mcp\"]\n\n[mcp_servers.mine]\ncommand = \"y\"\n",
		"inline":       "mcp_servers = { shikii = { command = \"x\" }, mine = { command = \"y\" } }\n",
	} {
		before := readTOML(t, []byte(text))
		out, found, err := withoutCodexServer(text)
		if err != nil {
			if out != text {
				t.Fatalf("%s: a refusal changes nothing", name)
			}
			continue
		}
		if !found {
			t.Fatalf("%s: Codex reads a shikii server here, so it must be removed or refused, not reported missing", name)
		}
		if !reflect.DeepEqual(readTOML(t, []byte(out)), withoutShikiiServer(before)) {
			t.Fatalf("%s: only shikii goes:\n%s", name, out)
		}
	}
}

// The confirmation audit of the audit's fixes (7d7646c, 02e3feb). The comments in JSON broke twice, so the hand-made reader was replaced
// by a JSONC reader (hujson), and removing lock files raced with a holder twice, so uninstall no longer removes them (an empty lock file is
// harmless and the next holder uses it).

// confirmation i-1: a JSONC opencode.json without shikii made uninstall fail with "shikii is still there".
func TestPatch099ConfirmUninstallJSONCWithoutShikiiHasNothingToRemove(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(p, []byte("{\n // mine\n \"mcp\": {\"other\": {\"type\": \"local\"}}\n}\n"), 0o644)
	if found, err := removeJSONEntry(p, "mcp", "shikii", false); err != nil || found {
		t.Fatalf("no shikii in a file with comments: nothing to remove: %v %v", found, err)
	}
}

// confirmation u-1: uninstall removed a lock file between a holder's open and its flock, and two holders then held two files. Lock files
// are not removed; uninstall still removes the page record.
func TestPatch099ConfirmUninstallLeavesLockFiles(t *testing.T) {
	b := newUninstallBox(t)
	locks := []string{filepath.Join(b.home, ".claude", "skills", ".shikii-skill.lock"), pageLockPath(b.agent, b.repo)}
	writeFileT(t, locks[1], "")
	capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	for _, l := range locks {
		if _, err := os.Stat(l); err != nil {
			t.Fatalf("a lock file stays (removing it races with a holder): %s %v", l, err)
		}
	}
	if _, err := os.Lstat(pagePath(b.agent, b.repo)); !os.IsNotExist(err) {
		t.Fatalf("the stopped page's record still goes: %v", err)
	}
}

// C SB-2: uninstall stopped the pages but left their records in ~/.shikii/agent/pages. They are shikii's, not data. The record of a page
// uninstall did not stop stays. (SB-1 and the page locks: lock files stay, see TestPatch099ConfirmUninstallLeavesLockFiles.)
func TestPatch099UninstallRemovesTheRecordsOfThePagesItStopped(t *testing.T) {
	b := newUninstallBox(t)
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if len(b.stopped) == 0 {
		t.Fatalf("precondition: the project's page is stopped:\n%s", out)
	}
	if _, err := os.Lstat(pagePath(b.agent, b.repo)); !os.IsNotExist(err) {
		t.Fatalf("the record of the stopped page is removed: %v\n%s", err, out)
	}
	if _, err := os.Stat(pagePath(b.agent, "/somewhere/else")); err != nil {
		t.Fatalf("control: the record of a page uninstall did not stop stays: %v", err)
	}
}

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
