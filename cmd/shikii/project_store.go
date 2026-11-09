package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func validateStoreDirectories(root string) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions", "guards", "handoff"} {
		info, err := os.Lstat(filepath.Join(root, part))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("the project store cannot be a link to a shared location")
		}
	}
	return nil
}

func currentProjectStore() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return projectStoreAt(cwd)
}

func defaultProjectStore() (string, error) {
	root, _, err := currentProjectStore()
	if err != nil {
		return "", err
	}
	cwd, _ := os.Getwd()
	if filepath.Dir(root) == feedbackScope(cwd) {
		return ".shikii", nil
	}
	return root, nil
}

// Resolve defaults only after reading all flags. Never fall back to a refused store through a relative path.
func resolveStoreFlag(fs *flag.FlagSet) error {
	f := fs.Lookup("store")
	if f == nil {
		return nil
	}
	path := f.Value.String()
	if path == ".shikii" {
		var err error
		path, err = defaultProjectStore()
		if err != nil {
			return err
		}
	}
	if err := validateStoreDirectories(path); err != nil {
		return err
	}
	return f.Value.Set(path)
}

// Never write individual conversations or calibrations into a symlinked shared location.
func checkProjectStore(root string, create bool) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions", "guards", "handoff"} {
		path := filepath.Join(root, part)
		if create {
			if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) && part != "" && !create {
				continue // a store made by an old version (0.6.0 - 0.7.2 have no guards/) also counts as registered. Missing folders are created when used
			}
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("the project's .shikii and store must be regular folders (links to shared locations are not used)")
		}
	}
	return nil
}

// Keep personal conversations out of git add -A. The handling of already-tracked files does not change.
func excludeProjectStore(scope string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "info/exclude")
	cmd.Dir = scope
	raw, err := cmd.Output()
	if err != nil {
		return nil
	} // outside git
	path := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(path) {
		path = filepath.Join(scope, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	if bytes.Contains(append([]byte("\n"), data...), []byte("\n/.shikii/\n")) {
		return nil
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err = f.WriteString("\n/.shikii/\n")
	return err
}

func prepareHookProject(cwd string) (string, string, error) {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return "", "", err
	}
	active, err := projectActive(root, scope)
	if err != nil {
		return "", "", err
	}
	if !active {
		return "", "", errProjectInactive
	}
	if err := excludeProjectStore(scope); err != nil {
		return "", "", err
	}
	if err := checkProjectStore(root, true); err != nil {
		return "", "", err
	}
	if _, err := seedAgentSpecs(filepath.Join(root, "decisions")); err != nil {
		return "", "", err
	}
	if _, err := seedWiki(filepath.Join(root, "wiki")); err != nil {
		return "", "", err
	}
	if err := registerProject(scope); err != nil {
		return "", "", err
	}
	return root, scope, nil
}
