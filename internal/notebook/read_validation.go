package notebook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"syscall"

	"github.com/hayaenfj/shikii/internal/store"
)

// Each policy preserves its reader's partial-data and error contract. The
// scratch buffer belongs to one sequential preparation, never to another reader.
func boundedReadDigest(r io.Reader, scratch []byte, limit int64, problem string, discardOversize bool) (string, error) {
	h := sha256.New()
	n, err := io.CopyBuffer(h, io.LimitReader(r, limit+1), scratch)
	if n > limit && (discardOversize || err == nil) {
		err = errors.New(problem)
		if discardOversize {
			return digest(nil), err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), err
}

func articleReadDigest(path string, scratch []byte) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return digest(nil), err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return digest(nil), err
	}
	if !info.Mode().IsRegular() {
		return digest(nil), errors.New("a wiki page must be a regular file")
	}
	return boundedReadDigest(f, scratch, 64<<10, "wiki page exceeds the size limit", false)
}

func sourceReadDigest(path string, scratch []byte) (string, error) {
	f, err := openSourceFile(path)
	if err != nil {
		return digest(nil), err
	}
	defer f.Close()
	return boundedReadDigest(f, scratch, int64(store.MaxLineBytes), "the saved source record exceeds the store's line limit", false)
}

func originalReadDigest(path string, scratch []byte) (string, error) {
	f, err := openSourceFile(path)
	if err != nil {
		return digest(nil), err
	}
	defer f.Close()
	return boundedReadDigest(f, scratch, 2<<20, "the long source text must be 2MB or less", true)
}

func (b Book) sharedArticle(path string) ([]byte, error) {
	return b.sharedReadValidated(path, readFile, articleReadDigest)
}
func (b Book) sharedSourceRecord(path string) ([]byte, error) {
	return b.sharedReadValidated(path, readSourceRecord, sourceReadDigest)
}
func (b Book) sharedOriginals(path string) ([]byte, error) {
	return b.sharedReadValidated(path, readOriginals, originalReadDigest)
}
