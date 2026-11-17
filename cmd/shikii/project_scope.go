package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// projectScopeAt is the project a folder belongs to: the nearest folder at or above it that shikii was initialized in. A worktree or
// any other folder kept under a project, with or without git, uses that project's store instead of being a place of its own. Where no
// folder above is initialized, it is the folder shikii init prepares (projectTopAt).
func projectScopeAt(cwd string) string {
	if cwd == "" {
		return ""
	}
	if dir := initializedAbove(feedbackScope(cwd)); dir != "" {
		return canonicalScope(dir)
	}
	return projectTopAt(cwd)
}

// initializedAbove is the nearest folder at or above dir that holds an initialized store (a real .shikii folder with a regular
// project.json), "" when there is none. The shared place (the home folder, whose .shikii is the agents' store) is never a project.
func initializedAbove(dir string) string {
	for dir != "" {
		if store, err := os.Lstat(filepath.Join(dir, ".shikii")); err == nil && store.IsDir() {
			if file, err := os.Lstat(filepath.Join(dir, ".shikii", "project.json")); err == nil && file.Mode().IsRegular() && !isSharedPlaceScope(dir) {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// treeProjectScope is the project the working tree at top belongs to, spelled as the hooks key their records: top itself when it is
// initialized or no folder above it is, else the nearest initialized folder above it. The tree's files are still compared at top.
func treeProjectScope(top string) string {
	scope := feedbackScope(top)
	if dir := initializedAbove(scope); dir != "" {
		return dir
	}
	return scope
}

// projectTopAt is the folder shikii init prepares from cwd: the top folder of its git working tree, or cwd itself outside git. init
// uses it also below an initialized folder, so a repository kept under a project can still be made a project of its own.
func projectTopAt(cwd string) string {
	if cwd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	scope := feedbackScope(cwd)
	if top, err := cmd.Output(); err == nil {
		scope = feedbackScope(strings.TrimSpace(string(top)))
	}
	return canonicalScope(scope)
}

// canonicalScope is the spelling of the project's path that its project.json records, when scope reaches that same folder by another
// spelling (another case on a case-insensitive disk): every key made from the path (the page, its lock, the index) is then one (re-audit 1-3).
func canonicalScope(scope string) string {
	a, ok := readProjectFile(filepath.Join(scope, ".shikii", "project.json"))
	if !ok || a.Scope == "" || a.Scope == scope || !filepath.IsAbs(a.Scope) {
		return scope
	}
	if sameFolder(a.Scope, scope) {
		return a.Scope
	}
	return scope
}

func projectID(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:])
}

func projectStoreAt(cwd string) (string, string, error) {
	return storeOfScope(projectScopeAt(cwd))
}

// projectTopStoreAt is the store shikii init prepares from cwd (projectTopAt).
func projectTopStoreAt(cwd string) (string, string, error) {
	return storeOfScope(projectTopAt(cwd))
}

func storeOfScope(scope string) (string, string, error) {
	if scope == "" {
		return "", "", errors.New("cannot verify the project folder")
	}
	root := filepath.Join(scope, ".shikii")
	if err := validateStoreDirectories(root); err != nil {
		return "", "", err
	}

	return root, scope, nil
}
