package notebook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// storeDirName is the fixed name of the project's wiki pages and sources inside its store (0.9.7). Before, both were named by sha256 of the
// project's path, so moving or renaming the folder emptied the wiki until a person moved them by hand, and every version found another place
// the manual steps missed. A store belongs to one project, so its name needs no path.
const storeDirName = "project"

var storeAreas = []string{"wiki/lessons", "records/wiki-sources"}

// legacyPathsFile, in the sources store, lists the project paths whose hash named the store before 0.9.7. A source id then held the path it
// was captured under, so these paths are what checks a source captured before 0.9.7 that no page cites yet.
const legacyPathsFile = ".legacy-paths.json"

// MigrateLayout moves a store written before 0.9.7 to the fixed names: the area named by scope's hash, or, when that is missing, the only
// hash-named area (a store belongs to one project, so the one left is this project's, written under the path it had then). Areas already
// at the fixed name are left. Two hash-named areas and none of this path's are left for the person (OtherPathStores names them).
// scope is remembered for checking the old source ids when its area is moved.
func MigrateLayout(root, scope string) error {
	for _, area := range storeAreas {
		fixed := filepath.Join(root, area, storeDirName)
		if _, err := os.Lstat(fixed); err == nil {
			mergeLater(root, area, fixed, scope)
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		from, known := filepath.Join(root, area, digest([]byte(scope))), true
		if _, err := os.Lstat(from); os.IsNotExist(err) {
			from, known = onlyHashNamed(filepath.Join(root, area)), false
		}
		if from == "" {
			continue
		}
		if info, err := os.Lstat(from); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue // generated stores are never links
		}
		if err := os.Rename(from, fixed); err != nil {
			if _, statErr := os.Lstat(fixed); statErr != nil { // another process moved it first
				return err
			}
		} else if area == "wiki/lessons" {
			if err := rememberName(fixed, filepath.Base(from)); err != nil {
				return err
			}
		}
		if known && area == "records/wiki-sources" {
			if err := RememberPath(root, scope); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeLater brings in what an older shikii (before 0.9.7, still running) wrote under the path's hash after the area moved to the fixed
// name: a file missing from the fixed area is moved in, one already there with the same content is dropped, and the sources registry is
// joined. The page links and source ids it wrote under that name are accepted (rememberName, RememberPath). It is skipped when the area
// is busy and tried again on the next use; files that differ stay, and OtherPathStores names them (0.9.7 full audit 1-2).
func mergeLater(root, area, fixed, scope string) {
	from := filepath.Join(root, area, digest([]byte(scope)))
	if info, err := os.Lstat(from); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	unlock, err := lock(fixed)
	if err != nil {
		return
	}
	defer unlock()
	var captured []string
	_ = filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(fixed, rel)
		switch name := d.Name(); {
		case name == ".captured.json":
			if ids, err := capturedIDs(filepath.Dir(path)); err == nil {
				captured = append(captured, ids...)
			}
			_ = os.Remove(path)
		case name == ".lock" || name == "index.md" || name == "log.md":
			_ = os.Remove(path) // made again per store
		default:
			old, err := os.ReadFile(target)
			switch {
			case os.IsNotExist(err):
				if os.MkdirAll(filepath.Dir(target), 0o700) == nil {
					_ = os.Rename(path, target)
				}
			case err == nil:
				if now, err := os.ReadFile(path); err == nil && bytes.Equal(old, now) {
					_ = os.Remove(path)
				}
			}
		}
		return nil
	})
	if len(captured) > 0 {
		ids, _ := capturedIDs(fixed)
		seen := map[string]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		for _, id := range captured {
			if !seen[id] {
				ids, seen[id] = append(ids, id), true
			}
		}
		if data, err := json.Marshal(ids); err == nil {
			_ = writeFile(filepath.Join(fixed, ".captured.json"), data)
		}
	}
	if area == "wiki/lessons" {
		_ = rememberName(fixed, filepath.Base(from))
	} else {
		_ = RememberPath(root, scope)
	}
	removeEmptyDirs(from)
}

func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			removeEmptyDirs(filepath.Join(dir, e.Name()))
		}
	}
	_ = os.Remove(dir) // only when empty
}

// RememberPath records a path the project had before 0.9.7 (or before a move), so its uncited source ids still check.
func RememberPath(root, scope string) error {
	dir := filepath.Join(root, "records", "wiki-sources", storeDirName)
	if _, err := os.Lstat(dir); err != nil {
		return nil
	}
	paths := legacyPaths(dir)
	for _, p := range paths {
		if p == scope {
			return nil
		}
	}
	data, _ := json.Marshal(append(paths, scope))
	return writeFile(filepath.Join(dir, legacyPathsFile), data)
}

func legacyPaths(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, legacyPathsFile))
	if err != nil {
		return nil
	}
	var paths []string
	if json.Unmarshal(data, &paths) != nil {
		return nil
	}
	return paths
}

func onlyHashNamed(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	only := ""
	for _, e := range entries {
		if e.IsDir() && scopeName.MatchString(e.Name()) {
			if only != "" {
				return ""
			}
			only = filepath.Join(dir, e.Name())
		}
	}
	return only
}

// sourceIDMatches reports whether id is the id of a source record with this content: since 0.9.7 the content's hash alone, before that the
// hash of the path it was captured under and the content (the current path, or a path the store had before).
func (b Book) sourceIDMatches(dir, id string, canonical []byte) bool {
	if digest(canonical)[:16] == id {
		return true
	}
	for _, scope := range append([]string{b.Scope}, legacyPaths(dir)...) {
		if digest(append([]byte(scope+"\n"), canonical...))[:16] == id {
			return true
		}
	}
	return false
}

// legacyNamesFile, in the pages store, lists the hash names the store had before 0.9.7. Pages written then link their sources through that
// name, and only those links are read as unchanged (a link edited to any other folder is a changed page; 0.9.7 audit B-4).
const legacyNamesFile = ".legacy-names.json"

func rememberName(dir, name string) error {
	names := legacyNames(dir)
	for _, n := range names {
		if n == name {
			return nil
		}
	}
	data, _ := json.Marshal(append(names, name))
	return writeFile(filepath.Join(dir, legacyNamesFile), data)
}

func legacyNames(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, legacyNamesFile))
	if err != nil {
		return nil
	}
	var names []string
	if json.Unmarshal(data, &names) != nil {
		return nil
	}
	out := names[:0]
	for _, n := range names {
		if scopeName.MatchString(n) {
			out = append(out, n)
		}
	}
	return out
}

// withCurrentSourceLinks turns a page's source links through a name the store had before 0.9.7 into links through the fixed name.
func (b Book) withCurrentSourceLinks(data []byte) []byte {
	dir, err := b.directory(false, "pages")
	if err != nil {
		return data
	}
	for _, name := range legacyNames(dir) {
		data = bytes.ReplaceAll(data, []byte("](../../../records/wiki-sources/"+name+"/"), []byte("](../../../records/wiki-sources/"+storeDirName+"/"))
	}
	return data
}
