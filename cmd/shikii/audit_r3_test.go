package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins down the findings of audit round 3 (2026-10-23).

// 1. Even if --init is redone with a new binary on a store with a mark from 0.7.3 or earlier (version 1), do not write version 2 while the wipe across 0.7.0 is still pending (so the wipe is never skipped forever).
func TestReinitDoesNotMarkAnOldStoreDoneWhileTheWipeIsPending(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	os.WriteFile(filepath.Join(root, "records", "old.jsonl"), []byte("old"), 0o600)
	os.Remove(installedVersionPath(common))
	initAt(t, repo) // redo --init with the new binary (the README steps)
	if v := projectInitVersion(root); v != 1 {
		t.Fatalf("a store whose wipe is not done stays at version 1: %d", v)
	}
	v, _ := updateVersion("0.7.5")
	wiped, err := resetStoresOnFirstRun(common, v, func(path string) error { initAt(t, path); return nil })
	if err != nil || !wiped {
		t.Fatalf("the following hook install wipes it: wiped=%v err=%v", wiped, err)
	}
	if _, err := os.Stat(filepath.Join(root, "records", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatal("old records are gone")
	}
	if v := projectInitVersion(root); v != 2 {
		t.Fatalf("after wiping, version 2: %d", v)
	}
}

// 3. Stray files such as .DS_Store do not make the wipe refuse (treated as strays that may be wiped).
func TestWipeTreatsFinderJunkAsDisposable(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, ".project-init-123"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	plan := planWipe([]projectEntry{{Path: repo, InitVersion: 1}})
	if len(plan) != 1 || plan[0].Action != wipeDo {
		t.Fatalf("not refused for .DS_Store and init leftovers: %+v", plan)
	}
	if err := wipeProjectStores([]projectEntry{{Path: repo}}, func(string) error { return nil }); err != nil {
		t.Fatalf("can be wiped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatal("the strays are gone too")
	}
}

// 4. The shared-store check compares the actual folders (the same even with a different spelling).
func TestSharedPlaceComparesByIdentity(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	upper := strings.ToUpper(home)
	if _, err := os.Stat(upper); err != nil || upper == home {
		t.Skip("case-sensitive FS")
	}
	if !isSharedPlaceScope(upper) {
		t.Fatal("HOME is a shared store even with a different spelling")
	}
	if !isSharedPlaceScope(filepath.Join(home, "sub", "..")) {
		t.Fatal("HOME/sub/.. is also HOME")
	}
	if isSharedPlaceScope(filepath.Join(home, "sub")) {
		t.Fatal("a folder under HOME is not a shared store")
	}
}
