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
// openPersonFile opens a file of the person's to write it; tests replace it to act like a disk mounted read-only.
var openPersonFile = os.OpenFile

func readOnlyError(path string) error { return fmt.Errorf("%s %w", path, errPersonFileReadOnly) }

// personFileError is a failure to write a file of the person's (its mode, an ACL, an immutable flag, the folder). init leaves that file
// to the person and goes on; settings it cannot read still stop init, since they are never overwritten (0.9.7 full audit 2-7).
type personFileError struct{ err error }

func (e *personFileError) Error() string { return e.err.Error() }
func (e *personFileError) Unwrap() error { return e.err }

// writePersonFile is the one way shikii changes a file of the person's (agent settings, config.toml, AGENTS.md; 0.9.7). Before, each writer
// did it its own way, and then the one writer replaced the file through a temp file and added a fix for each kind of file the audits found
// (links, chains of links, hard links, ACLs, groups, extended attributes, immutable flags, a relative link under a linked folder).
// Now it never replaces the file: it writes into the very file the OS opens at that path, so whatever the person set on it or around
// it stays as it is. A file it cannot open for writing (its mode, an ACL, an immutable flag) is refused as read-only. If writing stops
// halfway, the old content is written back, so the file does not end up empty or mixed (0.9.7 re-audit 2).
func writePersonFile(path string, content []byte, newMode os.FileMode) error {
	if err := writePersonFileTo(path, content, newMode); err != nil {
		return &personFileError{err}
	}
	return nil
}

func writePersonFileTo(path string, content []byte, newMode os.FileMode) error {
	old, err := os.ReadFile(path) // through any links, as the agent reads it
	if os.IsNotExist(err) {
		if _, lerr := os.Lstat(path); os.IsNotExist(lerr) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, newMode) // a new file, or the missing file at the end of a link
		if err != nil {
			if os.IsPermission(err) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS) { // EROFS: a disk mounted read-only (0.9.9)
				return readOnlyError(path)
			}
			return err
		}
		_, werr := f.Write(content)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return werr
	}
	if err != nil {
		return err
	}
	f, err := openPersonFile(path, os.O_WRONLY, 0)
	if err != nil {
		if os.IsPermission(err) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS) { // EROFS: a disk mounted read-only (0.9.9)
			return readOnlyError(path)
		}
		return err
	}
	_, werr := f.WriteAt(content, 0)
	if werr == nil {
		werr = f.Truncate(int64(len(content)))
	}
	if werr == nil {
		werr = f.Sync()
	}
	if werr != nil { // put the old content back: the old size fitted before
		_, _ = f.WriteAt(old, 0)
		_ = f.Truncate(int64(len(old)))
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// personFileWritable reports whether the person can write the file: access(2) looks at the mode, the owner, ACLs and immutable flags.
func personFileWritable(path string) bool {
	return syscall.Access(path, 0x2) == nil // W_OK
}
