package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	shikii "github.com/hayaenfj/shikii"
)

type updateOptions struct {
	executable, version, goos, goarch string
	latestURL, downloadBase           string
	client                            *http.Client
	finish                            func(context.Context, string) error
	replace                           func(string, string) error
	reset                             func(context.Context, string, [3]uint64, [3]uint64) error // re-initializes the projects' records when crossing a version (old version, new version)
}

// storeResetAt is the version at which an update crossing it deletes everything inside each project's .shikii/ and re-initializes it (the owner's decision, 2026-10-23).
var storeResetAt = [3]uint64{0, 7, 0}

func versionString(v [3]uint64) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// currentVersion is this binary's version (shikii.Version).
func currentVersion() [3]uint64 {
	v, err := updateVersion(shikii.Version)
	if err != nil {
		return [3]uint64{}
	}
	return v
}

// crossesStoreReset reports whether from < storeResetAt <= to.
func crossesStoreReset(from, to [3]uint64) bool {
	return updateNewer(storeResetAt, from) && !updateNewer(storeResetAt, to)
}

// resetProjectStores, on an update crossing 0.7.0, deletes everything inside each registered project's .shikii/ and re-initializes it with the new binary.
func resetProjectStores(ctx context.Context, exe string, from, to [3]uint64) error {
	if !crossesStoreReset(from, to) {
		return nil
	}
	common, err := agentHome()
	if err != nil {
		return err
	}
	projects := registeredProjects(common)
	if len(projects) == 0 {
		return nil
	}
	fmt.Fprintf(stdout, "Updating to %s: deleting everything inside .shikii/ of %d initialized projects and re-initializing them.\n", versionString(to), len(projects))
	// Note: only the old binary running update (before 0.7.0) crosses 0.7.0 in this function. In a new binary, resetStoresOnFirstRun makes the same decision.
	var targets []projectEntry
	var refused []string
	for _, p := range planWipe(projects) {
		switch p.Action {
		case wipeDo:
			targets = append(targets, p.Project)
		case wipeRefuse:
			refused = append(refused, p.Project.Path+": "+p.Reason)
		}
	}
	if err := wipeProjectStores(targets, reinitWith(ctx, exe)); err != nil {
		return err
	}
	if len(refused) > 0 { // if any were refused, do not write the mark (the next hook install --yes tries again; same as resetStoresOnFirstRun)
		return errors.New(strings.Join(refused, "\n"))
	}
	// Also set the mark to the new version (so the new binary's hook install, called next, does not delete again).
	return writeInstalledVersion(common, to)
}

// installedVersionPath is the "last installed version" mark kept on the shared side. Written from 0.7.0 on (0.6.0 and earlier never wrote it, so a missing mark means 0.0.0).
func installedVersionPath(common string) string { return filepath.Join(common, "version") }

// installedVersion reads the shared mark. Missing means 0.0.0 (0.6.0 or earlier); unreadable or corrupt gives broken=true (audit R2: must not treat it as 0.0.0 and delete everything).
func installedVersion(common string) (v [3]uint64, broken bool) {
	b, err := os.ReadFile(installedVersionPath(common))
	if err != nil {
		if os.IsNotExist(err) {
			return [3]uint64{}, false
		}
		return [3]uint64{}, true
	}
	v, err = updateVersion(strings.TrimSpace(string(b)))
	if err != nil {
		return [3]uint64{}, true
	}
	return v, false
}

func writeInstalledVersion(common string, v [3]uint64) error {
	if err := os.MkdirAll(common, 0o755); err != nil {
		return err
	}
	return os.WriteFile(installedVersionPath(common), []byte(versionString(v)+"\n"), 0o644)
}

// resetStoresOnFirstRun runs when a new binary first runs `hook install --yes` (an update from 0.6.0 or earlier calls the new binary's
// hook install --yes at the end). If the previously installed version (the shared mark) crosses storeResetAt, it deletes everything inside each project's .shikii/,
// re-initializes it, and writes the current version to the mark. update's reset runs inside the old binary running update, so
// for an update from 0.6.0 this function does the deleting (noticed on 2026-10-23). Returns true when it deleted.
func resetStoresOnFirstRun(common string, to [3]uint64, reinit func(path string) error) (bool, error) {
	from, broken := installedVersion(common)
	if broken {
		return false, fmt.Errorf("%s: the version mark cannot be read, so no project's .shikii/ will be deleted. Check its contents and either rewrite it with the current version (%s) or delete it, then run shikii hook install --yes", installedVersionPath(common), versionString(to))
	}
	wiped := false
	if crossesStoreReset(from, to) {
		plan := planWipe(registeredProjects(common))
		var targets []projectEntry
		for _, p := range plan {
			if p.Action == wipeDo {
				targets = append(targets, p.Project)
			}
		}
		if len(targets) > 0 {
			fmt.Fprintf(stdout, "Updating to %s: deleting everything inside .shikii/ of %d initialized projects and re-initializing them.\n", versionString(to), len(targets))
			if err := wipeProjectStores(targets, reinit); err != nil {
				return false, err
			}
			wiped = true
		}
		var refused []string
		for _, p := range plan {
			if p.Action == wipeRefuse {
				refused = append(refused, filepath.Join(p.Project.Path, ".shikii")+": "+p.Reason)
			}
		}
		if len(refused) > 0 { // do not write the mark while refused ones remain (after cleaning up and running again, finished ones are skipped by each project's mark and only the rest are deleted)
			return wiped, errors.New(strings.Join(refused, "\n"))
		}
	}
	if from != to {
		if err := writeInstalledVersion(common, to); err != nil {
			return wiped, err
		}
	}
	return wiped, nil
}

