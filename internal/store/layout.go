package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The store's name (0.9.7): the wiki, its sources, the conversations, the memory deliveries and the memory reviews of a project are kept
// under one name inside its .shikii, written once in records/store-name and never changed. It is the hash of the path the store was first
// opened at, the same name 0.9.6 used, so an older shikii at that path uses the same files. Moving the folder renames nothing: shikii
// never moves or merges records. Before this, 0.9.7 moved them to a fixed name and merged what older versions kept writing, and every
// audit found another way that lost or broke a line; the structure was changed instead (the person's review, 2026-10-27).

var storeNamePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func scopeKey(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])
}

// StoreName returns the store's name, recording it the first time: the hash of the path in the store's project.json (the path it was
// initialized at, the name 0.9.6 used even when the folder was moved before 0.9.7 opened it; final audit BC-1), else of scope.
func StoreName(dir, scope string) string {
	if name := readStoreName(dir); name != "" {
		return name
	}
	name := scopeKey(scope)
	if initialized := initializedAt(dir); initialized != "" {
		name = scopeKey(initialized)
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

// initializedAt is the path in the store's project.json, when it holds one.
func initializedAt(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil {
		return ""
	}
	var a struct {
		Scope string `json:"scope"`
	}
	if json.Unmarshal(b, &a) != nil || !filepath.IsAbs(a.Scope) {
		return ""
	}
	return a.Scope
}

// recordStoreName writes the name whole or not at all: the content goes to a temporary file first, which is then linked in place (the
// first writer wins; a crash or a full disk never leaves an empty name behind, final audit BC-3). A name file that is there but holds no
// valid name (left by a build before this) is replaced.
func recordStoreName(dir, name string) string {
	path := filepath.Join(dir, "records", "store-name")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return name
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".store-name-*")
	if err != nil {
		return name
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(name + "\n")
	if serr := tmp.Sync(); werr == nil {
		werr = serr
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return name
	}
	if err := os.Link(tmp.Name(), path); err == nil {
		return name
	}
	if existing := readStoreName(dir); existing != "" {
		return existing // recorded by another process
	}
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		_ = os.Rename(tmp.Name(), path) // a name file without a valid name
	}
	return name
}

// recordPath is records/<area>/<store name><ext>.
func (s Store) recordPath(area, ext, scope string) string {
	return filepath.Join(s.Dir, "records", area, StoreName(s.Dir, scope)+ext)
}
