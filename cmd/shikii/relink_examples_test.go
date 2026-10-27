package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Properties C1 to C4 of reconnecting a moved project (.shikii/handoff/audit-0.9.7/invariants.md), one named example per way a person
// handles project folders on one machine: move (mv), copy (cp -R), bring in a .shikii from elsewhere, make a request (the hook) and
// shikii --init. Each runs in a Git repository and in a plain folder (there the project is the folder itself, 0.9.7 full audit 1-1).
//   - C1: a store this machine initialized, moved and then asked for, is active at its new path.
//   - C2/C3: a copy, or a .shikii brought in, becomes active only with the person's shikii --init.
//   - C4: the shared index never names a store whose id or folder differs.
// They replace a test that ran random sequences of these steps 250 times: it caught some breakages only after 114 runs.

func relinkEachPlace(t *testing.T, run func(t *testing.T, common, p string)) {
	for _, place := range []string{"git", "plain folder"} {
		t.Run(place, func(t *testing.T) {
			p := t.TempDir()
			if place == "git" {
				p = gitRepo(t)
			}
			hookEnv(t, nil, p)
			common, _ := agentHome()
			run(t, common, feedbackScope(p))
		})
	}
}

func relinkMove(t *testing.T, from string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	return feedbackScope(to)
}

func relinkCopy(t *testing.T, from, name string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("cp", "-R", from, to).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	return feedbackScope(to)
}

func relinkActive(f string) bool {
	active, _ := projectActive(filepath.Join(f, ".shikii"), f)
	return active
}

func relinkInit(t *testing.T, f string) string {
	t.Helper()
	t.Chdir(f)
	return capture(t, func() error { return cmdInit(nil) })
}

// relinkIndexNamesItsStores checks C4: every entry of the shared index whose folder is an active project names that folder's store.
func relinkIndexNamesItsStores(t *testing.T, common string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, file := range files {
		b, _ := os.ReadFile(file)
		var p projectEntry
		if json.Unmarshal(b, &p) != nil {
			continue
		}
		if _, err := os.Stat(p.Path); err != nil {
			continue // a path no longer there is not listed (registeredProjects skips it)
		}
		root := filepath.Join(p.Path, ".shikii")
		if relinkActive(p.Path) && (p.StoreID != storeID(root, false) || p.StoreDir != storeDir(root)) {
			t.Fatalf("C4: the index entry for %s names another store: %+v (store id %s, folder %s)", p.Path, p, storeID(root, false), storeDir(root))
		}
	}
}

// C1: mv, then a request in the new folder: the hook reconnects it and it is active there.
func TestRelinkMovedFolderIsActiveAfterARequest(t *testing.T) {
	relinkEachPlace(t, func(t *testing.T, common, p string) {
		r := relinkMove(t, p)
		if n := relinkMovedProject(r, false); !strings.HasPrefix(n, "shikii:") {
			t.Fatalf("C1: the request does not say it reconnected the moved folder: %q", n)
		}
		if !relinkActive(r) {
			t.Fatal("C1: the moved folder, initialized here and asked for, is not active")
		}
		relinkIndexNamesItsStores(t, common)
	})
}

// C2: cp -R, then a request in the copy while the original is still in place: the copy stays inactive and the original keeps its entry.
func TestRelinkCopyStaysInactiveAfterARequest(t *testing.T) {
	relinkEachPlace(t, func(t *testing.T, common, p string) {
		q := relinkCopy(t, p, "copy")
		if n := relinkMovedProject(q, false); n != "" {
			t.Fatalf("C2: the request reconnected a copy: %q", n)
		}
		if relinkActive(q) {
			t.Fatal("C2: the copy became active without the person's shikii --init")
		}
		if !relinkActive(p) {
			t.Fatal("C2: the original is not active any more")
		}
		relinkIndexNamesItsStores(t, common)
	})
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
