package notebook

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
)

type hintedReadError struct{ err error }

func (r hintedReadError) Read(p []byte) (int, error) {
	return copy(p, "partial"), r.err
}

func TestReadPageBytesTreatSizeAsHint(t *testing.T) {
	for _, n := range []int{0, 1, 2048, 64 << 10, (64 << 10) + 20} {
		data := bytes.Repeat([]byte("x"), n)
		for _, hint := range []int64{-1, 0, 2, int64(n), 1 << 62} {
			got, err := readPageBytes(bytes.NewReader(data), hint)
			if !bytes.Equal(got, data[:min(n, (64<<10)+1)]) || (err != nil) != (n > 64<<10) {
				t.Fatalf("size %d hint %d must preserve all bounded bytes: got=%d err=%v", n, hint, len(got), err)
			}
		}
	}
	got, err := readPageBytes(iotest.OneByteReader(strings.NewReader("complete bytes")), 1)
	if err != nil || string(got) != "complete bytes" {
		t.Fatal("short reads cannot truncate the content", string(got), err)
	}
	wantErr := errors.New("synthetic reader failure")
	got, err = readPageBytes(hintedReadError{wantErr}, 0)
	if !errors.Is(err, wantErr) || string(got) != "partial" {
		t.Fatal("partial data and the original read error must survive", string(got), err)
	}
	if _, err = readPageBytes(iotest.ErrReader(io.ErrUnexpectedEOF), 5); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("a failed empty read must remain a failure", err)
	}
}

// bytesPerOp is the heap bytes one call of run allocates, counted as testing.Benchmark counts its B/op (the growth of the runtime's total
// allocated bytes over the calls, divided by their number) but over a fixed number of calls: testing.Benchmark keeps calling for
// a second to settle on a count, which made each allocation budget below take over a second. A call that is not counted comes first,
// for what a first call sets up and later calls reuse. Choose runs large enough that what the runtime allocates by itself in that time
// is small against the budget the result is held to.
func bytesPerOp(runs int, run func()) uint64 {
	run()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		run()
	}
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

func TestReadFileAllocationBudget(t *testing.T) {
	want := bytes.Repeat([]byte("abcdefgh"), 2048)
	path := filepath.Join(t.TempDir(), "page.md")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	measure := func(read func(string) ([]byte, error)) uint64 {
		return bytesPerOp(200, func() {
			got, err := read(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("all file bytes must survive", err)
			}
		})
	}
	// Compare under the same instrumentation, including a race build.
	before, after := measure(originalReadFile), measure(readFile)
	if after*10 > before*9 {
		t.Fatalf("reading 16 KiB must allocate at least 10%% less than before: old=%d new=%d", before, after)
	}
}

func originalReadFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("a wiki page must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err == nil && len(data) > 64<<10 {
		err = errors.New("wiki page exceeds the size limit")
	}
	return data, err
}
