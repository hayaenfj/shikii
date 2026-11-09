package notebook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// directory uses only fixed subdirectories. It does not follow symlinks in the generated area.
func (b Book) directory(create bool, area string) (string, error) {
	// Lookup and delivery journals can be written by the preparation itself.
	// Only the article and source layout belongs to its knowledge snapshot.
	if !create && b.snapshot != nil && (area == "pages" || area == "sources") {
		return b.sharedCheck("area:"+area, func() (string, error) {
			return (Book{Root: b.Root, Scope: b.Scope, name: b.name}).directory(false, area)
		})
	}
	if b.Scope == "" || b.Root == "" {
		return "", errors.New("wiki needs a project and a store")
	}
	name := b.storeName()
	parts := []string{"wiki", "lessons", name}
	if area == "sources" {
		parts = []string{"records", "wiki-sources", name}
	}
	if area == "deliveries" {
		parts = []string{"records", "wiki-deliveries", name}
	}
	if area == "searches" {
		parts = []string{"records", "wiki-searches", name}
	}
	if create {
		if err := os.MkdirAll(b.Root, 0o700); err != nil {
			return "", err
		}
	}
	path, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		return "", err
	}
	for _, part := range parts {
		path = filepath.Join(path, part)
		if create {
			if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
				return "", err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("wiki generated directories must not be symbolic links")
		}
	}
	return path, nil
}

func readFile(path string) ([]byte, error) {
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
	return readPageBytes(f, info.Size())
}

func writeFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".wiki-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func immutable(path string, data []byte) error {
	// Publish a complete temporary file by linking it. An existing source record is not replaced.
	f, err := os.CreateTemp(filepath.Dir(path), ".source-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Link(f.Name(), path); err != nil {
		if !os.IsExist(err) {
			return err
		}
		old, rerr := readSourceRecord(path) // a source record can be larger than a page
		if rerr != nil || !bytes.Equal(old, data) {
			return errors.New("an immutable wiki source or revision has changed")
		}
	}
	return nil
}

// lockBusy, when set (tests), is called each time lock finds the wiki's lock held and is about to wait: the moment another update is in
// progress.
var lockBusy func()

func lock(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	return lockOpened(f)
}

func lockOpened(f *os.File) (func(), error) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("wiki lock must be a regular file")
	}
	until := time.Now().Add(2 * time.Second)
	for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if time.Now().After(until) {
			f.Close()
			return nil, errors.New("wiki is busy; retry later")
		}
		if lockBusy != nil {
			lockBusy()
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

// pageLabels are the fixed words around a lesson's fields in its Markdown file. New pages are written with englishLabels (0.9.2);
// pages written before 0.9.2 used legacyLabels, and they are still accepted as unchanged (matchesPage), so updating does not
// turn every existing page into a changed page.
type pageLabels struct{ correction, action, reason, related, applies, sources, note string }

var englishLabels = pageLabels{"The person's correction: ", "Next action (hypothesis): ", "Reason for the change: ", "## Related", "Applies when: ", "## Sources",
	"This lesson is a hypothesis with sources. The current request and the rules come first."}

// legacyLabels are the Japanese words pages were written with before 0.9.2 (kept only to recognize those pages)
var legacyLabels = pageLabels{"人の訂正の引用: ", "次に変える行動（仮説）: ", "変更理由: ", "## 関連する知識", "適用する場面: ", "## 出典",
	"この教訓は出典付きの仮説です。今回の依頼と規則を優先します。"}
