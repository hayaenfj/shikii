package main

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	gconstant "go/constant"
	"go/token"
	"go/types"
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
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
	"gopkg.in/yaml.v3"
)

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
	if !strings.HasSuffix(string(cb), "\n"+codexServerArgsLine+"\n"+codexToolsAllowedLine+"\n") || strings.Contains(string(cb), "--store") {
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
	bad := map[string]string{"bad.go": "package main\nimport \"sync\"\ntype server struct{ calibMu sync.Mutex }\nfunc (s *server) handleNew() { s.calibMu.Lock(); defer s.calibMu.Unlock() }\n"}
	if got := calibMuUsers(t, bad); len(got) != 2 || got[0] != "handleNew" {
		t.Fatalf("negative example: a lock taken outside lockCalibration is found: %v", got)
	}
	// read by its type: a lock reached through a method value or an embedded server is found too
	for name, src := range map[string]string{
		"a method value":  "package main\nimport \"sync\"\ntype server struct{ calibMu sync.Mutex }\nfunc (s *server) handleNew() { lock := s.calibMu.Lock; lock() }\n",
		"an embedded one": "package main\nimport \"sync\"\ntype server struct{ calibMu sync.Mutex }\ntype wrapped struct{ *server }\nfunc handleNew(w wrapped) { w.calibMu.Lock() }\n",
	} {
		if got := calibMuUsers(t, map[string]string{"bad.go": src}); len(got) == 0 || got[0] != "handleNew" {
			t.Errorf("negative example (%s): a lock taken outside lockCalibration is not found: %v", name, got)
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
		`curl -fsSL -H 'Accept: application/octet-stream' -o /tmp/gitleaks.tar.gz https://api.github.com/repos/gitleaks/gitleaks/releases/assets/378332058
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
	if !strings.Contains(string(mod), "\ntoolchain go1.27.4\n") {
		t.Error("go.mod's toolchain is not go1.27.4 (GO-2026-6809 was fixed in go1.27.3, GO-2026-6859 in go1.27.4)")
	}
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
	if n, err := seedAgentSpecs(dir); err != nil || n != len(hookSpecs)+len(wikiSpecs)+len(deliverySpecs) {
		t.Fatal("prepare the ready-made questions (4 for the hook, 2 for the wiki and 1 for the delivery)", n, err)
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
	if problems := specPathBuilders(t, packageSources(t)); len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("%s builds a question file path itself (use hookSpecPath)", p)
		}
	}
	// negative: every spelling of the same path outside hookSpecPath is found, by its type and its constant values
	for name, src := range map[string]string{
		"spelled out":           "package main\nimport \"path/filepath\"\nfunc load(agent, id string) string { return filepath.Join(agent, \"decisions\", id+\".yaml\") }\n",
		"a fixed name":          "package main\nimport \"path/filepath\"\nfunc load(root string) string { return filepath.Join(root, \"decisions\", \"needs-plan.yaml\") }\n",
		"constants":             "package main\nimport \"path/filepath\"\nconst dir, ext = \"decisions\", \".yaml\"\nfunc load(root, id string) string { return filepath.Join(root, dir, id+ext) }\n",
		"another name for Join": "package main\nimport fp \"path/filepath\"\nfunc load(root, id string) string { join := fp.Join; return join(root, \"decisions\", id+\".yaml\") }\n",
		"one string":            "package main\nfunc load(root, id string) string { return root + \"/decisions/\" + id + \".yaml\" }\n",
	} {
		if problems := specPathBuilders(t, map[string]string{"load.go": src}); len(problems) == 0 {
			t.Errorf("negative example (%s): a question file path built outside hookSpecPath is not found", name)
		}
	}
	// control: the folder alone is no question file path
	if problems := specPathBuilders(t, map[string]string{"seed.go": "package main\nimport \"path/filepath\"\nfunc seed(root string) string { return filepath.Join(root, \"decisions\") }\n"}); len(problems) > 0 {
		t.Errorf("control: the folder alone is taken for a question file path: %v", problems)
	}
}

// specPathBuilders reads the sources by their types and returns where an expression holds the folder of the questions (the constant
// "decisions", or a constant with "decisions/" in it) and, in the same expression as a whole, a name that ends in ".yaml" (any
// constant with ".yaml" in it): a path of path/filepath's Join with the name inside or after it, a string built with +, a format of
// fmt.Sprintf. Only hookSpecPath builds one; building it elsewhere misses the adopted version of a question. The folder alone is no
// question file (seeding and listing it).
func specPathBuilders(t *testing.T, sources map[string]string) []string {
	t.Helper()
	var problems []string
	for _, tf := range typedSources(t, sources, typedIsThePackage(sources)) {
		if tf.prelude {
			continue
		}
		info := tf.info
		text := func(e ast.Expr) (string, bool) {
			if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == gconstant.String {
				return gconstant.StringVal(tv.Value), true
			}
			return "", false
		}
		typedDecls(tf.file, func(name string, root ast.Node) {
			if name == "hookSpecPath" && tf.name == "hook_variants.go" {
				return
			}
			parents := typedParents(root)
			seen := map[ast.Node]bool{}
			ast.Inspect(root, func(n ast.Node) bool {
				e, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				c, ok := text(e)
				if !ok || (c != "decisions" && !strings.Contains(c, "decisions/")) {
					return true
				}
				// the expression as a whole: up through the expressions that hold this one
				outer := ast.Node(e)
				for {
					p, ok := parents[outer].(ast.Expr)
					if !ok {
						break
					}
					outer = p
				}
				if seen[outer] {
					return true
				}
				yaml := false
				if strings.Contains(c, ".yaml") {
					// one string with both: a path only as the format of fmt's Sprintf (a text that names the path, like a help or
					// a refusal, is no path)
					if call, ok := parents[e].(*ast.CallExpr); ok && len(call.Args) > 0 && call.Args[0] == e {
						if fn, ok := typeutilCallee(info, nil, call).(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" && fn.Name() == "Sprintf" {
							yaml = true
						}
					}
				} else if _, constantAsAWhole := text(outer.(ast.Expr)); !constantAsAWhole {
					// the folder and a name joined: another constant with .yaml in the same expression
					ast.Inspect(outer, func(m ast.Node) bool {
						if x, ok := m.(ast.Expr); ok && x != e {
							if v, ok := text(x); ok && strings.Contains(v, ".yaml") {
								yaml = true
							}
						}
						return !yaml
					})
				}
				if yaml {
					seen[outer] = true
					problems = append(problems, tf.name+":"+name)
				}
				return true
			})
		})
	}
	return problems
}

// typeutilCallee is the function a call calls: through another name (join := filepath.Join), or a method value, too.
func typeutilCallee(info *types.Info, values map[types.Object]types.Object, call *ast.CallExpr) types.Object {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		obj := info.Uses[fun]
		if fn, ok := values[obj]; ok && values != nil {
			return fn
		}
		return obj
	case *ast.SelectorExpr:
		if sel := info.Selections[fun]; sel != nil {
			return sel.Obj()
		}
		return info.Uses[fun.Sel]
	}
	return nil
}

// typedConstantText joins the constant parts of a sum of strings ("/decisions/" + id + ".yaml" reads "/decisions/.yaml").
func typedConstantText(info *types.Info, e ast.Expr) string {
	if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == gconstant.String {
		return gconstant.StringVal(tv.Value)
	}
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return typedConstantText(info, b.X) + typedConstantText(info, b.Y)
	}
	return ""
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
	if !strings.Contains(mcpInstructions, "project") || !strings.Contains(mcpInstructions, "original project") {
		t.Fatal("mcpInstructions must tell agents to pass the original project for a worktree")
	}
}

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
	if err != nil || n != len(hookSpecs)+len(wikiSpecs)+len(deliverySpecs) {
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
	if len(files) != len(hookSpecs)+len(wikiSpecs)+len(deliverySpecs) {
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
