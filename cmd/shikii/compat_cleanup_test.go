package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The compatibility cleanup of 0.9.7 (2026-10-27): what released versions wrote as data is converted once, when shikii is updated; an
// older shikii running next to this one, or a setting in an older version's form, is only told to the person with the update to make.

// project096 is a project at its first path as 0.9.6 left it (no store id or name, a path-named index entry). Nothing is moved yet.
func project096(t *testing.T) (common, old string) {
	t.Helper()
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	old = feedbackScope(repo)
	root := filepath.Join(old, ".shikii")
	o, err := (notebook.Book{Root: root, Scope: old}).Capture(notebook.Observation{Kind: "test", Title: "checked", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (notebook.Book{Root: root, Scope: old}).Ingest(notebook.Draft{Title: "a page", Lesson: "Check.", AppliesTo: "a", Kind: "rule", Scene: "when writing", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
	common, _ = agentHome()
	asIf096(t, common, old, old)
	return common, old
}

// G1: the update writes the store id, the store folder and the store name for every project 0.9.6 registered, so a move after the update
// is reconnected at the next request (final audit BC-2 is gone). The path-named entry is replaced by the store id's.
func TestUpdateConvertsEveryProjectThe096IndexHas(t *testing.T) {
	common, old := project096(t)
	t.Chdir(t.TempDir())
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	root := filepath.Join(old, ".shikii")
	id := storeID(root, false)
	if id == "" {
		t.Fatalf("G1: the update writes the store id of a project 0.9.6 registered\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(common, "projects", id+".json")); err != nil {
		t.Fatalf("G1: and its entry under the store id: %v", err)
	}
	if _, err := os.Stat(filepath.Join(common, "projects", projectID(old)+".json")); !os.IsNotExist(err) {
		t.Fatalf("G1: the path-named entry 0.9.6 wrote is replaced: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "records", "store-name")); err != nil || strings.TrimSpace(string(b)) != projectID(old) {
		t.Fatalf("G1: the store name is the one 0.9.6 used: %q %v", b, err)
	}
	moved := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	t.Chdir(moved)
	if note := relinkMovedProject(moved, false); !strings.Contains(note, "moved or renamed") {
		t.Fatalf("G1: a move after the update is reconnected by itself: %q", note)
	}
	if live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live(); err != nil || len(live) != 1 {
		t.Fatalf("G1: with the wiki 0.9.6 wrote: %d pages %v", len(live), err)
	}
}

// G2: an older shikii on this machine wrote a path-named entry for a project 0.9.7 already gave a store id. The entry is not read and not
// removed; the hook tells the agent once per session to have every shikii updated.
func TestOlderShikiiNextToThisOneIsOnlyTold(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	common, _ := agentHome()
	scope := feedbackScope(repo)
	prompt := func(session string) string {
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": scope, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}))
	}
	if first := prompt("s0"); strings.Contains(first, "older shikii") {
		t.Fatalf("control: no older shikii, nothing told: %q", first)
	}
	if storeID(filepath.Join(scope, ".shikii"), false) == "" {
		t.Fatal("precondition: the store has its id")
	}
	legacy := filepath.Join(common, "projects", projectID(scope)+".json")
	b, _ := json.Marshal(map[string]any{"id": projectID(scope), "name": filepath.Base(scope), "path": scope, "url": "/p/" + projectID(scope) + "/"})
	if err := os.WriteFile(legacy, b, 0o600); err != nil {
		t.Fatal(err)
	}
	first, second := prompt("s1"), prompt("s1")
	if !strings.Contains(first, "older shikii") || !strings.Contains(first, "update every shikii") {
		t.Fatalf("G2: the hook tells the agent to have every shikii updated: %q", first)
	}
	if strings.Contains(second, "older shikii") {
		t.Fatalf("G2: once per session: %q", second)
	}
	if got, err := os.ReadFile(legacy); err != nil || string(got) != string(b) {
		t.Fatalf("G2: the older shikii's entry is left as it is: %q %v", got, err)
	}
	if paths := registeredPaths(common); len(paths) != 1 || paths[0] != scope {
		t.Fatalf("G2: the project is listed once, from its own entry: %v", paths)
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

// G4: shikii never rewrites AGENTS.md. A section an older version added is left, and init and the update say how to remove it.
func TestOlderAgentsSectionIsOnlyTold(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	plain, withOld := gitRepo(t), gitRepo(t)
	human := "# Office rules\n\n- Invoices go in invoices/.\n"
	old := human + "\n" + agentsOld("## shikii (decision rules)")
	os.WriteFile(filepath.Join(plain, "AGENTS.md"), []byte(human), 0o644)
	os.WriteFile(filepath.Join(withOld, "AGENTS.md"), []byte(old), 0o644)
	t.Chdir(plain)
	if out := capture(t, func() error { return cmdInit(nil) }); strings.Contains(out, "AGENTS.md") {
		t.Fatalf("control: nothing said about an AGENTS.md without the section: %s", out)
	}
	t.Chdir(withOld)
	for name, run := range map[string]func() error{"init": func() error { return cmdInit(nil) }, "update": func() error { return cmdHook([]string{"install", "--yes"}) }} {
		out := capture(t, run)
		if b, _ := os.ReadFile(filepath.Join(withOld, "AGENTS.md")); string(b) != old {
			t.Fatalf("G4 %s: AGENTS.md is left as it is: %q", name, b)
		}
		if !strings.Contains(out, "AGENTS.md: an older shikii added") || !strings.Contains(out, agentsMarker) {
			t.Fatalf("G4 %s: says how to remove the section by hand:\n%s", name, out)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(plain, "AGENTS.md")); string(b) != human {
		t.Fatalf("control: an AGENTS.md without the section is untouched: %q", b)
	}
}
