package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// The store's name (0.9.7): the wiki, its sources, the conversations, the memory deliveries and the memory reviews of a project are kept
// under one name inside its .shikii, written once in records/store-name and never changed. It is the hash of the path the store was first
// opened at, the same name 0.9.6 used, so an older shikii at that path uses the same files. Moving the folder renames nothing: shikii
// never moves or merges records. Before this, 0.9.7 moved them to a fixed name and merged what older versions kept writing, and every
// audit found another way that lost or broke a line; the structure was changed instead (the person's review, 2026-10-27).

var storeNamePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Link puts a file written in full at its path (os.Link); tests replace it to act like a disk without hard links.
var Link = os.Link

func scopeKey(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])
}

// StoreName returns the store's name, recording it the first time: the hash of the path in the store's project.json (the path it was
// initialized at, the name 0.9.6 used even when the folder was moved before 0.9.7 opened it; final audit BC-1), else of scope.
func StoreName(dir, scope string) string {
	if name := readStoreName(dir); name != "" {
		return name
	}
	name := scopeKey(scope)
	if initialized := initializedAt(dir); initialized != "" {
		name = scopeKey(initialized)
	}
	return recordStoreName(dir, name)
}

// RecordStoreNameFrom records the name of a store opened at another path before (scope): reconnecting a moved project calls it before
// pointing the project at its new path, so the records written at the old path keep being used.
func RecordStoreNameFrom(dir, scope string) string {
	if name := readStoreName(dir); name != "" {
		return name
	}
	return recordStoreName(dir, scopeKey(scope))
}

func readStoreName(dir string) string {
	if name := strings.TrimSpace(readSmallFile(filepath.Join(dir, "records", "store-name"), 128)); storeNamePattern.MatchString(name) {
		return name
	}
	return ""
}

// readSmallFile reads at most limit bytes of a regular file, without following a link or waiting on a FIFO (a .shikii brought in from
// elsewhere can hold anything; 0.9.9).
func readSmallFile(path string, limit int64) string {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(f, limit))
	return string(b)
}

// initializedAt is the path in the store's project.json, when it holds one.
func initializedAt(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil {
		return ""
	}
	var a struct {
		Scope string `json:"scope"`
	}
	if json.Unmarshal(b, &a) != nil || !filepath.IsAbs(a.Scope) {
		return ""
	}
	return a.Scope
}

// recordStoreName writes the name whole or not at all: the content goes to a temporary file first, which is then linked in place (the
// first writer wins; a crash or a full disk never leaves an empty name behind, final audit BC-3). A name file that is there but holds no
// valid name (left by a build before this) is replaced.
func recordStoreName(dir, name string) string {
	path := filepath.Join(dir, "records", "store-name")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return name
	}
	for try := 0; try < 2; try++ {
		if placed, err := PlaceOnce(path, []byte(name+"\n"), 0o600); err != nil || placed {
			return name
		}
		if existing := readStoreName(dir); existing != "" {
			return existing // recorded by another process
		}
		if !LeftOver(path) {
			return name // a name file being written just now, or not a regular file
		}
		_ = os.Remove(path) // a name file without a valid name (a stop while writing it)
	}
	return name
}

// LeftOver reports whether path is a regular file old enough (over 10 seconds) to be left by a stop while it was written, not a file another
// process is writing just now (on a disk without hard links PlaceOnce makes it first and writes it then; audit of 0.9.9, finding 4).
func LeftOver(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path) // a file that cannot be read now may hold a valid value: keep it (confirmation audit of 0.9.9)
	if err != nil {
		return false
	}
	f.Close()
	age := time.Since(info.ModTime())
	return age > 10*time.Second || age < -10*time.Second // a time in the future: copied from another machine, or a clock set back
}

// WriteTemp writes PlaceOnce's temp file; tests swap it to stop a write in the middle.
var WriteTemp = func(f *os.File, b []byte) (int, error) { return f.Write(b) }

// ChmodTemp sets the temp file's mode; tests swap it to refuse like Linux's exFAT and FAT drivers.
var ChmodTemp = (*os.File).Chmod

// PlaceOnce puts data at path only when nothing is there, and only in full: it is written to a temp file beside path and linked into place,
// so a reader never sees a part of it and the first writer wins. On a disk without hard links (exFAT, FAT) the temp file is renamed into
// place instead, while the folder is locked, so two shikii writers do not both place one; a stop never leaves a part at path (0.9.10).
// It reports whether it placed the file.
func PlaceOnce(path string, data []byte, perm os.FileMode) (bool, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".place-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	_, werr := WriteTemp(tmp, data)
	if err := ChmodTemp(tmp, perm); werr == nil && !errors.Is(err, syscall.EPERM) { // Linux's exFAT and FAT refuse modes (0.9.10)
		werr = err
	}
	if err := tmp.Sync(); werr == nil {
		werr = err
	}
	if err := tmp.Close(); werr == nil {
		werr = err
	}
	if werr != nil {
		return false, werr
	}
	err = Link(tmp.Name(), path)
	switch {
	case err == nil:
		return true, nil
	case os.IsExist(err):
		return false, nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	defer dir.Close()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

// recordPath is records/<area>/<store name><ext>.
func (s Store) recordPath(area, ext, scope string) string {
	return filepath.Join(s.Dir, "records", area, StoreName(s.Dir, scope)+ext)
}
