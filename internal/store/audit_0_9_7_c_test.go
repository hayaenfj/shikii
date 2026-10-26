package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func auditCLine(id string) string {
	return `{"id":"` + id + `","request":"x","summary":"y","reply":"z","at":"2026-10-01T00:00:00Z"}` + "\n"
}

// 0.9.7 re-audit C-2: a line an older shikii writes through a file it opened before the merge was lost with the file set aside.
func TestReauditCMergeLosesLineOfOpenWriter(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(s.Dir, "records", "feedback", scopeKey(scope)+".jsonl")
	if err := os.WriteFile(from, []byte(auditCLine("0000000000000002")), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.OpenFile(from, os.O_APPEND|os.O_WRONLY, 0) // the older shikii opened it
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecentFeedback(scope); err != nil { // this shikii merges
		t.Fatal(err)
	}
	if _, err := old.Write([]byte(auditCLine("0000000000000003"))); err != nil { // the older shikii writes
		t.Fatal(err)
	}
	old.Close()
	got, err := s.RecentFeedback(scope)
	if err != nil || len(got) != 3 {
		t.Fatalf("the third line the older shikii wrote is kept: %d %v", len(got), err)
	}
}

// 0.9.7 re-audit C-3: a line read while it was being written (no newline yet) was sealed as a broken line in the fixed name.
func TestReauditCMergeTornTail(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(s.Dir, "records", "feedback", scopeKey(scope)+".jsonl")
	line := auditCLine("0000000000000002")
	if err := os.WriteFile(from, []byte(line[:40]), 0o600); err != nil { // half written by the older shikii
		t.Fatal(err)
	}
	if _, err := s.RecentFeedback(scope); err != nil {
		t.Fatal(err)
	}
	fixed := filepath.Join(s.Dir, "records", "feedback", "project.jsonl")
	b, _ := os.ReadFile(fixed)
	if want := 2; len(splitLines(b)) >= want && string(splitLines(b)[1]) == line[:40] {
		t.Fatalf("a half-written line was sealed in the fixed name: %q", b)
	}
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	return out
}

// 0.9.7 re-audit C-5: in a folder (memory-reviews), a file whose name was already in the fixed folder kept the hash-named folder forever.
func TestReauditCMergeFolderLeavesSameName(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	dir := filepath.Join(s.Dir, "records", "memory-reviews")
	fixed := filepath.Join(dir, "project")
	from := filepath.Join(dir, scopeKey(scope))
	for _, d := range []string{fixed, from} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(fixed, "a.json"), []byte(`{"new":1}`), 0o600)
	os.WriteFile(filepath.Join(from, "a.json"), []byte(`{"old":1}`), 0o600)
	_ = s.memoryAssessmentPath(scope, "x", "y")
	if _, err := os.Lstat(from); err == nil {
		t.Fatalf("the hash-named folder is not left behind")
	}
}

// 0.9.7 re-audit S4: while an older shikii (before 0.9.7) still runs, it locks records/memories/<sha256(path)>.lock and this one locked
// project.lock, so the two did not exclude each other and the same delivery could be saved twice. The older name's lock, when there is one,
// is taken too.
func TestMemoryLockAlsoTakesTheOlderNamesLock(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	dir := filepath.Join(s.Dir, "records", "memories")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old, err := os.OpenFile(filepath.Join(dir, scopeKey(scope)+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := syscall.Flock(int(old.Fd()), syscall.LOCK_EX); err != nil { // the older shikii holds its lock
		t.Fatal(err)
	}
	if unlock, err := s.memoryLock(scope); err == nil {
		unlock()
		t.Fatal("the memory record is busy while the older shikii holds its lock")
	}
	_ = syscall.Flock(int(old.Fd()), syscall.LOCK_UN)
	unlock, err := s.memoryLock(scope)
	if err != nil {
		t.Fatalf("free again once it is released: %v", err)
	}
	unlock()
}
