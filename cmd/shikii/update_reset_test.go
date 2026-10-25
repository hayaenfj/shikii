package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// An update to 0.7.0 deletes everything inside each initialized project's .shikii/ and re-initializes it (the owner's decision, 2026-10-23).
// An update that does not cross 0.7.0 deletes nothing.
func TestUpdateResetsProjectStoresWhenCrossing070(t *testing.T) {
	for _, c := range []struct {
		from, to string
		want     bool
	}{
		{"0.6.0", "0.7.0", true}, {"0.5.0", "0.7.2", true}, {"0.6.9", "0.8.0", true},
		{"0.7.0", "0.7.1", false}, {"0.6.0", "0.6.1", false}, {"0.7.1", "0.8.0", false},
	} {
		from, _ := updateVersion(c.from)
		to, _ := updateVersion(c.to)
		if got := crossesStoreReset(from, to); got != c.want {
			t.Errorf("%s → %s: reset=%v, want %v", c.from, c.to, got, c.want)
		}
	}

	project := t.TempDir()
	store := filepath.Join(project, ".shikii")
	for _, p := range []string{"decisions/prompt-kind.yaml", "records/prompt-kind.jsonl", "labels/prompt-kind.jsonl", "wiki/lessons/x/y.md", "project.json", "sessions/a.json"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(store, p)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(store, p), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(project, "README.md")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var reinit []string
	err := wipeProjectStores([]projectEntry{{Path: project}}, func(path string) error {
		reinit = append(reinit, path)
		return os.WriteFile(filepath.Join(path, ".shikii", "project.json"), []byte("new"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(store)
	if len(entries) != 1 || entries[0].Name() != "project.json" {
		t.Fatalf("everything inside .shikii/ must be deleted before re-initializing: %v", entries)
	}
	if b, _ := os.ReadFile(filepath.Join(store, "project.json")); string(b) != "new" {
		t.Fatal("the re-initialized mark must be new")
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Fatal("nothing outside .shikii/ may be touched")
	}
	if len(reinit) != 1 || reinit[0] != project {
		t.Fatalf("each project must be re-initialized exactly once: %v", reinit)
	}

	// In the update flow, reset is called only when crossing the version
	f := fixtureUpdate(t)
	var seen [][2]string
	f.opts.version, f.tag = "0.6.0", "v0.7.0"
	f.files["shikii"] = []byte("#!/bin/sh\nprintf 'shikii 0.7.0\\n'\n") // the downloaded binary reports its own version
	f.opts.reset = func(_ context.Context, _ string, from, to [3]uint64) error {
		seen = append(seen, [2]string{versionString(from), versionString(to)})
		return nil
	}
	if err := runUpdate(context.Background(), f.opts); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != [2]string{"0.6.0", "0.7.0"} {
		t.Fatalf("the update flow must pass the old and new versions to reset: %v", seen)
	}
}
