package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The audit of the 0.9.9 changes (v0.9.8..34451f3, .shikii/handoff/plan-0.9.9/audit.md). Each test failed on 34451f3.

// finding 1: uninstall signals the page and looked at its lock at once, while the stopping serve still held it, so the lock stayed (50 of 50
// tries in the audit). It now waits a little for the lock to be let go.
func TestPatch099AuditUninstallWaitsForTheStoppedPagesLock(t *testing.T) {
	b := newUninstallBox(t)
	f, err := os.OpenFile(pageLockPath(b.agent, b.repo), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	stop := stopProcess
	stopProcess = func(pid int) error { // the serve exits a moment after the signal
		go func() { time.Sleep(300 * time.Millisecond); f.Close() }()
		return stop(pid)
	}
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if _, err := os.Lstat(pageLockPath(b.agent, b.repo)); !os.IsNotExist(err) {
		t.Fatalf("the lock of the page uninstall stopped goes once the serve lets it go (%v)\n%s", err, out)
	}
}

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

// finding 5: uninstall removed the skill lock without looking whether an init held it.
func TestPatch099AuditHeldSkillLockStays(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".shikii-skill.lock")
	f, _ := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	removeEmptyLock(p)
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		t.Fatal("a skill lock an init still holds is not removed")
	}
	f.Close()
	removeEmptyLock(p)
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatal("control: a lock nobody holds is removed")
	}
}
