package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// asIf096 turns the store at moved (moved from old) into what 0.9.6 left: no records/store-name, no records/store-id, and the shared
// index has only the path-named entry 0.9.6 writes (no store_id). The wiki stays under sha256(old), where 0.9.6 wrote it.
func asIf096(t *testing.T, common, old, moved string) {
	t.Helper()
	root := filepath.Join(moved, ".shikii")
	for _, f := range []string{"store-name", "store-id"} {
		if err := os.Remove(filepath.Join(root, "records", f)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, f := range files {
		os.Remove(f)
	}
	legacy, _ := json.Marshal(map[string]any{"id": projectID(old), "name": filepath.Base(old), "path": old, "url": "/p/" + projectID(old) + "/"})
	if err := os.WriteFile(filepath.Join(common, "projects", projectID(old)+".json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	// precondition, checked without the code under test: the page 0.9.6 wrote is under sha256(old)
	sum := sha256.Sum256([]byte(old))
	if entries, err := os.ReadDir(filepath.Join(root, "wiki", "lessons", hex.EncodeToString(sum[:]))); err != nil || len(entries) == 0 {
		t.Fatalf("precondition: the wiki is under sha256(old): %v", err)
	}
}

// Final audit BC-1 (B2): a store 0.9.6 wrote is moved before 0.9.7 ever opened it. One `shikii wiki query` at the new path before --init
// (or an MCP query on the store it started in) recorded the store's name as the hash of the new path; the --init after it could not change
// it, and the wiki 0.9.6 wrote was no longer found. The name now comes from the path in project.json.
func TestFinalBC1WikiQueryBeforeInitHidesThe096Wiki(t *testing.T) {
	for _, query := range []bool{false, true} { // false is the control: --init alone keeps the 0.9.6 wiki
		t.Run(map[bool]string{false: "control-init-only", true: "query-then-init"}[query], func(t *testing.T) {
			finalBC1(t, query)
		})
	}
}

func finalBC1(t *testing.T, query bool) {
	common, old, moved := movedProject(t)
	asIf096(t, common, old, moved)
	t.Chdir(moved)
	if query {
		capture(t, func() error { return cmdNotebook([]string{"query"}) }) // e.g. the skill's "query the wiki first", before --init
	}
	capture(t, func() error { return cmdInit(nil) }) // the person's --init, as the notice asks
	sum := sha256.Sum256([]byte(old))
	recorded, _ := os.ReadFile(filepath.Join(moved, ".shikii", "records", "store-name"))
	live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live()
	if strings.TrimSpace(string(recorded)) != hex.EncodeToString(sum[:]) || err != nil || len(live) != 1 {
		t.Fatalf("B2: a store 0.9.6 wrote is read as it is after a move: store-name=%q (want sha256(old)=%s), %d pages found %v",
			strings.TrimSpace(string(recorded)), hex.EncodeToString(sum[:]), len(live), err)
	}
}

// Final audit BC-2 (C1, decided 2026-10-27): a store 0.9.6 initialized on this machine, moved before the update converted the index (G1
// covers a move after it). The index entry 0.9.6 wrote has no store id or store folder, so the move cannot be told from a copy whose
// original is gone: the hook does not take it over, and says shikii is not running there. The person's --init then keeps the wiki 0.9.6
// wrote.
func TestFinalBC2MovedBy096StoreIsNotReconnected(t *testing.T) {
	common, old, moved := movedProject(t)
	asIf096(t, common, old, moved)
	if note := relinkMovedProject(moved, false); note != "" {
		t.Fatalf("not taken over by itself: %q", note)
	}
	if notice := copiedNotice(moved); !strings.Contains(notice, "--init") {
		t.Fatalf("the person is told how to keep using it: %q", notice)
	}
	t.Chdir(moved)
	capture(t, func() error { return cmdInit(nil) })
	if active, _ := projectActive(filepath.Join(moved, ".shikii"), moved); !active {
		t.Fatal("--init activates it")
	}
	if live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live(); err != nil || len(live) != 1 {
		t.Fatalf("and keeps the wiki 0.9.6 wrote: %d pages %v", len(live), err)
	}
}
