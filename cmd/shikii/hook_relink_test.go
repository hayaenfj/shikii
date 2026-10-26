package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// movedProject sets up a project with a wiki page, then moves its folder (os.Rename, as mv does) and returns the old and new paths.
func movedProject(t *testing.T) (agent, old, moved string) {
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
	moved = filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	t.Chdir(moved)
	common, _ := agentHome()
	return common, old, moved
}

func registeredPaths(common string) []string {
	var out []string
	for _, p := range registeredProjects(common) {
		out = append(out, p.Path)
	}
	return out
}

// 0.9.7: moving or renaming the project folder stopped shikii until a person followed the manual steps. The request's hook now reconnects
// the project by itself, says so once, and the turn goes on as usual: the wiki is there and the index points at the new path only.
func TestMovedProjectReconnectsByItself(t *testing.T) {
	common, old, moved := movedProject(t)
	prompt := map[string]any{"session_id": "s1", "cwd": moved, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}
	first := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	if !strings.Contains(first, "moved or renamed from "+old) || !strings.Contains(first, "nothing needs fixing") {
		t.Fatalf("says once that it reconnected the project: %q", first)
	}
	if loadSession(filepath.Join(moved, ".shikii"), "s1").TurnID == "" {
		t.Fatal("and the turn goes on as usual (the request is recorded)")
	}
	if active, err := projectActive(filepath.Join(moved, ".shikii"), moved); err != nil || !active {
		t.Fatalf("the project is active at the new path: %v %v", active, err)
	}
	if paths := registeredPaths(common); len(paths) != 1 || paths[0] != moved {
		t.Fatalf("the shared index lists the new path only: %v", paths)
	}
	if _, err := os.Lstat(filepath.Join(common, "projects", projectID(old)+".json")); !os.IsNotExist(err) {
		t.Fatalf("the old path's entry is removed from the shared index: %v", err)
	}
	if live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live(); err != nil || len(live) != 1 {
		t.Fatalf("the wiki is there: %+v %v", live, err)
	}
	prompt["prompt"] = "\u6b21\u306f CHANGELOG \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "moved or renamed") {
		t.Fatalf("only once: %q", again)
	}
}

