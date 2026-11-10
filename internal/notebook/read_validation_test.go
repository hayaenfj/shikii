package notebook

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// A commit needs hashes, not another article-sized allocation for every file.

func TestReadValidationPreservesReaderContracts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		read   func(string) ([]byte, error)
		verify func(string, []byte) (string, error)
	}{
		{"article", 64 << 10, readFile, articleReadDigest}, {"source", store.MaxLineBytes, readSourceRecord, sourceReadDigest}, {"original", originalsLimit, readOriginals, originalReadDigest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data")
			check := func() {
				t.Helper()
				raw, wantErr := tc.read(path)
				got, err := tc.verify(path, make([]byte, 32<<10))
				if !sameReadResult(digest(raw), wantErr, got, err) {
					t.Fatal(len(raw), wantErr, got, err)
				}
			}
			for _, size := range []int{0, tc.limit - 1, tc.limit, tc.limit + 1} {
				if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0600); err != nil {
					t.Fatal(err)
				}
				check()
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			check()
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			check()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			check()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			check()
		})
	}
}

type validationFailureReader struct {
	input *bytes.Reader
	err   error
}

func (r validationFailureReader) Read(p []byte) (int, error) {
	if r.input.Len() == 0 {
		return 0, r.err
	}
	return r.input.Read(p)
}

func TestReadValidationPreservesPartialFailure(t *testing.T) {
	failure := errors.New("synthetic read failure")
	for _, size := range []int{0, 7, 16, 17, 25} {
		for _, discard := range []bool{false, true} {
			raw := bytes.Repeat([]byte("x"), size)
			data, wantErr := io.ReadAll(io.LimitReader(validationFailureReader{bytes.NewReader(raw), failure}, 17))
			if len(data) > 16 && (discard || wantErr == nil) {
				wantErr = errors.New("too large")
				if discard {
					data = nil
				}
			}
			got, err := boundedReadDigest(validationFailureReader{bytes.NewReader(raw), failure}, make([]byte, 32), 16, "too large", discard)
			if !sameReadResult(digest(data), wantErr, got, err) {
				t.Fatal(size, discard, wantErr, err)
			}
		}
	}
}
