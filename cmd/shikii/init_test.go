package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
	"gopkg.in/yaml.v3"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, b)
	}
	return m
}

// shikii init prepares the agent in one go (no options): the base layout folders, the MCP settings (Claude Code / OpenCode / Codex; Cursor is not supported from 0.9.2),
// the AGENTS.md section, and puts the skill back at ~/.claude/skills/shikii. What a person wrote is left alone, and running it again is safe.
func TestInitPreparesTheProjectAndTheAgentInOneGo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"top":{"n":12345678901234567890},"mcpServers":{"other":{"command":"x","env":{"T":"a&b<c>"}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "shikii", "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "skills", "shikii", "references", "OLD.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"o3\""), 0o644); err != nil { // no trailing newline
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	for _, d := range []string{filepath.Join(".shikii", "decisions"), filepath.Join(".shikii", "wiki"), filepath.Join(".shikii", "records"), filepath.Join(".shikii", "handoff")} {
		if fi, err := os.Stat(filepath.Join(dir, d)); err != nil || !fi.IsDir() {
			t.Fatalf("%s must exist", d)
		}
	}
	// Claude Code: other entries, top-level keys, numbers and & < > stay as they are
	b, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	m := readJSON(t, filepath.Join(dir, ".mcp.json"))
	servers := m["mcpServers"].(map[string]any)
	if servers["other"] == nil || servers["shikii"] == nil || servers["shikii"].(map[string]any)["command"] != "shikii" {
		t.Fatalf("the shikii server must be added without dropping others: %s", b)
	}
	if !strings.Contains(string(b), `"a&b<c>"`) || !strings.Contains(string(b), "12345678901234567890") || !strings.Contains(string(b), `"top"`) {
		t.Fatalf("other keys, numbers and & < > must survive untouched: %s", b)
	}
	// Cursor is not supported (0.9.2): init does not write .cursor/mcp.json
	if _, err := os.Stat(filepath.Join(dir, ".cursor")); !os.IsNotExist(err) {
		t.Fatalf("init does not write Cursor's settings: %v", err)
	}
	// OpenCode
	oc := readJSON(t, filepath.Join(dir, "opencode.json"))
	if e, _ := oc["mcp"].(map[string]any)["shikii"].(map[string]any); e == nil || e["type"] != "local" {
		t.Fatalf("OpenCode reads opencode.json mcp.shikii with type local: %+v", oc)
	}
	// Codex: existing lines stay and [mcp_servers.shikii] is appended
	cb, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.HasPrefix(string(cb), "model = \"o3\"\n\n[mcp_servers.shikii]") || !strings.Contains(string(cb), `command = "/`) {
		t.Fatalf("Codex config must keep its lines and gain the shikii server:\n%s", cb)
	}
	// Codex's registration is shared by every project, so it names no store (each tool call passes project; 0.9.3)
	if !strings.HasSuffix(string(cb), "\n"+codexServerArgsLine+"\n") || strings.Contains(string(cb), "--store") {
		t.Fatalf("Codex's shared registration must be %s with no store:\n%s", codexServerArgsLine, cb)
	}
	if args, _ := servers["shikii"].(map[string]any)["args"].([]any); len(args) != len(codexMCPArgs) || args[1] != "--store" {
		t.Fatalf("Claude Code's per-project .mcp.json keeps its store: %v", args)
	}
	// AGENTS.md: the person's file, shikii adds nothing (0.9.2)
	ab, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(ab), "# My project") || strings.Contains(string(ab), "## shikii") || strings.Contains(string(ab), agentsMarker) {
		t.Fatalf("AGENTS.md must keep its content and gain nothing:\n%s", ab)
	}
	// skill: put back (old files are deleted)
	sb, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "shikii", "SKILL.md"))
	if err != nil || !strings.Contains(string(sb), "name: shikii") {
		t.Fatalf("the skill must be installed for the agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "references", "bands.md")); err != nil {
		t.Fatalf("references must come along: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "shikii", "references", "OLD.md")); !os.IsNotExist(err) {
		t.Fatalf("stale files in the skill folder must be removed (the skill is replaced, not merged)")
	}
	for _, must := range []string{".mcp.json", "opencode.json", "Codex", "skills/shikii", ".agents/skills/shikii", "shikii up"} {
		if !strings.Contains(out, must) {
			t.Fatalf("init must say what it did and what comes next; missing %q:\n%s", must, out)
		}
	}
	// Second run: nothing is duplicated and the --proposer a person added stays
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"other":{"command":"x"},"shikii":{"command":"shikii","args":["mcp","--proposer","http://127.0.0.1:8083"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdInit(nil) })
	b2, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(b2), "--proposer") || !strings.Contains(out, "kept") {
		t.Fatalf("an existing shikii entry must be kept as the person wrote it: %s\n%s", b2, out)
	}
	cb2, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	ab2, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if strings.Count(string(cb2), "[mcp_servers.shikii]") != 1 || string(ab2) != string(ab) {
		t.Fatalf("init must be idempotent for Codex and AGENTS.md")
	}
	// Broken JSON is not overwritten
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdInit(nil); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("a broken .mcp.json must stop init: %v", err)
	}
}

