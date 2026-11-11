package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hayaenfj/shikii/internal/rootfile"
)

// The shared side holds only the entries to projects. Conversations, scores and lines live in each .shikii/.
type projectEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	URL         string `json:"url"`
	StoreID     string `json:"store_id,omitempty"`  // the store's own id (records/store-id): a move is reconnected only for the store initialized here (0.9.7)
	StoreDir    string `json:"store_dir,omitempty"` // the .shikii folder itself (device:inode): mv on one disk keeps it, a copy never has it (0.9.7 audit B-1)
	InitVersion int    `json:"-"`                   // project.json version: 1 = --init of 0.7.4 or earlier, 2 = 0.7.5 or later (the wipe crossing 0.7.0 is done)
}

var errProjectInactive = errors.New("run shikii --init in this project")

// Not a setting: a mark that an explicit initialization finished completely. Copying it elsewhere does not activate anything.
type projectActivation struct {
	Version int    `json:"version"`
	Scope   string `json:"scope"`
}

// projectInitVersionNow is the project.json version --init writes. 2 = 0.7.5 or later (the mark that tells each project the wipe crossing 0.7.0 is "done".
// Audit R2: with only the shared mark, one refusal kept the mark from being written, and the next run wiped again a project that had already been wiped and re-initialized).
const projectInitVersionNow = 2

// projectInitVersion is the project.json version (0 if missing).
func projectInitVersion(root string) int {
	a, ok := readProjectFile(filepath.Join(root, "project.json"))
	if !ok {
		return 0
	}
	return a.Version
}

// readProjectFile reads a store's project.json without following a link in its name and without waiting on a FIFO (a .shikii brought
// in from elsewhere can hold anything): a link, a FIFO, a folder or a file past 4096 bytes is not one.
func readProjectFile(path string) (projectActivation, bool) {
	var a projectActivation
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return a, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return a, false
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 || json.Unmarshal(b, &a) != nil {
		return projectActivation{}, false
	}
	return a, true
}

// wipePending reports whether shikii's store (records/ etc.) remains with an old mark (version 1 or lower), i.e. the wipe crossing 0.7.0 has not happened yet.
// During a re-initialization in the middle of a wipe, the store holds only project.json, so it is false.
func wipePending(root string) bool {
	v := projectInitVersion(root)
	if v <= 0 || v >= projectInitVersionNow {
		return false
	}
	for name := range storeEntries {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}

func projectActive(root, scope string) (bool, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("the initialization store must be a regular folder")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer dir.Close()
	opened, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return false, errors.New("the initialization store changed")
	}
	markerInfo, err := dir.Lstat("project.json")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !markerInfo.Mode().IsRegular() {
		return false, errors.New("the initialization mark must be a regular file")
	}
	f, err := rootfile.OpenNoFollow(dir, "project.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || !os.SameFile(markerInfo, stat) || stat.Size() > 4096 {
		return false, errors.New("cannot check the project's initialization mark")
	}
	var activation projectActivation
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&activation); err != nil {
		return false, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return false, errors.New("the project's initialization mark is invalid")
	}
	return activation.Version >= 1 && (activation.Scope == scope || sameFolder(activation.Scope, scope)), nil
}

// version is the version written to project.json. If a new binary's --init was only rerun on a store with an old mark (version 1, 0.7.4 or earlier) and records,
// the caller (cmdInit) passes the original version: it neither raises the version nor writes the shared mark (raising it would make the next hook install --yes skip it as "already initialized by this version",
// and the wipe crossing 0.7.0 would be skipped forever. Audit R3)
func finishProjectInit(root, scope string, version int) error {
	// Even if the index is created first, the project shows up on neither the page nor the hook until the final mark exists.
	if _, err := os.UserHomeDir(); err == nil {
		if err := registerProject(scope); err != nil {
			return err
		}
	}
	// So that someone who installed with go install and ran --init before hook install is not treated as "an update from 0.6.0 or earlier" on their first hook install --yes,
	// write the current version if there is no shared mark (audit R2)
	// But do not write it when other projects are already registered (even if --init is called during the wipe crossing 0.7.0, do not pre-empt the wipe's decision)
	if agent, err := agentHome(); err == nil && version == projectInitVersionNow {
		if _, err := os.Stat(installedVersionPath(agent)); os.IsNotExist(err) {
			others := 0
			for _, p := range registeredProjects(agent) {
				if p.Path != scope {
					others++
				}
			}
			if others == 0 {
				_ = writeInstalledVersion(agent, currentVersion())
			}
		}
	}
	marker := filepath.Join(root, "project.json")
	if info, err := os.Lstat(marker); err == nil && !info.Mode().IsRegular() {
		return errors.New("the initialization mark cannot be a link")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, _ := json.Marshal(projectActivation{Version: version, Scope: scope})
	f, err := os.CreateTemp(root, ".project-init-*")
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
	return os.Rename(f.Name(), marker)
}
