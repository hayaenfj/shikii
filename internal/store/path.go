package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// parentRoot opens from the storage location down to the last parent using only ordinary folders.
// After opening each folder it checks it is still the same object, and from then on uses a Root that is not affected by renames.
func (s Store) parentRoot(path string, create bool) (*os.Root, string, error) {
	rootPath, err := filepath.Abs(s.Dir)
	if err != nil {
		return nil, "", err
	}
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	relative, err := filepath.Rel(rootPath, fullPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, "", errors.New("cannot open a file outside the storage location")
	}
	if create {
		if err := os.MkdirAll(rootPath, 0700); err != nil {
			return nil, "", err
		}
	}
	info, err := os.Lstat(rootPath)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", errors.New("links cannot be used in the storage location")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, "", err
	}
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		root.Close()
		return nil, "", errors.New("the storage folder changed")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		if create {
			if err := root.Mkdir(part, 0700); err != nil && !os.IsExist(err) {
				root.Close()
				return nil, "", err
			}
		}
		info, err := root.Lstat(part)
		if err != nil {
			root.Close()
			return nil, "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, "", errors.New("links cannot be used in the record folder")
		}
		child, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, "", err
		}
		actual, err := child.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(info, actual) {
			child.Close()
			return nil, "", errors.New("the record folder changed")
		}
		root = child
	}
	return root, parts[len(parts)-1], nil
}

// openInRoot opens a record file; a test swaps it to return ENOENT once on a create, as darwin did when two goroutines created the same file.
var openInRoot = (*os.Root).OpenFile

func (s Store) openFile(path string, flags int) (*os.File, error) {
	root, name, err := s.parentRoot(path, flags&os.O_CREATE != 0)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("make the record an ordinary file, not a link")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// When the same new file is created concurrently (parallel hook decisions, parallel decisions on wiki insertion points), os.Root's open with create can
	// return ENOENT (2026-10-23, reproduced on darwin; TestFirstRecordSurvivesACreateThatSaysNotThere). With create, wait briefly and open again.
	var f *os.File
	for attempt := 0; ; attempt++ {
		f, err = openInRoot(root, name, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
		if err == nil || flags&os.O_CREATE == 0 || !os.IsNotExist(err) || attempt >= 3 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Millisecond)
	}
	if err != nil {
		return nil, err // not wrapped (the caller uses os.IsNotExist to tell "not there yet")
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("cannot verify the record file")
	}
	return f, nil
}