// wipeAction is how each project is handled in the deletion when crossing 0.7.0. Display (hook install) and execution use the same decision (audit R2: display and reality used to disagree).
type wipeAction int

const (
	wipeDo     wipeAction = iota // delete and re-initialize
	wipeDone                     // already done: created (or re-initialized) by --init of 0.7.5 or later
	wipeRefuse                   // do not delete (shared store, not a regular folder, or has unknown entries)
)

type wipePlan struct {
	Project projectEntry
	Action  wipeAction
	Reason  string
}

func planWipe(projects []projectEntry) []wipePlan {
	var out []wipePlan
	for _, p := range projects {
		store := filepath.Join(p.Path, ".shikii")
		item := wipePlan{Project: p, Action: wipeDo}
		switch {
		case p.InitVersion >= projectInitVersionNow:
			item.Action, item.Reason = wipeDone, "already initialized by this version"
		case isSharedPlaceScope(p.Path):
			item.Action, item.Reason = wipeRefuse, "not deleted because it is the shared store (home)"
		default:
			info, err := os.Lstat(store)
			if err != nil {
				item.Action, item.Reason = wipeRefuse, err.Error()
				break
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				item.Action, item.Reason = wipeRefuse, "not deleted because it is not a regular folder"
				break
			}
			entries, err := os.ReadDir(store)
			if err != nil {
				item.Action, item.Reason = wipeRefuse, err.Error()
				break
			}
			var unknown []string
			for _, e := range entries {
				if !storeEntries[e.Name()] && e.Name() != "project.json" && !disposableEntry(e.Name()) {
					unknown = append(unknown, e.Name())
				}
			}
			if len(unknown) > 0 {
				item.Action, item.Reason = wipeRefuse, "not deleted because it has entries shikii did not create ("+strings.Join(unknown, ", ")+"). Clean them up by hand, then run shikii hook install --yes"
			}
		}
		out = append(out, item)
	}
	return out
}

// reinitWith re-initializes by running `exe --init` in path.
func reinitWith(ctx context.Context, exe string) func(path string) error {
	return func(path string) error {
		cmd := exec.CommandContext(ctx, exe, "--init")
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: cannot re-initialize: %v %s", path, err, strings.TrimSpace(string(out)))
		}
		fmt.Fprintf(stdout, "  %s: deleted and re-initialized\n", path)
		return nil
	}
}

// storeEntries are the entries shikii creates in each project's .shikii/ (allow list). Only these are deleted; a project with unknown entries is not deleted.
var storeEntries = map[string]bool{"decisions": true, "wiki": true, "records": true, "labels": true, "sessions": true, "guards": true, "handoff": true}

// disposableEntry is a stray that shikii did not create but that may be deleted: Finder's .DS_Store and temp files --init left behind midway (audit R3).
func disposableEntry(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, ".project-init-")
}

// isSharedPlaceScope reports whether scope is the parent of the shared store (~/.shikii/agent), i.e. home. Deleting it would remove the hook, index and marks too, so it is neither initialized nor deleted (audit 2026-10-23).
// Compares the real files (SameFile): a path spelled differently on a case-insensitive FS, or reached through a link, does not slip through (audit R2).
func isSharedPlaceScope(scope string) bool {
	agent, err := agentHome()
	if err != nil {
		return false
	}
	shared := filepath.Dir(filepath.Dir(agent))
	if scope == shared || feedbackScope(scope) == feedbackScope(shared) {
		return true
	}
	a, err1 := os.Stat(scope)
	b, err2 := os.Stat(shared)
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}

// wipeProjectStores deletes, inside each project's .shikii/ (a regular folder), the entries shikii created (storeEntries) and re-initializes with reinit.
// Nothing outside .shikii/ is touched. project.json is kept (so the project stays registered even if re-initializing fails). A project with unknown entries is not deleted.
// One failure does not stop it: it tries all of them and returns every failed path together.
func wipeProjectStores(projects []projectEntry, reinit func(path string) error) error {
	var failed []string
	for _, p := range projects {
		store := filepath.Join(p.Path, ".shikii")
		if isSharedPlaceScope(p.Path) {
			failed = append(failed, store+": not deleted because it is the shared store (home)")
			continue
		}
		info, err := os.Lstat(store)
		if err != nil {
			failed = append(failed, store+": "+err.Error())
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			failed = append(failed, store+": not deleted because it is not a regular folder")
			continue
		}
		entries, err := os.ReadDir(store)
		if err != nil {
			failed = append(failed, store+": "+err.Error())
			continue
		}
		unknown := []string{}
		for _, e := range entries {
			if !storeEntries[e.Name()] && e.Name() != "project.json" && !disposableEntry(e.Name()) {
				unknown = append(unknown, e.Name())
			}
		}
		if len(unknown) > 0 {
			failed = append(failed, fmt.Sprintf("%s: not deleted because it has entries shikii did not create (%s). Clean them up by hand, then run shikii --init", store, strings.Join(unknown, ", ")))
			continue
		}
		removed := true
		for _, e := range entries {
			if e.Name() == "project.json" {
				continue
			}
			if err := os.RemoveAll(filepath.Join(store, e.Name())); err != nil {
				failed = append(failed, filepath.Join(store, e.Name())+": "+err.Error())
				removed = false
				break
			}
		}
		if !removed {
			continue
		}
		if err := reinit(p.Path); err != nil {
			failed = append(failed, fmt.Sprintf("%s: deleted but could not re-initialize (%v). Run shikii --init in that folder", p.Path, err))
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "\n"))
	}
	return nil
}