// A copy (cp -r) leaves the original where it is, still a shikii project. The copy is not taken over: it stays inactive (records keep going to
// the original) and the agent is told how to make it a project of its own.
func TestCopiedProjectIsNotTakenOver(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	original := feedbackScope(repo)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	copied = feedbackScope(copied)
	t.Chdir(copied)
	notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": copied, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}))
	if !strings.Contains(notice, "copied from "+original) || !strings.Contains(notice, "shikii --init") {
		t.Fatalf("tells that this is a copy and how to make it a project of its own: %q", notice)
	}
	if active, _ := projectActive(filepath.Join(copied, ".shikii"), copied); active {
		t.Fatal("the copy is not taken over")
	}
	if active, _ := projectActive(filepath.Join(original, ".shikii"), original); !active {
		t.Fatal("the original stays a project")
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

// The page shikii started for the old path is stopped when the project is reconnected (a page for a folder that is gone would stay as an
// orphan), and only that page: a process whose command line is not that serve is never stopped. No real signal is sent (stopProcess records).
func TestReconnectingStopsOnlyTheOldPathsPage(t *testing.T) {
	common, old, moved := movedProject(t)
	page := os.Getppid() // a live pid stands in for the page; its command line comes from the table
	oldCommand, oldStop := processCommand, stopProcess
	t.Cleanup(func() { processCommand, stopProcess = oldCommand, oldStop })
	serve := "/usr/local/bin/shikii serve --store " + filepath.Join(old, ".shikii") + " --port 0"
	commands := map[int]string{page: serve}
	processCommand = func(pid int) string { return commands[pid] }
	var stopped []int
	stopProcess = func(pid int) error { stopped = append(stopped, pid); return nil }
	if err := writePageRecord(common, old, 1, page); err != nil {
		t.Fatal(err)
	}
	relinkMovedProject(moved, false)
	if len(stopped) != 1 || stopped[0] != page {
		t.Fatalf("the old path's page is stopped: %v", stopped)
	}
	if _, ok := readPageRecord(common, old); ok {
		t.Fatal("the old path's page record is removed")
	}
	// negative example: the recorded pid now runs something else (pid reuse), so it is not stopped
	stopped = nil
	commands[page] = "/bin/sleep 30"
	if err := writePageRecord(common, old, 1, page); err != nil {
		t.Fatal(err)
	}
	forgetProject(old)
	if len(stopped) != 0 {
		t.Fatalf("a process that is not the old path's page is never stopped: %v", stopped)
	}
}

// A .shikii this machine did not initialize at the old path (brought in from an archive or another machine) is never switched on by itself:
// its marker names a path this machine's index does not have, or the index has that path for another store. It stays inactive and its
// marker is not repaired; only the person's shikii --init activates it.
func TestStoreFromElsewhereIsNotReconnectedByItself(t *testing.T) {
	for _, kind := range []string{"path not in the index", "another store at that path"} {
		t.Run(kind, func(t *testing.T) {
			_, old, moved := movedProject(t)
			root := filepath.Join(moved, ".shikii")
			marker := filepath.Join(root, "project.json")
			switch kind {
			case "path not in the index": // a store from another machine: its own id, naming a path this machine never had
				data, _ := os.ReadFile(marker)
				data = []byte(strings.Replace(string(data), old, filepath.Join(t.TempDir(), "from-another-machine"), 1))
				if err := os.WriteFile(marker, data, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "records", "store-id"), []byte("fedcba9876543210fedcba9876543210\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "another store at that path":
				if err := os.WriteFile(filepath.Join(root, "records", "store-id"), []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(marker)
			if notice := relinkMovedProject(moved, false); notice != "" {
				t.Fatalf("not reconnected: %q", notice)
			}
			if after, _ := os.ReadFile(marker); string(after) != string(before) {
				t.Fatal("the marker is not repaired")
			}
			if active, _ := projectActive(root, moved); active {
				t.Fatal("it stays inactive")
			}
		})
	}
}

// The conversations, memory deliveries and memory reviews written before 0.9.7 under the old path's name move with the project when it is
// reconnected, even when an older file from a still earlier path sits beside them (so "the only old file" cannot tell which is this project's).
func TestReconnectingKeepsConversationsWrittenBefore097(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	old := feedbackScope(repo)
	dir := filepath.Join(old, ".shikii", "records", "feedback")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"id":"0000000000000abc","request":"fix the readme","summary":"fixed","reply":"thanks","at":"2026-10-01T00:00:00Z"}` + "\n"
	for _, scope := range []string{old, "/an/even/older/path"} {
		if err := os.WriteFile(filepath.Join(dir, projectID(scope)+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	moved := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	if relinkMovedProject(moved, false) == "" {
		t.Fatal("precondition: reconnected")
	}
	if got, err := (store.Store{Dir: filepath.Join(moved, ".shikii")}).RecentFeedback(moved); err != nil || len(got) != 1 {
		t.Fatalf("the conversations written under the old path are kept: %+v %v", got, err)
	}
}

// 0.9.7 real-agent check: Codex told the person about the move only in a message during the turn, and neither Claude Code nor Codex told
// the person about a copy at all (the copy notice read as background). Both notices ask for the final reply; the copy notice comes once per
// session (the copy is not a project, so nothing is written into its store).
func TestMoveAndCopyNoticesReachThePerson(t *testing.T) {
	_, _, moved := movedProject(t)
	if n := relinkMovedProject(moved, false); !strings.Contains(n, "in your final reply") {
		t.Fatalf("the move notice asks for the final reply: %q", n)
	}
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", feedbackScope(repo), copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	copied = feedbackScope(copied)
	t.Chdir(copied)
	ask := func(session string) string {
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": copied, "prompt": "README \u306e\u8aa4\u5b57\u3092\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002"}))
	}
	if n := ask("s1"); !strings.Contains(n, "copied from") || !strings.Contains(n, "in your final reply") {
		t.Fatalf("the copy notice asks for the final reply: %q", n)
	}
	if n := ask("s1"); n != "" {
		t.Fatalf("once per session: %q", n)
	}
	if n := ask("s2"); !strings.Contains(n, "copied from") {
		t.Fatalf("again in another session: %q", n)
	}
	if entries, _ := os.ReadDir(filepath.Join(copied, ".shikii", "sessions")); len(entries) != 0 {
		t.Fatalf("nothing is written into the copy's store: %d session files", len(entries))
	}
}

// 0.9.7 second real-agent check: after the copy notice, both agents asked the person again at the end of every later reply while no answer
// came. The notice asks once.
func TestCopyNoticeAsksOnce(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	copied := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", feedbackScope(repo), copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	if n := copiedNotice(copied); !strings.Contains(n, "ask once") {
		t.Fatalf("the copy notice asks the person once: %q", n)
	}
}
