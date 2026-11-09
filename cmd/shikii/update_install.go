package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func updateLock(root *os.Root) (*os.File, error) {
	const name = ".shikii-update.lock"
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("the update lock cannot be a link")
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.New("cannot write to the install location. Put shikii in a place you have write permission for")
	}
	info, err := f.Stat()
	named, nerr := root.Lstat(name)
	if err != nil || nerr != nil || !named.Mode().IsRegular() || !os.SameFile(info, named) {
		f.Close()
		return nil, errors.New("cannot check the update lock")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another shikii update is running. Try again after it finishes")
	}
	return f, nil
}

// installUpdate prepares everything on the same file system and replaces the binary last.
// If it fails midway, the files already replaced are restored from the backup.
func installUpdate(ctx context.Context, root *os.Root, exe, version string, files map[string][]byte, replace func(string, string) error) error {
	stage, err := os.MkdirTemp(filepath.Dir(exe), ".shikii-update-")
	if err != nil {
		return errors.New("cannot write to the install location. The existing executable was kept")
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(stage)
		}
	}()
	for _, name := range updateFiles {
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "shikii") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(stage, "new-"+name), files[name], mode); err != nil {
			return errors.New("cannot prepare the update files")
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, filepath.Join(stage, "new-shikii"), "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "shikii "+version {
		return errors.New("cannot confirm that the downloaded shikii starts and has the right version. The existing executable was kept")
	}
	licenseCreated := false
	if info, err := root.Lstat("shikii-licenses"); os.IsNotExist(err) {
		if err := root.Mkdir("shikii-licenses", 0o755); err != nil {
			return errors.New("cannot prepare the license destination")
		}
		licenseCreated = true
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("the license destination cannot be a link")
	}
	defer func() {
		if licenseCreated {
			_ = root.Remove("shikii-licenses")
		}
	}()
	type prior struct {
		name, target, backup string
		info                 os.FileInfo
	}
	var entries []prior
	for _, name := range updateFiles {
		target := name
		if name == "shikii" {
			target = filepath.Base(exe)
		} else if name != "shikii-readout" {
			target = filepath.Join("shikii-licenses", name)
		}
		p := prior{name: name, target: target, backup: filepath.Join(filepath.Base(stage), "old-"+name)}
		info, err := root.Lstat(target)
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("update target %s is not a regular file", target)
			}
			p.info = info
			old, err := root.Open(target)
			if err != nil {
				return errors.New("cannot move the existing executable aside")
			}
			opened, serr := old.Stat()
			if serr != nil || !os.SameFile(info, opened) {
				old.Close()
				return errors.New("the update target changed midway")
			}
			backup, err := root.OpenFile(p.backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				old.Close()
				return err
			}
			_, copyErr := io.Copy(backup, old)
			old.Close()
			syncErr := backup.Sync()
			closeErr := backup.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil {
				return errors.New("cannot finish moving the existing files aside")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		entries = append(entries, p)
	}
	for _, p := range entries {
		info, err := root.Lstat(p.target)
		if p.info == nil {
			if !os.IsNotExist(err) {
				return errors.New("the update target was created midway")
			}
		} else if err != nil || !os.SameFile(p.info, info) {
			return errors.New("the update target changed midway")
		}
	}
	if replace == nil {
		replace = root.Rename
	}
	var installed []prior
	for _, p := range entries {
		if err := ctx.Err(); err == nil {
			err = replace(filepath.Join(filepath.Base(stage), "new-"+p.name), p.target)
			if err == nil {
				installed = append(installed, p)
				continue
			}
		}
		for i := len(installed) - 1; i >= 0; i-- {
			old := installed[i]
			var err error
			if old.info == nil {
				err = root.Remove(old.target)
			} else {
				err = root.Rename(old.backup, old.target)
			}
			if err != nil {
				keep = true
			}
		}
		if keep {
			return fmt.Errorf("the update and the restore both failed. The moved-aside files are kept at: %s", stage)
		}
		return errors.New("could not write the update target. Restored the existing executable")
	}
	return nil
}

func finishUpdate(ctx context.Context, exe string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	installed := false
	for _, target := range hookTargets {
		body, err := os.ReadFile(filepath.Join(home, target.rel))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return errors.New("cannot check the hook settings")
		}
		var config struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(body, &config) != nil {
			return errors.New("cannot read the hook settings. Check them with shikii hook install --yes")
		}
		for _, groups := range config.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if isShikiiHook(hook.Command) {
						bin, _ := hookBinary(hook.Command)
						actual, _ := filepath.EvalSymlinks(bin)
						if actual == exe {
							installed = true
						}
					}
				}
			}
		}
	}
	if installed {
		cmd := exec.CommandContext(ctx, exe, "hook", "install", "--yes")
		cmd.Stdout, cmd.Stderr = stdout, stdout // pass the new binary's deletion targets and failure reasons through to the update output (audit R2: they used to be discarded)
		if err := cmd.Run(); err != nil {
			return errors.New("cannot update the hook and the working rules. Retry with shikii hook install --yes")
		}
		fmt.Fprintln(stdout, "Applied to the hook and the agents' working rules.")
	}
	fmt.Fprintln(stdout, "A running page or MCP switches to the new version when restarted.")
	return nil
}
