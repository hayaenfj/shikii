package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.9: the small items the 0.9.7 audits left (.shikii/handoff/plan-0.9.9/triage.md, C). Each test is checked to fail before its fix.

// C confirm1-5: a key written twice in a person's JSON settings. Go keeps the last one, so a rewrite dropped the person's first text.
// Claude Code reads the last one too, so the file is valid for its agent: shikii leaves it as written, and init goes on with the entry to
// add by hand, as for a read-only file.
func TestPatch099DuplicateKeysAreLeftAsWritten(t *testing.T) {
	for name, text := range map[string]string{
		"top level": `{"mcpServers": {"a": {"command": "x"}}, "mcpServers": {"b": {"command": "y"}}}` + "\n",
		"nested":    `{"mcpServers": {"a": {"command": "x"}, "a": {"command": "y"}}}` + "\n",
	} {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"})
		if b, _ := os.ReadFile(p); string(b) != text {
			t.Fatalf("%s: a file with a key written twice is left as written, got %q", name, b)
		}
		if !errors.As(err, new(*personFileError)) || !strings.Contains(err.Error(), "twice") {
			t.Fatalf("%s: init is told to add the entry by hand and goes on: %v", name, err)
		}
		withShikii := strings.Replace(text, `"command": "y"`, `"command": "y"}, "shikii": {"command": "shikii"`, 1)
		if err := os.WriteFile(p, []byte(withShikii), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := removeJSONEntry(p, "mcpServers", "shikii", true); err == nil {
			t.Fatalf("%s: uninstall says it left the file", name)
		}
		if b, _ := os.ReadFile(p); string(b) != withShikii {
			t.Fatalf("%s: uninstall leaves it as written, got %q", name, b)
		}
	}
	// control: the same key in two different objects is not a key written twice
	p := filepath.Join(t.TempDir(), ".mcp.json")
	os.WriteFile(p, []byte(`{"mcpServers": {"a": {"command": "x"}, "b": {"command": "x"}}}`), 0o644)
	if did, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"}); err != nil || !did {
		t.Fatalf("control: the same key in two objects is fine: %v %v", did, err)
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

// C confirm1-2: records/store-id was made with O_EXCL and then written; a stop in between left an empty file that was never made again
// (read gave "" and O_EXCL failed), so the store kept its path-named entry and a later move was not reconnected.
func TestPatch099EmptyStoreIDIsMadeAgain(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	if err := os.MkdirAll(filepath.Join(root, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "records", "store-id"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	id := storeID(root, true)
	if b, _ := os.ReadFile(filepath.Join(root, "records", "store-id")); id == "" || strings.TrimSpace(string(b)) != id {
		t.Fatalf("an empty store-id is made again: id=%q, file=%q", id, b)
	}
	if again := storeID(root, true); again != id {
		t.Fatalf("and then kept: %q, then %q", id, again)
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

// C AE-2: a read-only ~/.codex/config.toml whose shikii server still points at a binary that is gone: fixCodexServer's error stopped init,
// while every other read-only settings file is only told. init now says what to change by hand and goes on.
func TestPatch099ReadOnlyCodexConfigLetsInitGoOn(t *testing.T) {
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
	if err := os.Chmod(path, 0o444); err != nil { // WriteFile keeps the mode of a file that is already there
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	var err error
	out := capture(t, func() error { err = cmdInit(nil); return nil })
	if err != nil {
		t.Fatalf("init goes on with a read-only config.toml: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(path); string(b) != text {
		t.Fatalf("the read-only file is left as it is: %q", b)
	}
	if !strings.Contains(out, "config.toml") || !strings.Contains(out, "command = "+quotedList([]string{mcpCommand()})) || !strings.Contains(out, "Ready:") {
		t.Fatalf("init says what to change by hand:\n%s", out)
	}
}

// C AE-2, on a disk mounted read-only (EROFS): the error was not the read-only one, so fixCodexServer returned it and init stopped.
func TestPatch099CodexConfigOnAReadOnlyDiskLetsInitGoOn(t *testing.T) {
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
	openPersonFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if strings.HasSuffix(name, "config.toml") {
			return nil, &os.PathError{Op: "open", Path: name, Err: syscall.EROFS}
		}
		return os.OpenFile(name, flag, perm)
	}
	t.Cleanup(func() { openPersonFile = os.OpenFile })
	var err error
	out := capture(t, func() error { err = cmdInit(nil); return nil })
	if err != nil {
		t.Fatalf("init goes on when config.toml is on a read-only disk: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(path); string(b) != text {
		t.Fatalf("the file is left as it is: %q", b)
	}
	if !strings.Contains(out, "command = "+quotedList([]string{mcpCommand()})) || !strings.Contains(out, "Ready:") {
		t.Fatalf("init says what to change by hand:\n%s", out)
	}
}

// C SB-1, SB-2: uninstall removed the skill folders but left their lock files (~/.claude/skills/.shikii-skill.lock and the same under
// ~/.agents/skills), and stopped the pages but left their records and locks in ~/.shikii/agent/pages. They are shikii's, not data.
// A lock another page still holds, and the record of a page uninstall did not stop, stay.
func TestPatch099UninstallRemovesTheLocksAndRecordsOfWhatItRemoved(t *testing.T) {
	b := newUninstallBox(t)
	locks := []string{filepath.Join(b.home, ".claude", "skills", ".shikii-skill.lock"), filepath.Join(b.home, ".agents", "skills", ".shikii-skill.lock")}
	for _, l := range locks {
		if _, err := os.Stat(l); err != nil {
			t.Fatalf("precondition: hook install leaves the skill lock %s: %v", l, err)
		}
	}
	pageLock := pageLockPath(b.agent, b.repo)
	writeFileT(t, pageLock, "")
	takePageLock(t, b.agent, "/held/elsewhere") // a page that still runs holds its lock
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if len(b.stopped) == 0 {
		t.Fatalf("precondition: the project's page is stopped:\n%s", out)
	}
	for _, l := range locks {
		if _, err := os.Lstat(l); !os.IsNotExist(err) {
			t.Fatalf("the skill lock is removed with the skill folder: %s (%v)\n%s", l, err, out)
		}
	}
	for _, f := range []string{pagePath(b.agent, b.repo), pageLock} {
		if _, err := os.Lstat(f); !os.IsNotExist(err) {
			t.Fatalf("the record and lock of the stopped page are removed: %s (%v)\n%s", f, err, out)
		}
	}
	if _, err := os.Stat(pageLockPath(b.agent, "/held/elsewhere")); err != nil {
		t.Fatalf("control: a lock a running page holds stays: %v", err)
	}
	if _, err := os.Stat(pagePath(b.agent, "/somewhere/else")); err != nil {
		t.Fatalf("control: the record of a page uninstall did not stop stays: %v", err)
	}
}

// C BC-1: proj is moved to proj-old, another project is moved to proj and reconnected first, then a request comes at proj-old. The relink
// forgot proj and stopped the page recorded under proj, which now serves the other project. A path where a .shikii is again is someone's
// project: its page is left alone.
func TestPatch099ForgettingAPathDoesNotStopTheProjectNowThere(t *testing.T) {
	b := newUninstallBox(t)
	old := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(old, ".shikii"), 0o700); err != nil { // the other project now at the old path
		t.Fatal(err)
	}
	if err := writePageRecord(b.agent, old, 40003, os.Getppid()); err != nil {
		t.Fatal(err)
	}
	processCommand = func(pid int) string {
		return "/bin/shikii serve --store " + filepath.Join(old, ".shikii") + " --port 0"
	}
	forgetProject(old)
	if len(b.stopped) != 0 {
		t.Fatalf("the page of the project now at the old path is not stopped: %v", b.stopped)
	}
	if _, ok := readPageRecord(b.agent, old); !ok {
		t.Fatal("and its record stays")
	}
	// control: after a real move nothing is at the old path, and the old page is stopped
	if err := os.RemoveAll(filepath.Join(old, ".shikii")); err != nil {
		t.Fatal(err)
	}
	forgetProject(old)
	if len(b.stopped) != 1 {
		t.Fatalf("control: the moved project's old page is stopped: %v", b.stopped)
	}
}

// B DF-1, DF-2 (expected fixed by 37424df, checked here): the hook and serve spelled the project path differently when it was reached
// through a link, so the hook did not find the page (and restarted serve every 30 seconds) and uninstall did not stop it. Both now key by the
// same spelling: the hook's scope, the scope serve keys its page by, and the store serve's command line names are one string.
func TestPatch099HookAndServeSpellThePageTheSameWay(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	real := gitRepo(t)
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	if err := cmdInit(nil); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{link, real, filepath.Join(link, ".")} {
		hook := projectScopeAt(cwd)
		root := filepath.Join(hook, ".shikii") // what ensureProjectPage passes as serve --store
		stores := []string{root, filepath.Join(link, ".shikii"), filepath.Join(real, ".shikii")}
		if upper := strings.ToUpper(real); upper != real {
			if a, err := os.Stat(real); err == nil {
				if b, err := os.Stat(upper); err == nil && os.SameFile(a, b) { // a case-insensitive disk
					stores = append(stores, filepath.Join(upper, ".shikii"))
				}
			}
		}
		for _, store := range stores {
			if got := pageScope(store); got != hook {
				t.Fatalf("DF-1: serve --store %s keys its page by %q, the hook (cwd %s) by %q", store, got, cwd, hook)
			}
		}
		if !isShikiiServe("/bin/shikii serve --store "+root+" --port 0 --local x", filepath.Join(pageScope(root), ".shikii")) {
			t.Fatalf("DF-2: uninstall finds the page the hook started from %s", cwd)
		}
	}
}

// Found while doing 0.9.9 (2026-10-27): the hook adopted an agent-checked wording with only the local model's URL, so with a TypeSafe key
// and no local model the calibration's replay failed ("dial tcp 127.0.0.1:8080: connection refused") and a wording measured on Jev was
// never adopted. The adoption uses the same endpoints the hook decides with.
func TestPatch099WordingAdoptionUsesTheHooksEndpoints(t *testing.T) {
	_, _, res := wordingCheckFixture(t)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("SHIKII_MODEL", "")
	var got endpointFlags
	adopt := wordingAdopt
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		got = ef
		return adopt(r, ef, storeDir, labeled, reports...)
	}
	if out, err := submitWordingCheck(t, checkBody(res, "same")); err != nil || out["adopted"] != true {
		t.Fatalf("precondition: adopted: %v %v", out, err)
	}
	if got.hosted != hookHostedURL || got.local != hookLocalURL {
		t.Fatalf("the adoption calibrates with the hook's endpoints (hosted %q, local %q): %+v", hookHostedURL, hookLocalURL, got)
	}
}
