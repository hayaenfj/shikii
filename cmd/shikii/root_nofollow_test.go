package main

import (
	"os"
	"path/filepath"
	"testing"
)

// os.Root.OpenFile follows a link in the last name even with O_NOFOLLOW, so a session record or a lock placed as a link to another
// file in the same folder used to be read or locked through it. Each opener below must refuse the link instead.

func sessionsWithALink(t *testing.T, name, target, text string) *os.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, target), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestReadPlanningSessionRefusesARecordThatIsALink(t *testing.T) {
	r := sessionsWithALink(t, "a.json", "b.json", "{}")
	if s, err := readPlanningSession(r, "a.json"); err == nil {
		t.Fatalf("read the session record through a link: %+v", s)
	}
}

func TestLockPlanningSessionRefusesALockThatIsALink(t *testing.T) {
	r := sessionsWithALink(t, "a.json.lock", "b.json", "{}")
	unlock, err := lockPlanningSession(r, "a.json")
	if err == nil {
		unlock()
		t.Fatal("took the session lock through a link")
	}
}

func TestWorkLockRefusesALockThatIsALink(t *testing.T) {
	r := sessionsWithALink(t, "a.json.work.lock", "b.json", "{}")
	if unlock, err := workLock(r, "a.json"); err == nil {
		unlock()
		t.Fatal("took the work lock through a link")
	}
}
