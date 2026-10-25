package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests that pin down the findings of the audit (2026-10-23, 0.7.3).

// 1. A .shikii/ made by 0.6.0 to 0.7.2 has no guards/. Treat it as registered anyway, and wiping across 0.7.0 still works (missing places are created later).
func TestStoresWithoutGuardsDirStayRegisteredAndAreWiped(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	root := filepath.Join(repo, ".shikii")
	if err := os.RemoveAll(filepath.Join(root, "guards")); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range registeredProjects(common) {
		found = found || p.Path == repo
	}
	if !found {
		t.Fatal("a store without guards/ (from an old version) is also treated as registered")
	}
	leftover := filepath.Join(root, "records", "old.jsonl")
	if err := os.WriteFile(leftover, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600) // the --init mark from 0.7.3 and earlier
	os.Remove(installedVersionPath(common))
	v, _ := updateVersion("0.7.3")
	wiped, err := resetStoresOnFirstRun(common, v, func(path string) error {
		prev, _ := os.Getwd()
		if err := os.Chdir(path); err != nil {
			return err
		}
		defer os.Chdir(prev)
		return cmdInit(nil)
	})
	if err != nil || !wiped {
		t.Fatalf("an old store is also wiped and initialized again: wiped=%v err=%v", wiped, err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("old records are gone")
	}
	if _, err := os.Stat(filepath.Join(root, "guards")); err != nil {
		t.Fatal("initializing again creates guards/")
	}
}

// 2. Do not initialize HOME. Wiping never touches the shared store (the parent of ~/.shikii/agent), does not wipe when there are unknown entries, keeps project.json, and does not stop at one failure.
func TestWipeNeverTouchesTheSharedPlaceAndKeepsUnknownEntries(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	t.Chdir(home)
	if err := cmdInit(nil); err == nil {
		t.Fatal("do not initialize HOME (wiping would take ~/.shikii/agent with it)")
	}
	common, _ := agentHome()
	if err := wipeProjectStores([]projectEntry{{Path: home}}, func(string) error { return nil }); err == nil {
		t.Fatal("do not wipe the parent of the shared store")
	}
	if _, err := os.Stat(filepath.Join(common, "decisions")); err != nil {
		t.Fatal("the shared store is intact: ", err)
	}
	// Do not wipe a project with unknown entries. Do not delete project.json. Move on to the next even after a failure
	known := t.TempDir()
	for _, d := range []string{"decisions", "records", "wiki"} {
		os.MkdirAll(filepath.Join(known, ".shikii", d), 0o700)
	}
	os.WriteFile(filepath.Join(known, ".shikii", "records", "x.jsonl"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(known, ".shikii", "project.json"), []byte(`{"version":1}`), 0o600)
	odd := t.TempDir()
	os.MkdirAll(filepath.Join(odd, ".shikii", "records"), 0o700)
	os.WriteFile(filepath.Join(odd, ".shikii", "shikii_aside"), []byte("?"), 0o600)
	failing := t.TempDir()
	os.MkdirAll(filepath.Join(failing, ".shikii", "records"), 0o700)
	var reinit []string
	err := wipeProjectStores([]projectEntry{{Path: failing}, {Path: odd}, {Path: known}}, func(path string) error {
		reinit = append(reinit, path)
		if path == failing {
			return os.ErrPermission
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), failing) || !strings.Contains(err.Error(), odd) {
		t.Fatalf("report every failed and refused project: %v", err)
	}
	if _, e := os.Stat(filepath.Join(odd, ".shikii", "shikii_aside")); e != nil {
		t.Fatal("a project with unknown entries has nothing wiped")
	}
	if _, e := os.Stat(filepath.Join(known, ".shikii", "records", "x.jsonl")); !os.IsNotExist(e) {
		t.Fatal("known entries are wiped")
	}
	if _, e := os.Stat(filepath.Join(known, ".shikii", "project.json")); e != nil {
		t.Fatal("keep project.json (so a failed re-initialization does not drop it from the registry)")
	}
	if len(reinit) != 2 || reinit[1] != known {
		t.Fatalf("do not stop at one failure; move on to the next: %v", reinit)
	}
	// Do not wipe .shikii if it is a link (audit mutation A)
	linked := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linked, ".shikii")); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(target, "keep"), []byte("k"), 0o600)
	if err := wipeProjectStores([]projectEntry{{Path: linked}}, func(string) error { return nil }); err == nil {
		t.Fatal("a .shikii that is a link is not wiped")
	}
	if _, e := os.Stat(filepath.Join(target, "keep")); e != nil {
		t.Fatal("the link target is intact")
	}
}

