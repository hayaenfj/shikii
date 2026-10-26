package store

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func auditELines(t *testing.T, path string) int {
	b, _ := os.ReadFile(path)
	return bytes.Count(b, []byte("\n"))
}

func auditESetup(t *testing.T) (Store, string, string, string) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Dir, "records", "feedback")
	from := filepath.Join(dir, scopeKey(scope)+".jsonl")
	fixed := filepath.Join(dir, projectName+".jsonl")
	auditDAppend(t, from, auditCLine("0000000000000002")+auditCLine("0000000000000003"))
	s.RecentFeedback(scope)
	if n := auditELines(t, fixed); n != 3 {
		t.Fatalf("precondition: 3 lines: %d", n)
	}
	return s, scope, from, fixed
}

// 0.9.7 audit E-4: an old-format mark (offset only, written by 9b77d07) re-appended every line once.
func TestReauditEOldFormatMarkDuplicates(t *testing.T) {
	s, scope, from, fixed := auditESetup(t)
	st, _ := os.Stat(from)
	os.WriteFile(from+".merged", []byte(strconv.FormatInt(st.Size(), 10)), 0o600)
	s.RecentFeedback(scope)
	s.RecentFeedback(scope)
	if n := auditELines(t, fixed); n != 3 {
		t.Errorf("old-format mark: lines in fixed %d (want 3)", n)
	}
}

// 0.9.7 audit E-3: a copied .shikii (new inodes) re-appended every merged line.
func TestReauditECopiedStoreDuplicates(t *testing.T) {
	s, scope, _, _ := auditESetup(t)
	dst := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-R", s.Dir, dst).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	c := Store{Dir: dst}
	c.RecentFeedback(scope)
	c.RecentFeedback(scope)
	if n := auditELines(t, filepath.Join(dst, "records", "feedback", projectName+".jsonl")); n != 3 {
		t.Errorf("copied store: lines in fixed %d (want 3)", n)
	}
}

// 0.9.7 audit E-2: the older file removed and made again, longer than the old offset (and on ext4 or tmpfs often with the same inode):
// the new file's first lines were skipped. The mark holds a hash of the bytes before the offset, so another file starts from its beginning.
func TestReauditERemadeLongerFileKeepsEveryLine(t *testing.T) {
	s, scope, from, fixed := auditESetup(t)
	os.Remove(from)
	auditDAppend(t, from, auditCLine("0000000000000004")+auditCLine("0000000000000005")+auditCLine("0000000000000006"))
	s.RecentFeedback(scope)
	if n := auditELines(t, fixed); n != 6 {
		t.Errorf("remade older file: lines in fixed %d (want 6)", n)
	}
}

// 0.9.7 audit F-4: a mark in the form 855865f wrote ("<device>:<inode> <offset>") was not read as an offset, so every merged line was
// appended once more. It is taken as an offset, like the number-only form.
func TestReauditFInodeMarkIsTakenAsAnOffset(t *testing.T) {
	s, scope, from, fixed := auditESetup(t)
	st, _ := os.Stat(from)
	os.WriteFile(from+".merged", []byte("16777231:12345 "+strconv.FormatInt(st.Size(), 10)), 0o600)
	s.RecentFeedback(scope)
	s.RecentFeedback(scope)
	if n := auditELines(t, fixed); n != 3 {
		t.Errorf("a mark of 855865f: lines in fixed %d (want 3)", n)
	}
}
