package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
		}
		if known && area == "records/wiki-sources" {
			if err := RememberPath(root, scope); err != nil {
				return err
			}
		}
	}
	return nil
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

// legacySourceLink is a page's link to a source as written before 0.9.7, through the hash-named store. A page written then is still unchanged.
var legacySourceLink = regexp.MustCompile(`\]\(\.\./\.\./\.\./records/wiki-sources/[0-9a-f]{64}/`)