// 3. The hook install output (without --yes) shows the projects to be wiped.
func TestHookInstallPreviewListsTheProjectsItWouldWipe(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600) // the --init mark from 0.7.3 and earlier
	os.Remove(installedVersionPath(common))
	// Audit 0.8.1-1 item 15: also show, with a count, that work records (half-written files that cannot be recreated) are wiped
	os.MkdirAll(filepath.Join(repo, ".shikii", "handoff", "job"), 0o700)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "job", "a.md"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "job", "b.md"), []byte("b"), 0o600)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "c.md"), []byte("c"), 0o600) // 2 entries directly under it, 3 files (count files, as the list does)
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, "is removed and initialized again") || !strings.Contains(out, repo) {
		t.Fatalf("the output shows what will be wiped:\n%s", out)
	}
	if !strings.Contains(out, "3 work records (.shikii/handoff/) are removed too") {
		t.Fatalf("show, with a count, that work records are wiped too:\n%s", out)
	}
	if _, err := os.Stat(installedVersionPath(common)); err == nil {
		t.Fatal("showing alone does not write the mark")
	}
	writeInstalledVersion(common, currentVersion())
	if out = capture(t, func() error { return cmdHook([]string{"install"}) }); strings.Contains(out, "消して初期化し直します") {
		t.Fatalf("not shown when no version boundary is crossed:\n%s", out)
	}
}

// 4. commit-body does not look at commits when the current HEAD cannot be reached from the HEAD at request time (switched branches or pulled).
func TestCommitBodyIgnoresCommitsNotMadeInTheTurn(t *testing.T) {
	repo := gitRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	gitCommitAll(t, repo, "base")
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	write("b.txt", "b")
	if out, err := exec.Command("git", "-C", repo, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	old := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "feature: subject only")
	old.Env = append(os.Environ(), "GIT_COMMITTER_DATE=2026-10-21T00:00:00Z", "GIT_AUTHOR_DATE=2026-10-21T00:00:00Z")
	if out, err := old.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "-").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	head, since := gitHead(repo), time.Now()
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "feature").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	in := guardTurnInput(repo, "", nil, false, head, since)
	if len(in.Commits) != 0 {
		t.Fatalf("do not treat existing commits on the switched-to branch as this turn's: %+v", in.Commits)
	}
	// Commits stacked on top of the request's HEAD are looked at
	head, since = gitHead(repo), time.Now()
	write("c.txt", "c")
	gitCommitAll(t, repo, "made in this turn")
	if in = guardTurnInput(repo, "", nil, false, head, since); !in.CommitsKnown || len(in.Commits) != 1 {
		t.Fatalf("this turn's commits are looked at: %+v", in)
	}
}

// 5. A Stop that was blocked does not self-assess scope and completion (do not judge on the changes and reply from before the fix).
func TestBlockedStopDoesNotJudgeTheUnfinishedTurn(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# x\n"), 0o644)
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "j", "cwd": repo, "prompt": "README を直して"})
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# x\n根\n"), 0o644)
	out := runHook(t, "stop", map[string]any{"session_id": "j", "cwd": repo, "last_assistant_message": "直した"})
	if !strings.Contains(out, "block") {
		t.Fatalf("blocks: %s", out)
	}
	if loadSession(root, "j").Judged {
		t.Fatal("a blocked turn is not marked as decided")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "records", "done-as-asked.jsonl")); strings.TrimSpace(string(b)) != "" {
		t.Fatal("do not judge completion on the reply from before the fix")
	}
}

// 6. enable works even when except is a broad glob (the negative example's path uses a name except does not match).
func TestGuardEnableWorksWhenExceptIsBroad(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if err := saveGuard(root, guard{ID: "no-todo", Kind: guardKindForbiddenWords, Title: "Leave no TODO", Words: []string{"TODO"}, Except: []string{"*.txt", "docs/**"}}); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdGuard([]string{"enable", "no-todo"}) })
	if !strings.Contains(out, "RED:") {
		t.Fatalf("blocks on the negative example even when except is broad: %s", out)
	}
}

// 7. Path matching also matches on the name alone (audit mutation B).
func TestMatchPathMatchesByBaseName(t *testing.T) {
	if !matchPath("CHANGELOG.md", "docs/CHANGELOG.md") || matchPath("CHANGELOG.md", "docs/CHANGELOG_ja.md") || !matchPath("docs/**", "docs/a/b.md") || matchPath("docs/**", "src/docs.md") {
		t.Fatal("matching on the name alone and under a folder")
	}
}