// Audit 6: close the paths by which init breaks a person's settings.
//  1. If Codex's config.toml has mcp_servers written in any form, leave it alone (appending would define it twice in TOML and Codex could not read its settings)
//  2. An unreadable .mcp.json is not replaced
//  6. Do not create AGENTS.md in a project with only CLAUDE.md (OpenCode would stop reading CLAUDE.md)
//  9. Even without HOME, the project side is done and it ends in success
func TestInitNeverBreaksWhatPeopleWrote(t *testing.T) {
	for name, toml := range map[string]string{
		"inline":  "mcp_servers = { shikii = { command = \"x\" } }\n",
		"table":   "[mcp_servers]\nshikii = { command = \"x\" }\n",
		"quoted":  "[mcp_servers.\"shikii\"]\ncommand = \"x\"\n",
		"heading": "[mcp_servers.shikii]\ncommand = \"x\"\n",
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
		did, err := addCodexServer(p, []string{"mcp"})
		b, _ := os.ReadFile(p)
		if err != nil || did || string(b) != toml {
			t.Fatalf("%s: a config that already has mcp_servers must be left alone: did=%v err=%v\n%s", name, did, err, b)
		}
	}
	if os.Geteuid() != 0 {
		p := filepath.Join(t.TempDir(), ".mcp.json")
		if err := os.WriteFile(p, []byte(`{"mcpServers":{"mine":{"command":"x"}}}`), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := addJSONEntry(p, "mcpServers", "shikii", map[string]any{"command": "shikii"}); err == nil {
			t.Fatalf("an unreadable config must stop init, not be replaced")
		}
		_ = os.Chmod(p, 0o644)
		if b, _ := os.ReadFile(p); !strings.Contains(string(b), "mine") {
			t.Fatalf("the unreadable file must keep its content: %s", b)
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", "")
	if err := os.WriteFile("CLAUDE.md", []byte("# rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	if _, err := os.Stat("AGENTS.md"); !os.IsNotExist(err) {
		t.Fatalf("with only CLAUDE.md, AGENTS.md must not be created:\n%s", out)
	}
	if !strings.Contains(out, "No HOME") || !fileExists(".mcp.json") || !fileExists("opencode.json") {
		t.Fatalf("without HOME, the project side is still prepared and init succeeds:\n%s", out)
	}
}

// Save does not re-encode if variants were not touched (so people's comments are not lost).
func TestSaveKeepsCommentsInsideVariants(t *testing.T) {
	dir := t.TempDir()
	p := specWithVariants(t, dir)
	b, _ := os.ReadFile(p)
	src := strings.Replace(string(b), "variants:\n", "variants: # keep me\n  # and me\n", 1)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, p)
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), "keep me") || !strings.Contains(string(after), "and me") {
		t.Fatalf("comments inside variants must survive a Save that did not change them:\n%s", after)
	}
}

// 0.9.1's changelog and shikii_reflect's description say "submit has no limit on conversations or items". The input schema (what MCP clients and models follow) must not limit them either
func TestAudit091ReflectSchemaHasNoItemLimits(t *testing.T) {
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_reflect" {
			continue
		}
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		for _, k := range []string{"reviews", "wiki", "memory", "maintenance"} {
			if m, ok := props[k].(map[string]any)["maxItems"]; ok {
				t.Errorf("shikii_reflect's %s still has maxItems=%v (the description says there is no limit)", k, m)
			}
		}
	}
}

// Two `shikii --init` at once (two projects, one HOME) rewrite ~/.claude/skills/shikii at the same time. Before the lock, a copy failed with
// "directory not empty" or left the skill without SKILL.md. Checked in one run instead of waiting for copies to race: right before the old
// skill is deleted, the lock next to it cannot be taken from outside, so another copy waits until this one has put every file back.
func TestAuditSkillCopyHoldsTheLock(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "shikii")
	if err := copySkill(dst); err != nil { // a skill already there, as on every --init after the first
		t.Fatal(err)
	}
	tried := errors.New("whileCopyingSkill did not run")
	old := whileCopyingSkill
	t.Cleanup(func() { whileCopyingSkill = old })
	whileCopyingSkill = func() { tried = tryLockFromOutside(filepath.Join(filepath.Dir(dst), ".shikii-skill.lock")) }
	if err := copySkill(dst); err != nil {
		t.Fatal(err)
	}
	if tried != syscall.EWOULDBLOCK {
		t.Fatalf("the skill is rewritten while its lock is held: taking it from outside gave %v", tried)
	}
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatal("the skill was left without SKILL.md")
	}
}

