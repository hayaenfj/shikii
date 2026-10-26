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
	fixed := filepath.Join(dir, projectName+ext)
	if _, err := os.Lstat(fixed); !os.IsNotExist(err) {
		return fixed
	}
	from := filepath.Join(dir, scopeKey(scope)+ext)
	if _, err := os.Lstat(from); err != nil {
		from = onlyHashNamed(dir, ext)
	}
	if from == "" {
		return fixed
	}
	if info, err := os.Lstat(from); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return fixed // records are never links (openFile refuses them too)
	}
	if err := os.Rename(from, fixed); err != nil {
		if _, statErr := os.Lstat(fixed); statErr != nil { // not moved by another process either
			return from
		}
	}
	return fixed
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
