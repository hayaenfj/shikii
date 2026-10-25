package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins down the wipe-related findings of audit round 2 (2026-10-23).

func initAt(t *testing.T, dir string) {
	t.Helper()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)
	capture(t, func() error { return cmdInit(nil) })
}

// 1. Even if one wipe is refused and the mark cannot be written, a project that was wiped and initialized again is not wiped twice on the next run (judged by each project's mark).
// 2. --init writes the mark from 0.7.4 on (version 2), and writes the current version if there is no shared mark, so the order go install → --init → hook install --yes does not wipe either.
func TestWipeMarksEachProjectDoneAndInitWritesTheMarkers(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	os.Remove(installedVersionPath(common))
	os.RemoveAll(filepath.Join(common, "projects")) // remove what hookEnv registered, making this the first --init on this machine
	a := gitRepo(t)
	initAt(t, a)
	if _, err := os.Stat(installedVersionPath(common)); err != nil {
		t.Fatal("--init writes the current version if there is no shared mark (someone who installed with go install is not wiped on the first hook install): ", err)
	}
	if v := projectInitVersion(filepath.Join(a, ".shikii")); v < 2 {
		t.Fatalf("--init writes each project's mark as version 2: %d", v)
	}
	// 2 projects like 0.6.0 (version 1, no shared mark). One has an unknown entry
	os.Remove(installedVersionPath(common))
	b, c := gitRepo(t), gitRepo(t)
	initAt(t, b)
	initAt(t, c)
	for _, d := range []string{b, c} {
		os.WriteFile(filepath.Join(d, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+d+`"}`), 0o600)
		os.WriteFile(filepath.Join(d, ".shikii", "records", "old.jsonl"), []byte("old"), 0o600)
	}
	os.WriteFile(filepath.Join(c, ".shikii", "mystery"), []byte("?"), 0o600)
	os.Remove(installedVersionPath(common))
	v, _ := updateVersion("0.7.4")
	reinit := func(path string) error { initAt(t, path); return nil }
	if _, err := resetStoresOnFirstRun(common, v, reinit); err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("refuse the project with an unknown entry and return the reason: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b, ".shikii", "records", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatal("the one not refused is wiped and initialized again")
	}
	if _, err := os.Stat(installedVersionPath(common)); err == nil {
		t.Fatal("if a failure remains, do not write the shared mark (so the refused one is tried on the next run)")
	}
	os.WriteFile(filepath.Join(b, ".shikii", "records", "after.jsonl"), []byte("new"), 0o600)
	if _, err := resetStoresOnFirstRun(common, v, reinit); err == nil {
		t.Fatal("still a failure, since one is still refused")
	}
	if _, err := os.Stat(filepath.Join(b, ".shikii", "records", "after.jsonl")); err != nil {
		t.Fatal("a project that was initialized again is not wiped twice (it has a version 2 mark): ", err)
	}
	os.Remove(filepath.Join(c, ".shikii", "mystery"))
	if _, err := resetStoresOnFirstRun(common, v, reinit); err != nil {
		t.Fatalf("once tidied, the rest is wiped and it succeeds: %v", err)
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); strings.TrimSpace(string(b)) != "0.7.4" {
		t.Fatalf("when everything is done, write the shared mark: %q", b)
	}
}

// 3. A broken or empty mark is not taken as 0.0.0; stop without wiping.
func TestBrokenInstalledVersionMarkerRefusesToWipe(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	keep := filepath.Join(repo, ".shikii", "records", "keep.jsonl")
	os.WriteFile(keep, []byte("k"), 0o600)
	v, _ := updateVersion("0.7.4")
	for _, broken := range []string{"", "garbage\n", "0.7\n"} {
		os.WriteFile(installedVersionPath(common), []byte(broken), 0o644)
		wiped, err := resetStoresOnFirstRun(common, v, func(string) error { return nil })
		if err == nil || wiped {
			t.Fatalf("mark %q cannot be read, so stop without wiping: wiped=%v err=%v", broken, wiped, err)
		}
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("mark %q: records are intact", broken)
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

// 5. The hook install output (without --yes) shows what is wiped and what is not (unknown entries, version 2) separately.
func TestHookInstallPreviewExplainsWhatItWouldSkip(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	old, odd, fresh := gitRepo(t), gitRepo(t), gitRepo(t)
	for _, d := range []string{old, odd, fresh} {
		initAt(t, d)
	}
	for _, d := range []string{old, odd} {
		os.WriteFile(filepath.Join(d, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+d+`"}`), 0o600)
	}
	os.WriteFile(filepath.Join(odd, ".shikii", "mystery"), []byte("?"), 0o600)
	os.Remove(installedVersionPath(common))
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, old) || !strings.Contains(out, "is removed and initialized again") {
		t.Fatalf("shows what will be wiped:\n%s", out)
	}
	if !strings.Contains(out, odd) || !strings.Contains(out, "not removed: ") || !strings.Contains(out, "mystery") {
		t.Fatalf("shows, with the reason, that a project with unknown entries is not wiped:\n%s", out)
	}
	if !strings.Contains(out, "done: "+fresh) {
		t.Fatalf("a project initialized with this version (version 2) shows as done:\n%s", out)
	}
}

// 6. The shared-store check works on its own too (audit R2 mutation M6: it does not rely on the allowlist). planWipe makes HOME "not wiped".
func TestSharedPlaceIsRefusedByItself(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	if !isSharedPlaceScope(home) || !isSharedPlaceScope(home+string(filepath.Separator)) {
		t.Fatal("HOME is a shared store")
	}
	if isSharedPlaceScope(t.TempDir()) {
		t.Fatal("other folders are not a shared store")
	}
	plan := planWipe([]projectEntry{{Path: home, InitVersion: 1}})
	if len(plan) != 1 || plan[0].Action != wipeRefuse || !strings.Contains(plan[0].Reason, "shared store") {
		t.Fatalf("HOME is refused before looking at the allowlist: %+v", plan)
	}
}
