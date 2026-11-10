package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGCProtectsLabelSnapshotAndRecordRewrite(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	old := Record{TraceID: "old", SpecID: "s", At: time.Unix(1, 0)}
	if err := s.AppendRecord(old); err != nil {
		t.Fatal(err)
	}
	// Ensure both directories exist before probing the writers' locks.
	if err := s.AppendLabel(Label{TraceID: "other", SpecID: "s", SpecHash: "h", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	open := openInRoot
	beforeReplace := beforeGCReplace
	t.Cleanup(func() { openInRoot, beforeGCReplace = open, beforeReplace })
	checked := false
	labelLanded := false
	lateRecordLanded := false
	probe := func(replacing bool) {
		for _, path := range []string{s.labelsPath("s"), s.recordsPath("s")} {
			lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
				if !replacing && path == s.labelsPath("s") {
					if err := s.AppendLabel(Label{TraceID: old.TraceID, SpecID: "s", SpecHash: "h", Model: "m"}); err != nil {
						t.Fatal(err)
					}
					labelLanded = true
				}
				if replacing && path == s.recordsPath("s") {
					if err := s.AppendRecord(Record{TraceID: "late", SpecID: "s", At: time.Unix(3, 0)}); err != nil {
						t.Fatal(err)
					}
					lateRecordLanded = true
				}
				t.Errorf("GC must exclude the writer through snapshot and rewrite: replacing=%v path=%s", replacing, path)
			} else if !errors.Is(err, syscall.EWOULDBLOCK) {
				t.Errorf("unexpected lock error: %v", err)
			}
			lock.Close()
		}
	}
	openInRoot = func(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
		if name == "s.jsonl" && flags&syscall.O_ACCMODE == os.O_RDONLY && !checked && root.Name() == filepath.Join(s.Dir, "records") {
			// Labels are read first; fix the interleaving after their snapshot.
			checked = true
			probe(false)
		}
		return open(root, name, flags, mode)
	}
	replaced := false
	beforeGCReplace = func() {
		replaced = true
		probe(true)
	}
	if _, err := s.GC("s", time.Unix(2, 0), true); err != nil {
		t.Fatal(err)
	}
	openInRoot = open
	if !checked || !replaced {
		t.Fatal("record read and replacement must both be checked")
	}
	if labelLanded {
		if _, err := s.FindRecord("s", old.TraceID); err != nil {
			t.Errorf("a record labeled before the record scan was lost: %v", err)
		}
	}
	if lateRecordLanded {
		if _, err := s.FindRecord("s", "late"); err != nil {
			t.Errorf("a record appended before replacement was lost: %v", err)
		}
	}
	// Locks must be released after GC, allowing both writers to proceed.
	if err := s.AppendRecord(Record{TraceID: "new", SpecID: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLabel(Label{TraceID: "new", SpecID: "s", SpecHash: "h", Model: "m"}); err != nil {
		t.Fatal(err)
	}
}
