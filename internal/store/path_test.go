package store

import (
	"os"
	"testing"
)

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