func probeBlock(cmd string) string {
	return "[mcp_servers.shikii]\ncommand = " + quotedList([]string{cmd}) + "\nargs = [" + quotedList(codexServerArgs) + "]\n"
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

// Final audit BC-1 (B2): a store 0.9.6 wrote is moved before 0.9.7 ever opened it. One `shikii wiki query` at the new path before --init
// (or an MCP query on the store it started in) recorded the store's name as the hash of the new path; the --init after it could not change
// it, and the wiki 0.9.6 wrote was no longer found. The name now comes from the path in project.json.
func TestFinalBC1WikiQueryBeforeInitHidesThe096Wiki(t *testing.T) {
	for _, query := range []bool{false, true} { // false is the control: --init alone keeps the 0.9.6 wiki
		t.Run(map[bool]string{false: "control-init-only", true: "query-then-init"}[query], func(t *testing.T) {
			finalBC1(t, query)
		})
	}
}

func finalBC1(t *testing.T, query bool) {
	common, old, moved := movedProject(t)
	asIf096(t, common, old, moved)
	t.Chdir(moved)
	if query {
		capture(t, func() error { return cmdNotebook([]string{"query"}) }) // e.g. the skill's "query the wiki first", before --init
	}
	capture(t, func() error { return cmdInit(nil) }) // the person's --init, as the notice asks
	sum := sha256.Sum256([]byte(old))
	recorded, _ := os.ReadFile(filepath.Join(moved, ".shikii", "records", "store-name"))
	live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live()
	if strings.TrimSpace(string(recorded)) != hex.EncodeToString(sum[:]) || err != nil || len(live) != 1 {
		t.Fatalf("B2: a store 0.9.6 wrote is read as it is after a move: store-name=%q (want sha256(old)=%s), %d pages found %v",
			strings.TrimSpace(string(recorded)), hex.EncodeToString(sum[:]), len(live), err)
	}
}

// C2/C4 in a folder that is not a Git repository: shikii --init in a copy makes it a project of its own (a new store id) as it does in a
// Git repository.
func TestFull1InitInNonGitCopyGetsItsOwnStoreID(t *testing.T) {
	for _, git := range []bool{true, false} {
		p := t.TempDir()
		if git {
			p = gitRepo(t)
		}
		hookEnv(t, nil, p)
		P := feedbackScope(p)
		idP := storeID(filepath.Join(P, ".shikii"), false)
		q := filepath.Join(t.TempDir(), "copy")
		full1Copy(t, P, q)
		Q := feedbackScope(q)
		t.Chdir(Q)
		out := capture(t, func() error { return cmdInit(nil) })
		if active, _ := projectActive(filepath.Join(Q, ".shikii"), Q); !active {
			t.Fatalf("git=%v: precondition: --init makes the copy active", git)
		}
		if storeID(filepath.Join(Q, ".shikii"), false) == idP {
			t.Errorf("git=%v: C4: the copy taken over with --init keeps the original's store id (two active projects share one store id)\n%s", git, out)
		}
	}
}

// C1 for --init in a folder that is not a Git repository: the person's --init on a moved folder forgets the old path's entry, as in Git.
func TestFull1InitInNonGitMovedFolderForgetsOldEntry(t *testing.T) {
	for _, git := range []bool{true, false} {
		p := t.TempDir()
		if git {
			p = gitRepo(t)
		}
		hookEnv(t, nil, p)
		common, _ := agentHome()
		P := feedbackScope(p)
		r := filepath.Join(t.TempDir(), "moved")
		if err := os.Rename(P, r); err != nil {
			t.Fatal(err)
		}
		R := feedbackScope(r)
		t.Chdir(R)
		out := capture(t, func() error { return cmdInit(nil) })
		if !strings.Contains(out, "Reconnected") {
			t.Errorf("git=%v: --init on a moved folder does not reconnect it:\n%s", git, out)
		}
		if _, err := os.Stat(filepath.Join(common, "projects", projectID(P)+".json")); err == nil {
			t.Errorf("git=%v: the old path's index entry is left", git)
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

// 4. HOME is judged by the actual folder (SameFile): refuse even a spelling with different case (meaningful only on a case-insensitive FS).
func TestInitRefusesHomeSpelledDifferently(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	upper := strings.ToUpper(home)
	if upper == home {
		t.Skip("cannot make an upper-case spelling")
	}
	if _, err := os.Stat(upper); err != nil {
		t.Skip("case-sensitive FS")
	}
	if err := os.Chdir(upper); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { os.Chdir(home) })
	if err := cmdInit(nil); err == nil || !strings.Contains(err.Error(), "home folder") {
		t.Fatalf("HOME is not initialized even with a different spelling: %v", err)
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

// re3-4 (C5, wording): the other-path notice says when to ask the person as the skill's wiki.md does: copy the missing files, keep the
// old store, ask before deleting it. The notice says to copy when the request is done and also to check with the person first.
func TestRe3OtherPathNoticeAsksWhenTheSkillDoes(t *testing.T) {
	b, err := os.ReadFile("../../skills/shikii/references/wiki.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(b)
	if !strings.Contains(skill, "copy the missing files, keep the other store, and ask the person before deleting it") {
		t.Fatalf("precondition: wiki.md says to ask before deleting")
	}
	if strings.Contains(movedHow, "check with the person first") {
		t.Fatalf("C5: the notice asks the person before copying, wiki.md only before deleting: %q", movedHow)
	}
}

// 0.9.7: replies in flight are kept alive while a calibration holds or waits for the lock (keepRepliesAliveDuringCalibration), whatever path
// reached it. That rests on one rule kept here: only lockCalibration touches calibMu, so the count of active calibrations is always kept.

func packageSources(t *testing.T) map[string]string {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	out := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		out[file] = string(b)
	}
	return out
}

// calibMuUsers returns the functions that touch calibMu.
func calibMuUsers(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var out []string
	for file, src := range srcs {
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "calibMu" {
					out = append(out, fn.Name.Name)
				}
				return true
			})
		}
	}
	return out
}

func TestOnlyLockCalibrationTouchesTheCalibrationLock(t *testing.T) {
	users := calibMuUsers(t, packageSources(t))
	if len(users) == 0 {
		t.Fatal("precondition: lockCalibration is found")
	}
	for _, fn := range users {
		if fn != "lockCalibration" {
			t.Errorf("%s touches calibMu: take the lock with defer s.lockCalibration(w)(), which extends the waiting reply's write deadline", fn)
		}
	}
	bad := map[string]string{"bad.go": "package main\nfunc (s *server) handleNew() { s.calibMu.Lock(); defer s.calibMu.Unlock() }\n"}
	if got := calibMuUsers(t, bad); len(got) != 2 || got[0] != "handleNew" {
		t.Fatalf("negative example: a lock taken outside lockCalibration is found: %v", got)
	}
}

// Audit 0.8.1 item 2: CI runs with read-only permissions, cancels older runs on the same branch, and caches npm downloads (CI cost).
func TestCIWorkflowIsReadOnlyAndCached(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
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

// 0.8.2: CI checks for leaked secrets (gitleaks, version and hash pinned, history included) and known vulnerabilities in the Go packages and standard library in use
// (govulncheck, version pinned). The Go version is aligned through go.mod's toolchain to a version with the vulnerabilities fixed.
// Audit 0.8.2-2 B2: compare the security job's steps to a fixed sequence by exact match (an allowlist), not by whether strings are present.
// Removing the hash check, commenting out govulncheck, or swallowing gitleaks failures all fail.
func TestCIScansSecretsAndVulnerabilities(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
				Run  string            `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatal(err)
	}
	job, ok := wf.Jobs["security"]
	if !ok {
		t.Fatal("test.yml has no security job")
	}
	var got []string
	for _, st := range job.Steps {
		switch {
		case st.Uses != "":
			got = append(got, "uses "+st.Uses+" "+st.With["fetch-depth"]+st.With["go-version-file"])
		default:
			got = append(got, strings.TrimSpace(st.Run))
		}
	}
	want := []string{
		"uses actions/checkout@v4 0",
		"uses actions/setup-go@v5 go.mod",
		`curl -fsSL -o /tmp/gitleaks.tar.gz https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz
echo "551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb  /tmp/gitleaks.tar.gz" | sha256sum -c -
tar -xzf /tmp/gitleaks.tar.gz -C /tmp gitleaks
/tmp/gitleaks git --redact --no-banner .`,
		"go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the security job's steps differ from the fixed sequence:\n got %q\nwant %q", got, want)
	}
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "\ntoolchain go1.27.3\n") {
		t.Error("go.mod's toolchain is not go1.27.3 (GO-2026-6809 was fixed in go1.27.3)")
	}
}

// Codex audit 0.9.2 (.shikii/handoff/codex-audit-0.9.2/report.md).

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
	if b, _ := os.ReadFile(path); string(b) != "[mcp_servers.shikii]\ncommand = "+quotedList([]string{exe})+"\n"+codexServerArgsLine+"\n" {
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

// Confirmation audit 2: a lone surrogate escape (\ud800) is valid JSON grammar and Node's JSON.parse keeps it, but Go decodes it to
// U+FFFD. Two different keys then collapse into one and the rewrite drops an entry of the person's (and changes the value text).
func TestConfirm2LoneSurrogateKeysAreNotDropped(t *testing.T) {
	text := "{\"mcp\": {\"\\ud800\": {\"command\": \"a\"}, \"\\udc00\": {\"command\": \"b\"}}}\n"
	p := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	did, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
	b, _ := os.ReadFile(p)
	if err != nil && string(b) == text {
		return // refused and left unchanged: fine
	}
	if !bytes.Contains(b, []byte("\"a\"")) || !bytes.Contains(b, []byte("\"b\"")) || !strings.Contains(strings.ToLower(string(b)), "\\ud800") {
		t.Errorf("a person's entry was dropped or changed by the rewrite (did=%v, err=%v): %q", did, err, b)
	}
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

// Counterexamples for the stumbles found in the first-time user trial (2026-10-18, one full round of sorting inquiry emails from the documents alone).
//
// (1) init copies the ready-made knowledge (index.md, types/, patterns/, models/) into wiki/. Pages the person already has are not touched. Every link in the copied pages resolves inside that wiki.
func TestInitSeedsTheWikiWithSharedKnowledgeWithoutOverwriting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	mine := filepath.Join(dir, ".shikii", "wiki", "types", "choice.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "wiki/: ") || !strings.Contains(out, "built-in knowledge") {
		t.Fatalf("init must say it seeded the wiki:\n%s", out)
	}
	for _, p := range []string{"index.md", "types/noul.md", "types/score.md", "models/kev-4b.md"} {
		if !fileExists(filepath.Join(dir, ".shikii", "wiki", p)) {
			t.Fatalf("seeded wiki must have %s", p)
		}
	}
	if pats, _ := filepath.Glob(filepath.Join(dir, ".shikii", "wiki", "patterns", "*.md")); len(pats) == 0 {
		t.Fatal("seeded wiki must have the patterns (the proposer reads them)")
	}
	if b, _ := os.ReadFile(mine); string(b) != "# mine\n" {
		t.Fatalf("a page the person already has is never overwritten: %q", b)
	}
	// Relative links in the copied pages all resolve inside this wiki (fails if they point to runs/ or specs/, which exist only in the repo)
	link := regexp.MustCompile(`\]\(([^)#]+)[^)]*\)`)
	if err := filepath.Walk(filepath.Join(dir, ".shikii", "wiki"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, m := range link.FindAllStringSubmatch(string(b), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(p), filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to %s, which a fresh project's wiki does not have", strings.TrimPrefix(p, dir+"/"), target)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The second time: nothing to add, so it stays quiet
	if out := capture(t, func() error { return cmdInit(nil) }); strings.Contains(out, "pages of shared knowledge added") {
		t.Fatalf("second init has nothing to seed:\n%s", out)
	}
}

func TestGenericAgentSpecsKeepCodingCalibrationsSeparate(t *testing.T) {
	// Hashes of the ready-made questions of 0.4.4. Old lines are not carried over to the generalized questions.
	previous := map[string]string{
		"prompt-kind": "68ed82631e63f2a2", "needs-plan": "66d4d3ef366b8b96",
		"changes-in-scope": "9381415d9f1d3696", "done-as-asked": "2bba11b5ee687b57",
	}
	dir := t.TempDir()
	if n, err := seedAgentSpecs(dir); err != nil || n != len(hookSpecs)+len(wikiSpecs) {
		t.Fatal("prepare the ready-made questions (4 for the hook and 2 for the wiki)", n, err)
	}
	for id, oldHash := range previous {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(dir, id+".yaml")
			sp := mustLoad(t, path)
			sp.Calibration = []spec.Calibration{{Model: "fixture", SpecHash: oldHash, Method: "identity", N: 50}}
			if sp.Hash() == oldHash || sp.CalibrationFor("fixture") != nil || sp.LiveHash(oldHash) {
				t.Fatal("separate the old coding-oriented questions and calibration from the new questions")
			}
			if id == "needs-plan" && !needsPlanningContext(sp) {
				t.Fatal("keep the contract of using agreements and open items with the original text")
			}
			// Questions and lines in use are protected from re-initialization and updates.
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("\n# 利用者の質問を保持\n")...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := seedAgentSpecs(dir); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatal("do not overwrite existing questions with the generic version", err)
			}
		})
	}
}

// Running shikii --init in a moved folder reconnects it the same way (the CLI and MCP say "run shikii --init" until a request's hook has run).
func TestInitInAMovedFolderReconnectsIt(t *testing.T) {
	common, old, moved := movedProject(t)
	capture(t, func() error { return cmdInit(nil) })
	if paths := registeredPaths(common); len(paths) != 1 || paths[0] != moved {
		t.Fatalf("the shared index lists the new path only (old %s): %v", old, paths)
	}
	if _, err := os.Lstat(filepath.Join(common, "projects", projectID(old)+".json")); !os.IsNotExist(err) {
		t.Fatalf("the old path's entry is removed from the shared index: %v", err)
	}
}

// Only hookSpecPath builds the path of a question file (decisions/<id>.yaml) (allow list). Building it elsewhere misses the adopted version.
func TestOnlyHookSpecPathBuildsSpecFilePaths(t *testing.T) {
	pattern := regexp.MustCompile(`"decisions",\s*[^)]*\+\s*"\.yaml"|"decisions",\s*"[a-z-]+\.yaml"`)
	if !pattern.MatchString(`filepath.Join(agent, "decisions", id+".yaml")`) || !pattern.MatchString(`filepath.Join(root, "decisions", "needs-plan.yaml")`) {
		t.Fatal("the check's pattern does not catch building with a fixed name")
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range pattern.FindAllStringIndex(string(data), -1) {
			if f == "hook_variants.go" && strings.Contains(string(data[max(0, m[0]-400):m[0]]), "func hookSpecPath(") {
				continue
			}
			line := strings.Count(string(data[:m[0]]), "\n") + 1
			t.Errorf("%s:%d builds a question file path itself (use hookSpecPath)", f, line)
		}
	}
}

// Tests never start real background processes (up, serve) and never open a browser (0.9.1). Even a test that runs the hook without the harness (hookEnv)
// started a real `shikii.test up` on a machine with llama-server, which was never cleaned up (2026-10-25: 75 of them kept calling git,
// and each git call ran xcodebuild -license check, pushing the machine's load above 150)
// The same holds for uninstall: it never sends a real signal and never runs the real ps.
func TestTestsNeverStartRealProcesses(t *testing.T) {
	for _, c := range []struct {
		name         string
		inTest, real any
	}{
		{"hookSpawn (spawnShikii)", hookSpawn, spawnShikii},
		{"openBrowser (openInBrowser)", openBrowser, openInBrowser},
		{"stopProcess (signalTerm)", stopProcess, signalTerm},
		{"processCommand (psCommand)", processCommand, psCommand},
	} {
		if reflect.ValueOf(c.inTest).Pointer() == reflect.ValueOf(c.real).Pointer() {
			t.Errorf("during tests, %s must be a recorder, not the real one", c.name)
		}
	}
}

// Every tool takes project, so a server shared by every Codex session (one registration in ~/.codex/config.toml) uses the caller's project.
func TestMCPToolsListProject(t *testing.T) {
	for _, tl := range mcpTools {
		props := tl["inputSchema"].(map[string]any)["properties"].(map[string]any)
		p, _ := props["project"].(map[string]any)
		if p == nil || p["type"] != "string" || !strings.Contains(p["description"].(string), "absolute path") {
			t.Fatalf("%s: inputSchema must have project with a description: %v", tl["name"], props["project"])
		}
	}
	if !strings.Contains(mcpInstructions, "project") || !strings.Contains(mcpInstructions, "working folder") {
		t.Fatal("mcpInstructions must tell agents to pass their working folder as project")
	}
}

var builtInSpec = []byte("name: needs-plan\nquestion: Should the plan be agreed first?\n")

// A built-in file already there is the person's from then on: shortened, emptied or whole, init and the hook leave it as it is (0.9.10
// audit a-1: a rewrite of a part brought back what the person had cut).
func TestPatch0910BuiltInFileAlreadyThereStays(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	for _, text := range []string{"name: needs-plan\n", "", string(builtInSpec), "name: mine\n"} {
		path := filepath.Join(t.TempDir(), "needs-plan.yaml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		created, err := seedFile(path, builtInSpec)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); string(b) != text || created {
			t.Fatalf("%q became %q (created %v)", text, b, created)
		}
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

// The audit of the 0.9.9 changes (v0.9.8..34451f3, .shikii/handoff/plan-0.9.9/audit.md). Each test failed on 34451f3.

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

func TestFailedInitDoesNotEnableHooks(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	if err := os.WriteFile(".mcp.json", []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	oldOut := stdout
	stdout = &strings.Builder{}
	err := cmdInit(nil)
	stdout = oldOut
	if err == nil {
		t.Fatal("initialization must fail with broken settings")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "failed", "cwd": repo, "prompt": "README の日本語化を進めてください"})
	root := filepath.Join(repo, ".shikii")
	records, _ := (store.Store{Dir: root}).Records("prompt-kind", 0)
	if len(records) != 0 {
		t.Fatal("an initialization that failed midway must not count as active")
	}
	common, _ := agentHome()
	for _, p := range registeredProjects(common) {
		if p.Path == repo {
			t.Fatal("a failed project must not be registered with the page")
		}
	}
}

func TestInitAliasUsesTheSameSetup(t *testing.T) {
	if os.Getenv("SHIKII_TEST_INIT_ALIAS") == "1" {
		os.Args = []string{"shikii", "--init"}
		main()
		return
	}
	dir, home := t.TempDir(), t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitAliasUsesTheSameSetup$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SHIKII_TEST_INIT_ALIAS=1", "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--init must lead to the same initialization: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".shikii", "project.json")); err != nil {
		t.Fatal("--init must activate the project: ", err)
	}
}

// The release archive (scripts/build-release.py) bundles both READMEs, and the English install text points at the English README's
// Requirements section, which exists (audit 0.9.2 translation, low 4: it pointed at README_ja.md and bundled only that).
func TestReleaseBundlesBothReadmesAndPointsAtTheEnglishOne(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-release.py"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	bundled := regexp.MustCompile(`(?s)BUNDLED = \[(.*?)\]\n`).FindStringSubmatch(script)
	if bundled == nil {
		t.Fatal("build-release.py has no BUNDLED list")
	}
	pairs := regexp.MustCompile(`\("([^"]+)", "([^"]+)"\)`).FindAllStringSubmatch(bundled[1], -1)
	got := map[string]string{}
	for _, p := range pairs {
		got[p[1]] = p[2]
	}
	if got["README.md"] != "README.md" || got["docs/readme/README_ja.md"] != "README_ja.md" {
		t.Errorf("both READMEs are bundled: %v", got)
	}
	// 2 binaries and INSTALL.txt besides the bundled files: the count the script checks must follow the list
	if want := len(pairs) + 3; !strings.Contains(script, "len(members) == "+strconv.Itoa(want)) || !strings.Contains(script, "are not "+strconv.Itoa(want)+" regular files") {
		t.Errorf("the archive check expects %d files (%d bundled + 2 binaries + INSTALL.txt)", want, len(pairs))
	}
	install := regexp.MustCompile(`(?s)install = f"""(.*?)"""`).FindStringSubmatch(script)
	if install == nil || !strings.Contains(install[1], `See "Requirements" in README.md`) {
		t.Fatalf("the English install text points at README.md's Requirements: %v", install)
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^## Requirements$`).Match(readme) {
		t.Error("README.md has a Requirements section")
	}
}

func relinkInit(t *testing.T, f string) string {
	t.Helper()
	t.Chdir(f)
	return capture(t, func() error { return cmdInit(nil) })
}

// C3: a .shikii brought in from elsewhere (another machine, an archive) naming this project: a request leaves it inactive, and the
// person's shikii --init makes it a project of its own.
func TestRelinkStoreBroughtInWaitsForInit(t *testing.T) {
	relinkEachPlace(t, func(t *testing.T, common, p string) {
		f := relinkCopy(t, p, "brought-in")
		if err := os.WriteFile(filepath.Join(f, ".shikii", "records", "store-id"), []byte(strings.Repeat("ab", 16)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if n := relinkMovedProject(f, false); n != "" || relinkActive(f) {
			t.Fatalf("C3: a request switched on a store brought in from elsewhere: %q", n)
		}
		out := relinkInit(t, f)
		if !relinkActive(f) {
			t.Fatalf("C3: shikii --init does not make it a project:\n%s", out)
		}
		if !relinkActive(p) {
			t.Fatal("C3: the original is not active any more")
		}
		relinkIndexNamesItsStores(t, common)
	})
}

// C1/C4: cp -R, then the original is moved before any request, then the person runs shikii --init in the copy. The copy becomes a project
// of its own with a new store id, so the original, asked for at its new path, is still reconnected.
func TestRelinkCopyTakenOverAfterTheOriginalMovedKeepsTheOriginal(t *testing.T) {
	relinkEachPlace(t, func(t *testing.T, common, p string) {
		q := relinkCopy(t, p, "copy")
		r := relinkMove(t, p)
		out := relinkInit(t, q)
		if !relinkActive(q) {
			t.Fatalf("shikii --init does not make the copy a project:\n%s", out)
		}
		if storeID(filepath.Join(q, ".shikii"), false) == storeID(filepath.Join(r, ".shikii"), false) {
			t.Fatalf("C4: the copy taken over with --init keeps the original's store id:\n%s", out)
		}
		if n := relinkMovedProject(r, false); !strings.HasPrefix(n, "shikii:") || !relinkActive(r) {
			t.Fatalf("C1: the moved original is not reconnected after its copy was taken over: %q", n)
		}
		relinkIndexNamesItsStores(t, common)
	})
}

func TestSeedFailureDoesNotPublishPartialSpec(t *testing.T) {
	if dir := os.Getenv("SHIKII_SEED_FAILURE_DIR"); dir != "" {
		// The file size limit applies only to this throwaway child process.
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 0, Max: 0}); err != nil {
			os.Exit(2)
		}
		if _, err := seedAgentSpecs(dir); err == nil {
			os.Exit(3)
		}
		return
	}
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestSeedFailureDoesNotPublishPartialSpec$")
	cmd.Env = append(os.Environ(), "SHIKII_SEED_FAILURE_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("confirm the synthetic I/O failure in the child process: %v %s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) != 0 {
		t.Fatalf("do not publish a spec that failed to write under its final name: %v %v", files, err)
	}
	n, err := seedAgentSpecs(dir)
	if err != nil || n != len(hookSpecs)+len(wikiSpecs) {
		t.Fatalf("a retry prepares every ready-made question correctly: n=%d err=%v", n, err)
	}
	for _, id := range hookSpecs {
		mustLoad(t, filepath.Join(dir, id+".yaml"))
	}
}

// 0.9.6 recheck, low 2: a background tune holds the calibration lock for its model run and then a calibration that may ask the model again, so
// a reply waiting behind it needs longResponseLimit longer than both together (it was 30 minutes, the same as the tune alone).
func TestLongResponseLimitCoversATuneAndItsCalibration(t *testing.T) {
	if longResponseLimit <= tuneRunLimit+replayLimit {
		t.Fatalf("longResponseLimit %v must exceed a tune (%v) plus its calibration (%v)", longResponseLimit, tuneRunLimit, replayLimit)
	}
}

// The person asked (2026-10-26) that the skill says not to add duplicate wiki pages: one topic, one page; merge into the page
// that already covers the topic (id), and fold existing duplicates into one (replaces) instead of keeping both.
func TestSkillSaysNotToAddDuplicatePages(t *testing.T) {
	for path, wants := range map[string][]string{
		"../../skills/shikii/SKILL.md":             {"Do not add duplicate pages", "one page per topic"},
		"../../skills/shikii/references/wiki.md":   {"Do not add duplicate pages", "one page per topic", "`replaces`"},
		"../../skills/shikii/references/review.md": {"duplicate"},
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wants {
			if !strings.Contains(string(b), w) {
				t.Fatalf("%s must say %q", path, w)
			}
		}
	}
}

// 0.9.3: the person asked that the skill says briefly how to remove shikii, so an agent asked to "uninstall shikii" runs the dry run,
// confirms with the person, keeps the data, and passes on the rm command instead of deleting files by hand.
func TestSkillSaysHowToUninstall(t *testing.T) {
	b, err := os.ReadFile("../../skills/shikii/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shikii uninstall", "--yes", "data", "rm"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("SKILL.md must say how to uninstall (missing %q)", want)
		}
	}
}

// 0.9.2 redesign: the page asks a person each ready-made question once, in plain words, in Japanese and in English.
// Every shipped spec has a display for ja and en with a title, a question that is one sentence (the question mark comes before any end of a sentence,
// so there is no preface such as "from your answer the AI learns ..." in front of it), and a name for every answer.
// Without an English display the English page fell back to the prompt for the decision model (audit E1).
func TestShippedSpecsAskPeopleInOneSentenceInBothLanguages(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if len(files) != len(hookSpecs)+len(wikiSpecs) {
		t.Fatalf("expected every shipped spec, got %v", files)
	}
	marks := map[string][2]string{"ja": {"？", "。"}, "en": {"?", ". "}}
	for _, f := range files {
		s, err := spec.Load(f)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{"yes", "no"}
		if s.Type != spec.Noul {
			ids = nil
			for _, o := range s.Options {
				ids = append(ids, o.ID)
			}
		}
		for lang, m := range marks {
			d, ok := s.Display[lang]
			if !ok || d.Title == "" || d.Question == "" {
				t.Errorf("%s: display.%s needs a title and a question", s.ID, lang)
				continue
			}
			q := strings.TrimSpace(d.Question)
			i := strings.Index(q, m[0])
			if i < 0 || strings.Contains(q[:i], m[1]) {
				t.Errorf("%s: display.%s.question must be one question sentence with nothing before it: %q", s.ID, lang, q)
			}
			for _, id := range ids {
				if d.Options[id] == "" {
					t.Errorf("%s: display.%s.options has no name for %q", s.ID, lang, id)
				}
			}
		}
	}
}

// A split spec puts every step in one call and builds the original choices' distribution from the product of branch probabilities.
func TestDecideRunsStepsInOneCallAndMultipliesBranches(t *testing.T) {
	srv, calls, questions := stepwiseModel(t)
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	sp.Steps = []spec.Step{{Ask: "Does it ask for work?", No: "other"}, {Ask: "Does it ask to change files?", Yes: "change_request", No: "question"}}
	if err := sp.Validate(); err != nil {
		t.Fatal(err)
	}
	eps := []decide.Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
	d, err := decide.Decide(context.Background(), sp, map[string]string{"message": "fix a"}, decide.Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	// fix a: work 0.9 → change 0.8: other 0.1, change_request 0.72, question 0.18
	want := map[string]float64{"other": 0.1, "change_request": 0.72, "question": 0.18}
	for k, v := range want {
		if math.Abs(d.Raw[k]-v) > 1e-9 {
			t.Fatalf("raw[%s]=%v want %v (all: %v)", k, d.Raw[k], v, d.Raw)
		}
	}
	if d.Top != "change_request" || *calls != 1 || *questions != 2 {
		t.Fatalf("steps must travel in one request: top=%s calls=%d questions=%d", d.Top, *calls, *questions)
	}
	if d.SpecHash != sp.Hash() || d.Type != "choice" {
		t.Fatalf("a decomposed decision is still a choice decision under the spec's key: %+v", d)
	}
	// A middle step that settles on yes: change 0.8 → remaining 0.2 × (work 0.9 → question 0.18, other 0.02)
	sp.Steps = []spec.Step{{Ask: "Does it ask to change files?", Yes: "change_request"}, {Ask: "Does it ask for work?", Yes: "question", No: "other"}}
	d, err = decide.Decide(context.Background(), sp, map[string]string{"message": "fix a"}, decide.Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]float64{"change_request": 0.8, "question": 0.18, "other": 0.02} {
		if math.Abs(d.Raw[k]-v) > 1e-9 {
			t.Fatalf("yes-settling middle step: raw[%s]=%v want %v (all: %v)", k, d.Raw[k], v, d.Raw)
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
