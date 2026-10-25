package main

import (
	"os"
	"path/filepath"
	"testing"
)

// An update from 0.6.0 is run by the old binary, which finally calls the new binary's `hook install --yes`. The old binary has no reset inside update, so
// the wipe across 0.7.0 is decided by the new binary's first hook install using the shared mark (~/.shikii/agent/version):
// if there is no mark (0.6.0 or earlier) or it is before 0.7.0, wipe the inside of each project's .shikii/, initialize it again, and set the mark to the current version. The second time does not wipe.
func TestFirstHookInstallAfter070WipesRegisteredProjectsOnce(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	if len(registeredProjects(common)) != 2 { // 1 initialized by hookEnv + this repo
		t.Fatal("init registers the project")
	}
	// hookEnv's hook install wrote the mark, so go back to the state of 0.6.0 or earlier (no mark, project.json at version 1)
	if err := os.Remove(installedVersionPath(common)); err != nil {
		t.Fatal("the new binary's hook install writes the mark: ", err)
	}
	for _, p := range registeredProjects(common) {
		os.WriteFile(filepath.Join(p.Path, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+p.Path+`"}`), 0o600)
	}
	store := filepath.Join(repo, ".shikii")
	leftover := filepath.Join(store, "records", "prompt-kind.jsonl")
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(leftover, "old")
	var reinit []string
	run := func(to string) bool {
		v, _ := updateVersion(to)
		var wiped bool
		capture(t, func() error {
			var err error
			wiped, err = resetStoresOnFirstRun(common, v, func(path string) error {
				reinit = append(reinit, path)
				prev, _ := os.Getwd()
				if err := os.Chdir(path); err != nil {
					return err
				}
				defer os.Chdir(prev)
				return cmdInit(nil)
			})
			return err
		})
		return wiped
	}

	// No mark (from 0.6.0 or earlier) → wipe and initialize again; the mark becomes 0.7.0
	if !run("0.7.0") {
		t.Fatal("with no mark, wipe as an update from 0.6.0 or earlier")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("old records are gone")
	}
	if _, err := os.Stat(filepath.Join(store, "project.json")); err != nil {
		t.Fatal("initialize again after wiping: ", err)
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); string(b) != "0.7.0\n" {
		t.Fatalf("set the mark to the current version: %q", b)
	}
	if len(reinit) != 2 || (reinit[0] != repo && reinit[1] != repo) {
		t.Fatalf("initialize each registered project again once: %v", reinit)
	}

	// Again with the same version (hook install run by hand) → do not wipe
	write(leftover, "new")
	if run("0.7.0") {
		t.Fatal("do not wipe if the mark is the current version")
	}
	if b, _ := os.ReadFile(leftover); string(b) != "new" {
		t.Fatal("the second time keeps the records")
	}

	// 0.7.0 → 0.7.1 does not cross → do not wipe; only the mark moves forward
	if run("0.7.1") {
		t.Fatal("an update that does not cross 0.7.0 does not wipe")
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); string(b) != "0.7.1\n" {
		t.Fatalf("the mark moves to the new version: %q", b)
	}
	if b, _ := os.ReadFile(leftover); string(b) != "new" {
		t.Fatal("an update that does not cross keeps the records")
	}

	// Mark is 0.6.0 (in case an old binary someday wrote a mark) → projects with a mark from 0.7.3 or earlier (version 1) are wiped
	for _, p := range registeredProjects(common) {
		write(filepath.Join(p.Path, ".shikii", "project.json"), `{"version":1,"scope":"`+p.Path+`"}`)
	}
	write(installedVersionPath(common), "0.6.0\n")
	if !run("0.7.0") {
		t.Fatal("wipe if the mark is before 0.7.0")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("if the mark is old, the records are gone")
	}
}
