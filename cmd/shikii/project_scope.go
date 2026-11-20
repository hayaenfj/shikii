package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// projectScopeAt is the project a folder belongs to. It is the folder shikii init prepares from there (projectTopAt: the top of its
// git working tree, or the folder itself outside git), unless a project of this machine is met first on the way up from cwd: a
// worktree or any other folder kept under such a project, with or without git, uses that project's store instead of being a place
// of its own.
func projectScopeAt(cwd string) string {
	if cwd == "" {
		return ""
	}
	top := projectTopAt(cwd)
	if dir := projectAbove(feedbackScope(cwd), top); dir != "" {
		return canonicalScope(dir)
	}
	return top
}

// projectAbove is the nearest folder at or above dir that holds an initialized store (a real .shikii folder with a regular
// project.json) and is either own, the folder shikii init prepares from there, or a project registered on this machine: for such
// a project it returns the path this machine's index records (registeredStorePath). "" when there is none. A store this machine
// did not initialize, such as one copied in or placed in a folder above by someone else, is passed over: walking up never hands a
// session to it, and nothing it holds but its id is read (audit of 0.37.0). The shared place (the home folder, whose .shikii is
// the agents' store) is never a project.
func projectAbove(dir, own string) string {
	agent, _ := agentHome()
	for dir != "" {
		if store, err := os.Lstat(filepath.Join(dir, ".shikii")); err == nil && store.IsDir() {
			if file, err := os.Lstat(filepath.Join(dir, ".shikii", "project.json")); err == nil && file.Mode().IsRegular() && !isSharedPlaceScope(dir) {
				if sameFolder(dir, own) {
					return dir
				}
				if agent != "" {
					if path := registeredStorePath(agent, dir); path != "" {
						return path
					}
				}
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

// treeProjectScope is the project a session at cwd belongs to, as the hooks key their records, where top is the folder its files
// are compared at (the top of cwd's git working tree, or cwd outside git). It is projectScopeAt for the hooks, which already asked
// git for top: the same walk and the same spelling, so the hooks and the CLI never name two projects for one folder, also when
// cwd spells the path in another case (confirmation audit of 0.37.0).
func treeProjectScope(cwd, top string) string {
	top = canonicalScope(feedbackScope(top))
	if dir := projectAbove(feedbackScope(cwd), top); dir != "" {
		return canonicalScope(dir)
	}
	return top
}

// projectTopAt is the folder shikii init prepares from cwd: the top folder of its git working tree, or cwd itself outside git. init
// uses it also below an initialized folder, so a repository kept under a project can still be made a project of its own.
func projectTopAt(cwd string) string {
	if cwd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookPlaceTimeout)
	defer cancel()
	scope := feedbackScope(cwd)
	if place, ok := gitPlaceAt(ctx, cwd); ok {
		scope = feedbackScope(place.top)
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
	if sameFolder(a.Scope, scope) && !leadsThroughLink(a.Scope) {
		return a.Scope // not a link left at an old path: that spelling is reconnected, not kept
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

// leadsThroughLink reports whether a recorded path reaches its folder through a link (the other spelling of the same folder that is not just
// another case): the project was moved and a link was left at the old path, so project.json still names the old path.
func leadsThroughLink(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && !strings.EqualFold(resolved, filepath.Clean(path))
}
