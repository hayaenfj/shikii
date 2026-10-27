package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The audit of the 0.9.9 changes (v0.9.8..34451f3, .shikii/handoff/plan-0.9.9/audit.md). Each test failed on 34451f3.

// finding 2: a broken JSON with "//" outside a string (a URL without quotes) was called JSONC, and init went on as if the file were fine.
// Only a file that is valid JSON once its comments are taken out is JSONC; a broken one is refused as broken.
func TestPatch099AuditBrokenJSONIsNotCalledJSONC(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(p, []byte(`{"$schema": https://opencode.ai/config.json, "mcp": {}}`), 0o644)
	_, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
	if err == nil || strings.Contains(err.Error(), "comments") || !strings.Contains(err.Error(), "broken settings") {
		t.Fatalf("a broken JSON is refused as broken, not as comments: %v", err)
	}
}

// finding 3: the person added shikii by hand to an opencode.json with comments, as init told them, and init kept telling them to add it.
func TestPatch099AuditJSONCWithShikiiAlreadyThereIsKept(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	text := "{\n // mine\n \"mcp\": {\"shikii\": {\"type\": \"local\", \"command\": [\"shikii\", \"mcp\"]}}\n}\n"
	os.WriteFile(p, []byte(text), 0o644)
	did, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
	if err != nil || did {
		t.Fatalf("shikii is already there: kept, nothing to do: %v %v", did, err)
	}
	if b, _ := os.ReadFile(p); string(b) != text {
		t.Fatalf("and the file is left as written: %q", b)
	}
}

// finding 4: on a disk without hard links PlaceOnce makes the file with O_EXCL and then writes it. Another process that read the empty file in
// between took it for an id an earlier version left empty, removed it and put its own: two processes held two different ids. A file is
// made again only when it is old enough to be left over; a file being written is not replaced.
func TestPatch099AuditStoreIDBeingWrittenIsNotReplaced(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { store.Link = os.Link })
	root := filepath.Join(t.TempDir(), ".shikii")
	os.MkdirAll(filepath.Join(root, "records"), 0o700)
	path := filepath.Join(root, "records", "store-id")
	a, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // A: made, not yet written
	if err != nil {
		t.Fatal(err)
	}
	aID := strings.Repeat("a", 32)
	bID := storeID(root, true) // B, in between
	a.WriteString(aID + "\n")
	a.Close()
	if got := storeID(root, false); got != aID || (bID != "" && bID != aID) {
		t.Fatalf("the first writer wins: A=%s B=%s file=%s", aID, bID, got)
	}
}

// The confirmation audit of the audit's fixes (7d7646c, 02e3feb). The comments in JSON broke twice, so the hand-made reader was replaced
// by a JSONC reader (hujson), and removing lock files raced with a holder twice, so uninstall no longer removes them (an empty lock file is
// harmless and the next holder uses it).

// confirmation i-1: a JSONC opencode.json without shikii made uninstall fail with "shikii is still there".
func TestPatch099ConfirmUninstallJSONCWithoutShikiiHasNothingToRemove(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(p, []byte("{\n // mine\n \"mcp\": {\"other\": {\"type\": \"local\"}}\n}\n"), 0o644)
	if found, err := removeJSONEntry(p, "mcp", "shikii", false); err != nil || found {
		t.Fatalf("no shikii in a file with comments: nothing to remove: %v %v", found, err)
	}
}

// confirmation i-2: OpenCode's JSONC also allows trailing commas; with comments and a trailing comma init stopped as if it were broken.
func TestPatch099ConfirmJSONCWithTrailingCommaLetsInitGoOn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "opencode.json")
	text := "{\n // mine\n \"mcp\": {\"other\": {\"type\": \"local\"},},\n}\n"
	os.WriteFile(p, []byte(text), 0o644)
	_, err := addJSONEntry(p, "mcp", "shikii", map[string]any{"type": "local"})
	if !errors.As(err, new(*personFileError)) || !strings.Contains(err.Error(), "comments") {
		t.Fatalf("JSONC with a trailing comma is left as written and init goes on: %v", err)
	}
}

// confirmation u-1: uninstall removed a lock file between a holder's open and its flock, and two holders then held two files. Lock files
// are not removed; uninstall still removes the page record.
func TestPatch099ConfirmUninstallLeavesLockFiles(t *testing.T) {
	b := newUninstallBox(t)
	locks := []string{filepath.Join(b.home, ".claude", "skills", ".shikii-skill.lock"), pageLockPath(b.agent, b.repo)}
	writeFileT(t, locks[1], "")
	capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	for _, l := range locks {
		if _, err := os.Stat(l); err != nil {
			t.Fatalf("a lock file stays (removing it races with a holder): %s %v", l, err)
		}
	}
	if _, err := os.Lstat(pagePath(b.agent, b.repo)); !os.IsNotExist(err) {
		t.Fatalf("the stopped page's record still goes: %v", err)
	}
}

// confirmation u-2: a valid store-id that could not be opened for a moment (mode 000) was taken for an invalid one and replaced.
func TestPatch099ConfirmUnreadableValidStoreIDIsKept(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	os.MkdirAll(filepath.Join(root, "records"), 0o700)
	path := filepath.Join(root, "records", "store-id")
	id := strings.Repeat("b", 32)
	os.WriteFile(path, []byte(id+"\n"), 0o600)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(path, old, old)
	os.Chmod(path, 0)
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	storeID(root, true)
	os.Chmod(path, 0o600)
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != id {
		t.Fatalf("a valid id that could not be read for a moment is kept: %q", b)
	}
}

// confirmation u-3: an empty id whose time is in the future (copied from another machine, a clock set back) was never made again.
func TestPatch099ConfirmFutureEmptyStoreIDIsMadeAgain(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".shikii")
	os.MkdirAll(filepath.Join(root, "records"), 0o700)
	path := filepath.Join(root, "records", "store-id")
	os.WriteFile(path, nil, 0o600)
	future := time.Now().Add(24 * time.Hour)
	os.Chtimes(path, future, future)
	if id := storeID(root, true); id == "" {
		t.Fatal("an empty id with a future time is a leftover: it is made again")
	}
}
