package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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
// the only hash-named one there (written under the path the project had then), is moved to it first. If it cannot be moved, it is used
// where it is. What an older shikii still running writes under the scope's hash afterwards is merged in (0.9.7 audit B-3).
func (s Store) projectPath(area, ext, scope string) string {
	dir := filepath.Join(s.Dir, "records", area)
	from := filepath.Join(dir, scopeKey(scope)+ext)
	fixed := filepath.Join(dir, projectName+ext)
	if !empty(fixed) {
		if _, err := os.Lstat(from); err == nil {
			s.merge(from, fixed, ext)
		}
		return fixed
	}
	if _, err := os.Lstat(from); err != nil {
		from = onlyHashNamed(dir, ext)
	}
	return s.adopt(area, ext, from)
}

// merge adds the records in from to fixed and removes from. A file is first renamed aside, so lines the older shikii appends meanwhile go
// to a new file and are merged next time; a folder's files are moved in unless the same name is already there.
func (s Store) merge(from, fixed, ext string) {
	info, err := os.Lstat(from)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if ext == "" {
		entries, err := os.ReadDir(from)
		if err != nil {
			return
		}
		for _, e := range entries {
			if _, err := os.Lstat(filepath.Join(fixed, e.Name())); os.IsNotExist(err) && e.Type().IsRegular() {
				_ = os.Rename(filepath.Join(from, e.Name()), filepath.Join(fixed, e.Name()))
			}
		}
		_ = os.Remove(from) // only when emptied
		return
	}
	aside := fmt.Sprintf("%s.merging-%d", from, os.Getpid())
	if os.Rename(from, aside) != nil {
		return
	}
	data, err := os.ReadFile(aside)
	if err != nil {
		return
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	f, err := os.OpenFile(fixed, os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		_ = os.Rename(aside, from)
		return
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil && cerr == nil {
		_ = os.Remove(aside)
	}
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
	if err := os.Rename(from, fixed); err != nil { // replaces an empty file or folder in one step (0.9.7 audit B-a)
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
