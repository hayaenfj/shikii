package store

import (
	"os"
	"syscall"
	"testing"
)

// Writing the first records concurrently from the same process into a location with no records/ yet loses none of them (the hook asks 2
// specs in parallel, and wiki insertion points are asked in parallel per article). When two goroutines created the same new file at once,
// darwin's open with create returned ENOENT (2026-10-23) and one record was lost. Checked in one run, without waiting on a race: the open
// of the new record file says ENOENT once, as it did then, and the record is still written.
func TestFirstRecordSurvivesACreateThatSaysNotThere(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	old := openInRoot
	t.Cleanup(func() { openInRoot = old })
	var opens []int
	openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
		opens = append(opens, flag)
		if len(opens) == 1 {
			return nil, &os.PathError{Op: "openat", Path: name, Err: syscall.ENOENT}
		}
		return old(r, name, flag, perm)
	}
	if err := st.AppendRecord(Record{TraceID: "t0", SpecID: "spec-0"}); err != nil {
		t.Fatalf("the first record is written even when the create says ENOENT once: %v", err)
	}
	if len(opens) != 2 || opens[0]&os.O_CREATE == 0 {
		t.Fatalf("one create that says ENOENT, then one that works: %v", opens)
	}
	openInRoot = old
	if recs, err := st.Records("spec-0", 0); err != nil || len(recs) != 1 || recs[0].TraceID != "t0" {
		t.Fatalf("the record is there: %v %v", recs, err)
	}
}

// A read of a record file that is not there is not retried and stays "not there" for the caller (os.IsNotExist).
func TestMissingRecordFileIsNotRetried(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if err := st.AppendRecord(Record{TraceID: "t0", SpecID: "spec-0"}); err != nil {
		t.Fatal(err)
	}
	old := openInRoot
	t.Cleanup(func() { openInRoot = old })
	opens := 0
	openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
		opens++
		return old(r, name, flag, perm)
	}
	if _, err := st.openFile(st.recordsPath("spec-1"), os.O_RDONLY); !os.IsNotExist(err) || opens != 1 {
		t.Fatalf("one open, and the error says the file is not there: %d opens, %v", opens, err)
	}
}
