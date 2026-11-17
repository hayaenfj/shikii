package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The audit of 0.37.0. Walking up finds only a project of this machine: the folder shikii init prepares from there, or a project
// registered here. The hooks, the CLI and the MCP server name the same project for one folder.

// handMark is a project.json written by hand for scope: no shikii init ran there, so this machine has no entry for it.
func handMark(t *testing.T, scope string) []byte {
	t.Helper()
	b, err := json.Marshal(projectActivation{Version: projectInitVersionNow, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// storeTextHits lists the files of the store at root that hold text.
func storeTextHits(root, text string) []string {
	var hits []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := os.ReadFile(path); err == nil && bytes.Contains(b, []byte(text)) {
			hits = append(hits, strings.TrimPrefix(path, root))
		}
		return nil
	})
	return hits
}

// shikii init below a store that is an inactive copy leaves that store as it is and prepares the folder it runs in.
func TestInitBelowACopiedStoreLeavesTheStoreAboveAlone(t *testing.T) {
	for _, git := range []bool{false, true} {
		hookEnv(t, nil)
		orig := feedbackScope(t.TempDir())
		t.Chdir(orig)
		capture(t, func() error { return cmdInit(nil) })
		outer := feedbackScope(t.TempDir())
		if out, err := exec.Command("cp", "-R", filepath.Join(orig, ".shikii"), filepath.Join(outer, ".shikii")).CombinedOutput(); err != nil {
			t.Fatalf("cp: %v %s", err, out)
		}
		mark := filepath.Join(outer, ".shikii", "project.json")
		id := filepath.Join(outer, ".shikii", "records", "store-id")
		markBefore, _ := os.ReadFile(mark)
		idBefore, errBefore := os.ReadFile(id)
		if active, _ := projectActive(filepath.Join(outer, ".shikii"), outer); active {
			t.Fatalf("the copy is not active before the init")
		}
		inner := filepath.Join(outer, "tree")
		if err := os.MkdirAll(inner, 0o755); err != nil {
			t.Fatal(err)
		}
		if git {
			if out, err := exec.Command("git", "-C", inner, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
		}
		t.Chdir(inner)
		out := capture(t, func() error { return cmdInit(nil) })
		markAfter, _ := os.ReadFile(mark)
		idAfter, errAfter := os.ReadFile(id)
		if !bytes.Equal(markBefore, markAfter) {
			t.Errorf("git=%v: init in %s rewrote the project.json of the store above:\nbefore %s\nafter  %s\ninit said:\n%s", git, inner, markBefore, markAfter, out)
		}
		if !bytes.Equal(idBefore, idAfter) || (errBefore == nil) != (errAfter == nil) {
			t.Errorf("git=%v: init in %s changed records/store-id of the store above: before %q (%v), after %q (%v)", git, inner, idBefore, errBefore, idAfter, errAfter)
		}
		if active, _ := projectActive(filepath.Join(outer, ".shikii"), outer); active {
			t.Errorf("git=%v: init in %s activated the copied store above", git, inner)
		}
		if got := projectScopeAt(inner); got != feedbackScope(inner) {
			t.Errorf("git=%v: the folder init ran in is the project: got %q", git, got)
		}
		if active, err := projectActive(filepath.Join(inner, ".shikii"), feedbackScope(inner)); !active {
			t.Errorf("git=%v: the folder init ran in is initialized: %v", git, err)
		}
	}
}

// initializedBelowAGitTop prepares a folder initialized outside git whose parent then became a git repository: the top of the
// working tree is not initialized, the folder below it is.
func initializedBelowAGitTop(t *testing.T) (top, sub string) {
	t.Helper()
	top = feedbackScope(t.TempDir())
	sub = filepath.Join(top, "notes")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	capture(t, func() error { return cmdInit(nil) })
	if out, err := exec.Command("git", "-C", top, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(top, ".gitignore"), []byte(".shikii/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, "README.md"), []byte("# top\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, top, "init")
	return top, sub
}

// The project the hooks key their records with is the project the CLI uses, inside git as outside it: a session in an initialized
// folder below the top of a git working tree is that folder's, its own request is not refused as another project's, and guard try
// there checks the project's own tree.
func TestHooksAndCLINameTheSameProjectBelowAGitTop(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	_, sub := initializedBelowAGitTop(t)
	cli := projectScopeAt(sub)
	if cli != sub {
		t.Fatalf("the nearest folder initialized on this machine is the project: got %q, want %q", cli, sub)
	}
	if _, _, _, _, hook := treeStateWithScope(sub); hook != cli {
		t.Errorf("the hooks' scope is %q, the CLI's is %q", hook, cli)
	}
	root := filepath.Join(sub, ".shikii")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": sub, "prompt": "add a note to the README"})
	s, err := readSession(root, "n")
	if err != nil || s.TurnID == "" {
		t.Fatalf("the session is in the project's store: %+v %v", s, err)
	}
	if s.Scope != cli {
		t.Errorf("the session in the project's store names another project: session scope %q, project %q", s.Scope, cli)
	}
	if err := currentPlanningFrame(s, s.TurnID, cli); err != nil && strings.Contains(err.Error(), "another project folder") {
		t.Errorf("the CLI in the same folder refuses this session's own request: %v", err)
	}
	writeGuard(t, root, guard{ID: "no-token", Kind: guardKindForbiddenWords, Title: "no forbidden-token", Words: []string{"forbidden-token"}})
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("forbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out := capture(t, func() error { return cmdGuard([]string{"try"}) })
	if strings.Contains(out, "since it left") {
		t.Errorf("guard try in the project's own folder says it is a worktree apart from the project:\n%s", out)
	}
	if !strings.Contains(out, "STOP no-token") {
		t.Errorf("guard try sees the change:\n%s", out)
	}
}

// The home folder is never the project of a folder below it, also when ~/.shikii holds a project.json an old version wrote.
func TestHomeFolderIsNeverTheProjectOfAFolderBelow(t *testing.T) {
	hookEnv(t, nil)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home = feedbackScope(home)
	if err := os.WriteFile(filepath.Join(home, ".shikii", "project.json"), handMark(t, home), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(home, "work", "x")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectScopeAt(work); got != work {
		t.Errorf("a folder under home with a project.json in ~/.shikii: got %q, want %q", got, work)
	}
	if _, _, _, _, got := treeStateWithScope(work); got != work {
		t.Errorf("the hooks' scope under home: got %q, want %q", got, work)
	}
	if _, _, err := prepareHookProject(work); !errors.Is(err, errProjectInactive) {
		t.Errorf("the hooks stay off under home: %v", err)
	}
}

// A .shikii that is a link or a file, and a project.json that is a link or a folder, make no project for the folders below, also
// when the link leads to a project registered on this machine.
func TestLinkedStoreOrMarkAboveIsNotAProject(t *testing.T) {
	hookEnv(t, nil)
	real := feedbackScope(t.TempDir())
	t.Chdir(real)
	capture(t, func() error { return cmdInit(nil) })
	below := func(dir string) string {
		sub := filepath.Join(dir, "x", "y")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		return sub
	}
	a := feedbackScope(t.TempDir()) // .shikii is a link to a real store
	if err := os.Symlink(filepath.Join(real, ".shikii"), filepath.Join(a, ".shikii")); err != nil {
		t.Fatal(err)
	}
	b := feedbackScope(t.TempDir()) // project.json is a link to a real one
	if err := os.Mkdir(filepath.Join(b, ".shikii"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, ".shikii", "project.json"), filepath.Join(b, ".shikii", "project.json")); err != nil {
		t.Fatal(err)
	}
	c := feedbackScope(t.TempDir()) // project.json is a folder
	if err := os.MkdirAll(filepath.Join(c, ".shikii", "project.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := feedbackScope(t.TempDir()) // .shikii is a file
	if err := os.WriteFile(filepath.Join(d, ".shikii"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"linked .shikii": a, "linked project.json": b, "project.json is a folder": c, ".shikii is a file": d} {
		sub := below(dir)
		if got := projectScopeAt(sub); got != sub {
			t.Errorf("%s: a folder below it is its own place: got %q, want %q", name, got, sub)
		}
		if _, _, _, _, got := treeStateWithScope(sub); got != sub {
			t.Errorf("%s: the hooks' scope: got %q, want %q", name, got, sub)
		}
	}
}

// guard try in a worktree kept under the project, at the edges: from a subfolder, names outside ASCII and with spaces, the
// project's branch moved on, a detached HEAD on either side, the project's own tree from a subfolder, a repository with no commit.
func TestGuardTryInAWorktreeAtTheEdges(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-token", Kind: guardKindForbiddenWords, Title: "no forbidden-token", Words: []string{"forbidden-token"}})
	try := func() string { return capture(t, func() error { return cmdGuard([]string{"try"}) }) }
	note := "\u30e1\u30e2.txt" // a name outside ASCII
	if err := os.WriteFile(filepath.Join(tree, note), []byte("forbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, tree, "note")
	docs := filepath.Join(tree, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a b.txt"), []byte("forbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(docs)
	if out := try(); !strings.Contains(out, "since it left") || !strings.Contains(out, note) || !strings.Contains(out, "a b.txt") {
		t.Errorf("from a subfolder of the worktree, names outside ASCII and with spaces:\n%s", out)
	}
	// the project's branch moved on, with the word in its own commit: that is not the worktree's change
	if err := os.WriteFile(filepath.Join(repo, "main-only.txt"), []byte("forbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "the project's branch moves on")
	if out := try(); strings.Contains(out, "main-only.txt") || !strings.Contains(out, note) {
		t.Errorf("after the project's branch moved on:\n%s", out)
	}
	if out, err := exec.Command("git", "-C", tree, "checkout", "-q", "--detach").CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v %s", err, out)
	}
	if out := try(); !strings.Contains(out, "since it left") || !strings.Contains(out, note) {
		t.Errorf("detached HEAD in the worktree:\n%s", out)
	}
	// the project's own tree, from a subfolder: compared with HEAD, and the worktree's change is not seen
	pkg := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "x.txt"), []byte("forbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(pkg)
	if out := try(); strings.Contains(out, "since it left") || !strings.Contains(out, "STOP no-token") || !strings.Contains(out, "pkg/x.txt") || strings.Contains(out, note) {
		t.Errorf("the project's own tree from a subfolder:\n%s", out)
	}
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "--detach").CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v %s", err, out)
	}
	t.Chdir(tree)
	if out := try(); !strings.Contains(out, "since it left") || !strings.Contains(out, note) || strings.Contains(out, "main-only.txt") {
		t.Errorf("detached HEAD in the project's own tree:\n%s", out)
	}
	fresh := filepath.Join(repo, "vendor", "fresh")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", fresh, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(fresh, "n.txt"), []byte("forbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(fresh)
	if out := try(); strings.Contains(out, "since it left") || !strings.Contains(out, "STOP no-token") || !strings.Contains(out, "n.txt") {
		t.Errorf("a repository with no commit under the project:\n%s", out)
	}
}

// A store nobody initialized with shikii on this machine, sitting in a folder above (a place others can write to, such as /tmp),
// gets no record of a session in a folder below it: not the request, not in a file made beforehand that everyone can read, and it
// is not registered as a project of this machine.
func TestStorePlacedAboveByHandGetsNoRecordOfTheSession(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	shared := feedbackScope(t.TempDir())
	planted := filepath.Join(shared, ".shikii")
	for _, dir := range []string{planted, filepath.Join(planted, "records")} {
		if err := os.Mkdir(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(planted, "project.json"), handMark(t, shared), 0o644); err != nil {
		t.Fatal(err)
	}
	open := filepath.Join(planted, "records", "prompt-kind.jsonl")
	if err := os.WriteFile(open, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	agent, _ := agentHome()
	registered := func() int { m, _ := filepath.Glob(filepath.Join(agent, "projects", "*.json")); return len(m) }
	before := registered()
	repo := filepath.Join(shared, "someone", "work") // a git repository nobody initialized
	plain := filepath.Join(shared, "someone", "notes")
	for _, dir := range []string{repo, plain} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for i, work := range []string{repo, plain} {
		if got := projectScopeAt(work); got != work {
			t.Errorf("a folder below a store placed by hand is its own place: got %q, want %q", got, work)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": []string{"v", "w"}[i], "cwd": work, "prompt": "the plan is tangerine-4471"})
	}
	if hits := storeTextHits(planted, "tangerine-4471"); len(hits) > 0 {
		t.Errorf("the request was written into the store placed above: %v", hits)
	}
	if after := registered(); after != before {
		t.Errorf("the store placed by hand was registered as a project of this machine: %d -> %d entries", before, after)
	}
}

// The guards of a store placed above by hand do not steer a session in a folder below that nobody initialized.
func TestStorePlacedAboveByHandDoesNotSteerTheSession(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	shared := feedbackScope(t.TempDir())
	planted := filepath.Join(shared, ".shikii")
	if err := os.Mkdir(planted, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "project.json"), handMark(t, shared), 0o644); err != nil {
		t.Fatal(err)
	}
	writeGuard(t, planted, guard{ID: "planted", Kind: guardKindForbiddenWords, Title: "text from the store placed above reaches the agent", Words: []string{"hello"}})
	work := filepath.Join(shared, "someone", "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", work, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, work, "init")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "v", "cwd": work, "prompt": "add a greeting to the README"})
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# work\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "v", "cwd": work, "last_assistant_message": "added"})
	if strings.Contains(out, "planted") || strings.Contains(out, `"block"`) {
		t.Errorf("the Stop of a session in a folder nobody initialized carries the text of the store placed above: %s", out)
	}
	t.Chdir(work)
	if out := capture(t, func() error { return cmdGuard([]string{"try"}) }); strings.Contains(out, "planted") {
		t.Errorf("guard try in that folder applies the guard of the store placed above:\n%s", out)
	}
}

// A project.json that names its folder by a relative path is not an initialization made at this path: a store brought in from
// elsewhere stays off until shikii init runs there.
func TestRelativeScopeInProjectJSONDoesNotActivate(t *testing.T) {
	dir := feedbackScope(t.TempDir())
	if err := os.Mkdir(filepath.Join(dir, ".shikii"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".shikii", "project.json"), []byte(`{"version":2,"scope":"."}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if active, err := projectActive(filepath.Join(dir, ".shikii"), dir); active {
		t.Errorf("a project.json with scope \".\" is taken as initialized here (%v)", err)
	}
	// the absolute path is an initialization made here
	if err := os.WriteFile(filepath.Join(dir, ".shikii", "project.json"), handMark(t, dir), 0o644); err != nil {
		t.Fatal(err)
	}
	if active, err := projectActive(filepath.Join(dir, ".shikii"), dir); !active {
		t.Errorf("a project.json naming this folder by its absolute path is initialized here: %v", err)
	}
}

// Below a project, a folder holding a copied (inactive) store: the CLI, the MCP server and the hooks name the project, as before
// the walk upward existed.
func TestCLIMCPAndHooksNameTheSameProjectBelowACopiedStore(t *testing.T) {
	hookEnv(t, nil)
	repo, _ := nestedWorktree(t)
	other := feedbackScope(t.TempDir())
	t.Chdir(other)
	capture(t, func() error { return cmdInit(nil) })
	inner := filepath.Join(feedbackScope(repo), "vendor", "copy")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-R", filepath.Join(other, ".shikii"), filepath.Join(inner, ".shikii")).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	want := filepath.Join(feedbackScope(repo), ".shikii")
	cli, _, cerr := projectStoreAt(inner)
	mcp, _, merr := mcpProjectStore(inner)
	hook, _, herr := prepareHookProject(inner)
	if cli != want || mcp != want || hook != want || cerr != nil || merr != nil || herr != nil {
		t.Errorf("want %q everywhere: the CLI uses %q (%v), the MCP server %q (%v), the hooks %q (%v)", want, cli, cerr, mcp, merr, hook, herr)
	}
}

// shikii handoff write names the written file by a path that is there from where the person is: the short form in the project's
// folder, the whole path from a worktree kept under the project.
func TestHandoffWriteNamesAPathThatIsThere(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	whole := filepath.Join(projectScopeAt(repo), ".shikii", "handoff", "w", "a.md")
	t.Chdir(tree)
	withStdin(t, "from the worktree\n", func() {
		out := capture(t, func() error { return cmdHandoff([]string{"write", "w/a.md"}) })
		if !strings.Contains(out, "wrote: "+whole+" ") {
			t.Errorf("from a worktree under the project, the whole path: %s", out)
		}
	})
	if b, err := os.ReadFile(whole); err != nil || string(b) != "from the worktree\n" {
		t.Fatalf("the record is in the project's store: %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(tree, ".shikii")); !os.IsNotExist(err) {
		t.Fatalf("the worktree gets no store of its own: %v", err)
	}
	t.Chdir(repo)
	withStdin(t, "from the project\n", func() {
		out := capture(t, func() error { return cmdHandoff([]string{"write", "w/a.md"}) })
		if !strings.Contains(out, "wrote: .shikii/handoff/w/a.md ") {
			t.Errorf("in the project's folder, the short form: %s", out)
		}
	})
}

// After a project's folder was moved, this machine's entry still names the old path, so a folder below (here another repository
// kept under the project) is its own place. Once a hook ran in the project's folder and reconnected it, the folder below finds
// the project again.
func TestMovedProjectIsFoundFromBelowAfterItReconnects(t *testing.T) {
	_, old, moved := movedProject(t)
	inner := filepath.Join(moved, "vendor", "tool")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", inner, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if got := projectScopeAt(inner); got != inner {
		t.Fatalf("right after the move this machine's entry names %s, so the folder below is its own place: got %q", old, got)
	}
	if _, _, err := prepareHookProject(inner); !errors.Is(err, errProjectInactive) {
		t.Fatalf("the hooks are off in the folder below until the project reconnects: %v", err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": moved, "prompt": "fix a typo in the README"})
	if got := projectScopeAt(inner); got != moved {
		t.Errorf("after a hook ran in the project's folder, the folder below finds the project: got %q, want %q", got, moved)
	}
	if root, _, err := prepareHookProject(inner); err != nil || root != filepath.Join(moved, ".shikii") {
		t.Errorf("and the hooks there use the project's store: %q %v", root, err)
	}
}

// On a disk that does not tell upper case from lower case (the macOS default), the project's folder and a folder below it reached
// by a path that differs only in case find the project, spelled as its project.json records it, inside git and outside it. The
// hooks and the CLI use that one spelling, so the CLI takes the request the hooks recorded.
func TestProjectIsFoundFromBelowByAPathInAnotherCase(t *testing.T) {
	for _, git := range []bool{false, true} {
		model := fakeAgentModel(t, func(string) float64 { return .1 })
		base := feedbackScope(t.TempDir())
		p := filepath.Join(base, "Proj")
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		lower := filepath.Join(base, "proj")
		if _, err := os.Stat(lower); err != nil {
			t.Skip("this disk tells upper case from lower case")
		}
		if git {
			if out, err := exec.Command("git", "-C", p, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
		}
		hookEnv(t, model, p)
		sub := filepath.Join(lower, "a", "b")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, cwd := range []string{lower, sub} {
			if got := projectScopeAt(cwd); got != p {
				t.Errorf("git=%v: from %s the CLI's project is %q, want %q", git, cwd, got, p)
			}
			if _, _, _, _, hook := treeStateWithScope(cwd); hook != p {
				t.Errorf("git=%v: from %s the hooks' project is %q, want %q", git, cwd, hook, p)
			}
			if root, _, err := prepareHookProject(cwd); err != nil || root != filepath.Join(p, ".shikii") {
				t.Errorf("git=%v: from %s the hooks use the project's store: %q %v", git, cwd, root, err)
			}
		}
		for i, cwd := range []string{lower, sub} {
			session := []string{"at-the-project", "below-it"}[i]
			runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": cwd, "prompt": "add a note to the README"})
			s, err := readSession(filepath.Join(p, ".shikii"), session)
			if err != nil || s.TurnID == "" {
				t.Fatalf("git=%v: the session of a request from %s is in the project's store: %+v %v", git, cwd, s, err)
			}
			if s.Scope != p {
				t.Errorf("git=%v: the session of a request from %s names another project: %q, want %q", git, cwd, s.Scope, p)
			}
			if err := currentPlanningFrame(s, s.TurnID, projectScopeAt(cwd)); err != nil && strings.Contains(err.Error(), "another project folder") {
				t.Errorf("git=%v: the CLI in %s refuses this session's own request: %v", git, cwd, err)
			}
		}
	}
}

// An entry an older version wrote by the path (no store id) for a project that is gone does not vouch for a store placed at that
// path later: walking up takes a registered project only by its store's own id, which the index holds in the person's home.
func TestEntryByThePathDoesNotVouchForAStorePlacedThereLater(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	shared := feedbackScope(t.TempDir())
	common, _ := agentHome()
	entry, _ := json.Marshal(projectEntry{ID: projectID(shared), Name: filepath.Base(shared), Path: shared, URL: "/p/" + projectID(shared) + "/"})
	if err := os.WriteFile(filepath.Join(common, "projects", projectID(shared)+".json"), entry, 0o600); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(shared, ".shikii") // placed there later, by hand
	if err := os.Mkdir(planted, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "project.json"), handMark(t, shared), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(shared, "someone", "notes")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectScopeAt(plain); got != plain {
		t.Errorf("a folder below a store placed by hand: got %q, want %q", got, plain)
	}
	if _, _, _, _, got := treeStateWithScope(plain); got != plain {
		t.Errorf("the hooks' project there: got %q, want %q", got, plain)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "v", "cwd": plain, "prompt": "the plan is tangerine-4471"})
	if hits := storeTextHits(planted, "tangerine-4471"); len(hits) > 0 {
		t.Errorf("the request was written into the store placed above: %v", hits)
	}
}

// The home folder is not the project of a folder below it also when this machine's index names the store in ~/.shikii by its own
// id (an older version ran there): only the condition on the shared place keeps it out.
func TestHomeFolderRegisteredByItsStoreIDIsNotTheProjectOfAFolderBelow(t *testing.T) {
	hookEnv(t, nil)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home = feedbackScope(home)
	root := filepath.Join(home, ".shikii")
	if err := os.WriteFile(filepath.Join(root, "project.json"), handMark(t, home), 0o600); err != nil {
		t.Fatal(err)
	}
	id := storeID(root, true)
	if id == "" {
		t.Fatal("the store in ~/.shikii gets an id")
	}
	common, _ := agentHome()
	entry, _ := json.Marshal(projectEntry{ID: projectID(home), Name: filepath.Base(home), Path: home, URL: "/p/" + projectID(home) + "/", StoreID: id, StoreDir: storeDir(root)})
	if err := os.WriteFile(filepath.Join(common, "projects", id+".json"), entry, 0o600); err != nil {
		t.Fatal(err)
	}
	if !registeredProjectAt(common, home) {
		t.Fatal("the setup: the home folder is registered by its store's id")
	}
	work := filepath.Join(home, "work", "x")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectScopeAt(work); got != work {
		t.Errorf("a folder under a registered home: got %q, want %q", got, work)
	}
	if _, _, _, _, got := treeStateWithScope(work); got != work {
		t.Errorf("the hooks' project under a registered home: got %q, want %q", got, work)
	}
	if _, _, err := prepareHookProject(work); !errors.Is(err, errProjectInactive) {
		t.Errorf("the hooks stay off under a registered home: %v", err)
	}
}

// A worktree under the project whose .shikii is a link to another project's store: the link is passed over, and the CLI, the MCP
// server and the hooks name the project the worktree is kept under. Only the condition on a real folder keeps the link out.
func TestLinkedStoreAtAWorktreeTopBelowTheProjectIsPassedOver(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	project := projectScopeAt(repo)
	other := feedbackScope(t.TempDir())
	t.Chdir(other)
	capture(t, func() error { return cmdInit(nil) })
	if err := os.Symlink(filepath.Join(other, ".shikii"), filepath.Join(tree, ".shikii")); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, ".shikii")
	cli, _, cerr := projectStoreAt(tree)
	mcp, _, merr := mcpProjectStore(tree)
	hook, _, herr := prepareHookProject(tree)
	_, _, _, _, scope := treeStateWithScope(tree)
	if cli != want || mcp != want || hook != want || scope != project || cerr != nil || merr != nil || herr != nil {
		t.Errorf("want %q: the CLI uses %q (%v), the MCP server %q (%v), the hooks %q (%v), the hooks' project %q", want, cli, cerr, mcp, merr, hook, herr, scope)
	}
}

// The MCP server finds the project of a folder as the CLI and the hooks do. A store placed later at a path that only an entry of
// an older version names (no store id), and the home folder registered by its store's id, are not used for a folder below them:
// the call is refused and nothing is written there.
func TestMCPDoesNotUseAStoreTheIndexDoesNotVouchForBelowIt(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	shared := feedbackScope(t.TempDir())
	entry, _ := json.Marshal(projectEntry{ID: projectID(shared), Name: filepath.Base(shared), Path: shared, URL: "/p/" + projectID(shared) + "/"})
	if err := os.WriteFile(filepath.Join(common, "projects", projectID(shared)+".json"), entry, 0o600); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(shared, ".shikii") // placed there later, by hand
	if err := os.Mkdir(planted, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "project.json"), handMark(t, shared), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(shared, "someone", "notes")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if root, scope, err := mcpProjectStore(plain); err == nil {
		t.Errorf("the MCP server uses the store placed above for a folder below it: store %q, project %q (the CLI says %q)", root, scope, projectScopeAt(plain))
	}
	args, _ := json.Marshal(map[string]any{"operation": "handoff_write", "path": "zz/a.md", "content": "tangerine-4471\n", "project": plain})
	params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(args)})
	reply, _ := json.Marshal(handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, &mcpRouter{}))
	if hits := storeTextHits(planted, "tangerine-4471"); len(hits) > 0 {
		t.Errorf("a tool call naming the folder below wrote into the store placed above: %v (reply %.300s)", hits, reply)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home = feedbackScope(home)
	root := filepath.Join(home, ".shikii")
	if err := os.WriteFile(filepath.Join(root, "project.json"), handMark(t, home), 0o600); err != nil {
		t.Fatal(err)
	}
	id := storeID(root, true)
	entry, _ = json.Marshal(projectEntry{ID: projectID(home), Name: filepath.Base(home), Path: home, URL: "/p/" + projectID(home) + "/", StoreID: id, StoreDir: storeDir(root)})
	if err := os.WriteFile(filepath.Join(common, "projects", id+".json"), entry, 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(home, "work", "x")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if mcp, scope, err := mcpProjectStore(work); err == nil {
		t.Errorf("the MCP server uses the home folder's store for a folder below it: store %q, project %q (the CLI says %q)", mcp, scope, projectScopeAt(work))
	}
}

// The MCP server still reaches the project from a worktree kept under it whose top holds a copy of the project's store (a project
// that commits .shikii/project.json to git): the copy is passed over and the registered project above is used.
func TestMCPUsesTheRegisteredProjectAboveACopiedStoreAtAWorktreeTop(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	tree = feedbackScope(tree)
	if out, err := exec.Command("cp", "-R", filepath.Join(repo, ".shikii"), filepath.Join(tree, ".shikii")).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	want := filepath.Join(feedbackScope(repo), ".shikii")
	if mcp, scope, err := mcpProjectStore(tree); err != nil || mcp != want {
		t.Errorf("the MCP server names the project's store %q for the worktree: %q, project %q (%v)", want, mcp, scope, err)
	}
	docs := filepath.Join(tree, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if mcp, _, err := mcpProjectStore(docs); err != nil || mcp != want {
		t.Errorf("and for a folder in the worktree: %q (%v)", mcp, err)
	}
}

// records/store-id of a store placed in a folder above is a FIFO whose writer never writes. The walk reads the id of every
// initialized store it passes, so it reads only a regular file and does not wait.
func TestStoreIDThatIsAFIFODoesNotHoldTheWalk(t *testing.T) {
	hookEnv(t, nil)
	shared := feedbackScope(t.TempDir())
	planted := filepath.Join(shared, ".shikii")
	if err := os.MkdirAll(filepath.Join(planted, "records"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "project.json"), handMark(t, shared), 0o644); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(planted, "records", "store-id")
	if err := syscall.Mkfifo(fifo, 0o666); err != nil {
		t.Fatal(err)
	}
	w, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	plain := filepath.Join(shared, "someone", "notes")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() { done <- projectScopeAt(plain) }()
	select {
	case got := <-done:
		if got != plain {
			t.Errorf("a folder below: got %q, want %q", got, plain)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the walk from %s is still waiting after 5s on the FIFO placed as records/store-id of the store above", plain)
	}
	if _, err := w.WriteString("0123456789abcdef0123456789abcdef\n"); err != nil {
		t.Fatal(err)
	}
	if id := storeID(planted, false); id != "" {
		t.Errorf("an id written into a FIFO is read as the store's id: %q", id)
	}
}

// An entry with a store id and no store folder (an older form) does not vouch for a store placed later at the project's path by
// someone who knows the id: a folder below is not handed to it, and the request is written into none.
func TestEntryWithoutStoreFolderDoesNotVouchForAStorePlacedThere(t *testing.T) {
	hookEnv(t, fakeAgentModel(t, func(string) float64 { return .1 }))
	gone := feedbackScope(t.TempDir())
	t.Chdir(gone)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(gone, ".shikii")
	id := storeID(root, false)
	common, _ := agentHome()
	entryPath := filepath.Join(common, "projects", id+".json")
	e, ok := readEntry(entryPath)
	if !ok || e.StoreDir == "" {
		t.Fatalf("the setup: registered by its id with its store folder: %+v %v", e, ok)
	}
	e.StoreDir = ""
	b, _ := json.Marshal(e)
	if err := os.WriteFile(entryPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(gone, ".shikii")
	if err := os.MkdirAll(filepath.Join(planted, "records"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "project.json"), handMark(t, gone), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "records", "store-id"), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(gone, "someone", "notes")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectScopeAt(plain); got != plain {
		t.Errorf("the CLI's project of a folder below: got %q, want %q", got, plain)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "zz", "cwd": plain, "prompt": "the plan is tangerine-4471"})
	if hits := storeTextHits(planted, "tangerine-4471"); len(hits) > 0 {
		t.Errorf("the request was written into the store placed there: %v", hits)
	}
}

// After a project's folder is moved and a link to the new place is left at the old path, one request in the moved folder
// reconnects it: the record names the new path, and a folder below finds the project.
func TestMovedProjectWithALinkLeftAtTheOldPathIsFoundFromBelow(t *testing.T) {
	_, old, moved := movedProject(t)
	if err := os.Symlink(moved, old); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(moved, "vendor", "tool")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", inner, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": moved, "prompt": "fix a typo in the README"})
	if got := projectScopeAt(inner); !sameFolder(got, moved) {
		t.Errorf("after one request in the moved folder, a folder below finds %q, want the project %q", got, moved)
	}
	if hook := treeProjectScope(moved, moved); projectScopeAt(moved) != hook {
		t.Errorf("in the moved folder the CLI says %q and the hooks %q", projectScopeAt(moved), hook)
	}
}
