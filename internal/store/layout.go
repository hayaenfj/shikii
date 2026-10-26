package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// projectName is the fixed name of the project's conversations, memory deliveries and memory reviews under records/ (0.9.7). Before, they
// were named by sha256 of the project's path, so moving or renaming the folder hid them. A store belongs to one project.
const projectName = "project"

var hashName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func scopeKey(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])
}

// projectPath is records/<area>/project<ext> (a file, or a folder when ext is ""). One written before 0.9.7 under the scope's hash, or
// the only hash-named one there (written under the path the project had then), is moved to it first. If it cannot be moved, it is used where it is.
func (s Store) projectPath(area, ext, scope string) string {
	dir := filepath.Join(s.Dir, "records", area)
	from := filepath.Join(dir, scopeKey(scope)+ext)
	if _, err := os.Lstat(from); err != nil {
		from = onlyHashNamed(dir, ext)
	}
	return s.adopt(area, ext, from)
}

// MigrateLayout moves the records written before 0.9.7 under scope's hash to the fixed names. Reconnecting a moved project calls it with
// the old path, which tells this project's files apart even when files from still earlier paths sit beside them.
func (s Store) MigrateLayout(scope string) {
	for _, a := range []struct{ area, ext string }{{"feedback", ".jsonl"}, {"memories", ".jsonl"}, {"memory-reviews", ""}} {
		s.adopt(a.area, a.ext, filepath.Join(s.Dir, "records", a.area, scopeKey(scope)+a.ext))
	}
}

// adopt moves from to records/<area>/project<ext> unless that already holds records (an empty file or folder made before is replaced).
func (s Store) adopt(area, ext, from string) string {
	fixed := filepath.Join(s.Dir, "records", area, projectName+ext)
	if !empty(fixed) {
		return fixed
	}
	if from == "" {
		return fixed
	}
	if info, err := os.Lstat(from); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return fixed // records are never links (openFile refuses them too)
	}
	if _, err := os.Lstat(fixed); err == nil {
		_ = os.Remove(fixed) // empty
	}
	if err := os.Rename(from, fixed); err != nil {
		if _, statErr := os.Lstat(fixed); statErr != nil { // not moved by another process either
			return from
		}
	}
	return fixed
}

// empty reports whether path is missing, an empty file or an empty folder.
func empty(path string) bool {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		return err == nil && len(entries) == 0
	}
	return info.Size() == 0
}

func onlyHashNamed(dir, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	only := ""
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ext)
		if !ok || !hashName.MatchString(name) || e.IsDir() != (ext == "") {
			continue
		}
		if only != "" {
			return ""
		}
		only = filepath.Join(dir, e.Name())
	}
	return only
}
