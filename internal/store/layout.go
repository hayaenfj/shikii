package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// The store's name (0.9.7): the wiki, its sources, the conversations, the memory deliveries and the memory reviews of a project are kept
// under one name inside its .shikii, written once in records/store-name and never changed. It is the hash of the path the store was first
// opened at, the same name 0.9.6 used, so an older shikii at that path uses the same files. Moving the folder renames nothing: shikii
// never moves or merges records. Before this, 0.9.7 moved them to a fixed name and merged what older versions kept writing, and every
// audit found another way that lost or broke a line; the structure was changed instead (the person's review, 2026-10-27).

var storeNamePattern = regexp.MustCompile(`^([0-9a-f]{64}|project)$`)

func scopeKey(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])
}

// StoreName returns the store's name, recording it the first time: the hash of scope, or "project" for a store a 0.9.7 build in
// development already moved there.
func StoreName(dir, scope string) string {
	if name := readStoreName(dir); name != "" {
		return name
	}
	name := scopeKey(scope)
	for _, p := range []string{filepath.Join(dir, "wiki", "lessons", "project"), filepath.Join(dir, "records", "feedback", "project.jsonl")} {
		if _, err := os.Lstat(p); err == nil {
			name = "project"
		}
	}
	return recordStoreName(dir, name)
}

// RecordStoreNameFrom records the name of a store opened at another path before (scope): reconnecting a moved project calls it before
// pointing the project at its new path, so the records written at the old path keep being used.
func RecordStoreNameFrom(dir, scope string) string {
	if name := readStoreName(dir); name != "" {
		return name
	}
	return recordStoreName(dir, scopeKey(scope))
}

func readStoreName(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "records", "store-name"))
	if err != nil {
		return ""
	}
	if name := strings.TrimSpace(string(b)); storeNamePattern.MatchString(name) {
		return name
	}
	return ""
}

func recordStoreName(dir, name string) string {
	path := filepath.Join(dir, "records", "store-name")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return name
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if existing := readStoreName(dir); existing != "" {
			return existing // recorded by another process just now
		}
		return name
	}
	defer f.Close()
	_, _ = f.WriteString(name + "\n")
	return name
}

// recordPath is records/<area>/<store name><ext>.
func (s Store) recordPath(area, ext, scope string) string {
	return filepath.Join(s.Dir, "records", area, StoreName(s.Dir, scope)+ext)
}
