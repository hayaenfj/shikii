package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.7 audit B-1: cp -R A B, then mv A C. B carries the same marker and store id as the original. If B's hook ran first, B took the
// registration and the real project C stayed stopped without a word. Only the store that was registered (the same folder, moved) is
// reconnected; the copy is told that shikii cannot tell, and stays inactive.
func TestAuditCopyThenMoveOriginal(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	original := feedbackScope(repo)
	base := t.TempDir()
	copied := filepath.Join(base, "copy")
	if out, err := exec.Command("cp", "-R", original, copied).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	moved := filepath.Join(base, "moved")
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	copied, moved = feedbackScope(copied), feedbackScope(moved)
	if n := relinkMovedProject(copied, false); n != "" {
		t.Fatalf("the copy is not reconnected as the project: %q", n)
	}
	if n := copiedNotice(copied); !strings.Contains(n, "shikii --init") {
		t.Fatalf("the copy is told how to make it a project of its own: %q", n)
	}
	if n := relinkMovedProject(moved, false); !strings.Contains(n, "moved or renamed") {
		t.Fatalf("the real project is reconnected: %q", n)
	}
	if active, _ := projectActive(filepath.Join(moved, ".shikii"), moved); !active {
		t.Fatal("the real project is active")
	}
	if active, _ := projectActive(filepath.Join(copied, ".shikii"), copied); active {
		t.Fatal("the copy stays inactive")
	}
	// shikii --init in the copy makes it a project of its own, with its own store id (so it never matches the original's entry)
	t.Chdir(copied)
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "a project of its own") {
		t.Fatalf("init says the copy became a project of its own: %s", out)
	}
	if a, b := storeID(filepath.Join(copied, ".shikii"), false), storeID(filepath.Join(moved, ".shikii"), false); a == "" || a == b {
		t.Fatalf("the copy has its own store id: %q %q", a, b)
	}
	if active, _ := projectActive(filepath.Join(moved, ".shikii"), moved); !active {
		t.Fatal("the original stays active")
	}
}

// 0.9.7 audit B-2: an index entry written before 0.9.7 has no store id. A .shikii brought in from elsewhere without a store id, naming the
// same path, matched it ("" equals "") and switched itself on. An entry without a store id never reconnects by itself.
func TestAuditForeignLegacyStoreActivatesItself(t *testing.T) {
	_, old, _ := movedProject(t)
	common, _ := agentHome()
	entry := filepath.Join(common, "projects", projectID(old)+".json")
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(data, &p)
	delete(p, "store_id") // the shape 0.9.6 wrote
	delete(p, "store_dir")
	data, _ = json.Marshal(p)
	if err := os.WriteFile(entry, data, 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := feedbackScope(gitRepo(t)) // a 0.9.6 .shikii from another machine, naming the same path
	root := filepath.Join(foreign, ".shikii")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(projectActivation{Version: 2, Scope: old})
	if err := os.WriteFile(filepath.Join(root, "project.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	notice := relinkMovedProject(foreign, false)
	if active, _ := projectActive(root, foreign); active || notice != "" {
		t.Fatalf("a .shikii brought in switched itself on without shikii --init: notice=%q active=%v", notice, active)
	}
	if n := copiedNotice(foreign); !strings.Contains(n, "cannot confirm") || !strings.Contains(n, "shikii --init") {
		t.Fatalf("the agent is told that shikii is not running and why: %q", n)
	}
}
