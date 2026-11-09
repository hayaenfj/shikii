package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func projectScopeAt(cwd string) string {
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
	b, err := os.ReadFile(filepath.Join(scope, ".shikii", "project.json"))
	if err != nil {
		return scope
	}
	var a projectActivation
	if json.Unmarshal(b, &a) != nil || a.Scope == "" || a.Scope == scope || !filepath.IsAbs(a.Scope) {
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
	scope := projectScopeAt(cwd)
	if scope == "" {
		return "", "", errors.New("cannot verify the project folder")
	}
	root := filepath.Join(scope, ".shikii")
	if err := validateStoreDirectories(root); err != nil {
		return "", "", err
	}

	return root, scope, nil
}
