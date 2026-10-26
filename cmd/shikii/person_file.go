package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// errPersonFileReadOnly is returned for a settings file the person made read-only: shikii does not replace it.
var errPersonFileReadOnly = errors.New("is read-only, so shikii left it as it is; make the change by hand")

// readOnlyError names the file (0.9.7 audit A-1: the refusal did not say which file).
func readOnlyError(path string) error { return fmt.Errorf("%s %w", path, errPersonFileReadOnly) }

// personFileError is a failure to write a file of the person's (its mode, an ACL, an immutable flag, the folder). init leaves that file
// to the person and goes on; settings it cannot read still stop init, since they are never overwritten (0.9.7 full audit 2-7).
type personFileError struct{ err error }

func (e *personFileError) Error() string { return e.err.Error() }
func (e *personFileError) Unwrap() error { return e.err }

// writePersonFile is the one way shikii changes a file of the person's (agent settings, config.toml, AGENTS.md; 0.9.7). Before, each writer
// did it its own way, and audits kept finding one that replaced a link with a regular file, dropped the file's permissions or wrote over a
// read-only file. It writes the file at the end of any chain of links (dotfiles stay links, even when that file does not exist yet), keeps
// the existing permissions (newMode is for a new file), refuses a file the person cannot write (its mode, an ACL, an immutable flag), and
// replaces the content through a temp file next to it, so a reader never sees half a file. A file with hard links is rewritten in place
// instead, so every name keeps showing the same file (0.9.7 full audit 2-3 to 2-5).
func writePersonFile(path string, content []byte, newMode os.FileMode) error {
	if err := writePersonFileTo(path, content, newMode); err != nil {
		return &personFileError{err}
	}
	return nil
}

func writePersonFileTo(path string, content []byte, newMode os.FileMode) error {
	path, err := endOfLinks(path)
	if err != nil {
		return err
	}
	mode := newMode
	hardLinked := false
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		if !personFileWritable(path) {
			return readOnlyError(path)
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
			hardLinked = true
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".shikii-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if hardLinked {
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		_, werr := out.Write(content)
		if cerr := out.Close(); werr != nil {
			return werr
		} else if cerr != nil {
			return cerr
		}
		return nil
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// endOfLinks follows a chain of links to the file at its end (which may not exist yet).
func endOfLinks(path string) (string, error) {
	for range 40 {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
	}
	return "", errors.New("too many links")
}

// personFileWritable reports whether the person can write the file: access(2) looks at the mode, the owner, ACLs and immutable flags.
func personFileWritable(path string) bool {
	return syscall.Access(path, 0x2) == nil // W_OK
}
