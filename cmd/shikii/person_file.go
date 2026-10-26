package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// errPersonFileReadOnly is returned for a settings file the person made read-only: shikii does not replace it.
var errPersonFileReadOnly = errors.New("is read-only, so shikii left it as it is; make the change by hand")

// readOnlyError names the file (0.9.7 audit A-1: the refusal did not say which file).
func readOnlyError(path string) error { return fmt.Errorf("%s %w", path, errPersonFileReadOnly) }

// writePersonFile is the one way shikii changes a file of the person's (agent settings, config.toml, AGENTS.md; 0.9.7). Before, each writer
// did it its own way, and audits kept finding one that replaced a link with a regular file, dropped the file's permissions or wrote over a
// read-only file. It writes the file a link points at (dotfiles stay links), keeps the existing permissions (newMode is for a new file),
// refuses a read-only file, and replaces the content through a temp file next to it, so a reader never sees half a file.
func writePersonFile(path string, content []byte, newMode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		} else if target, err := os.Readlink(path); err == nil { // a link whose file does not exist yet: create that file, the link stays (0.9.7 audit A-4)
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			path = target
		} else {
			return err
		}
	}
	mode := newMode
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		if mode&0o200 == 0 {
			return readOnlyError(path)
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
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
