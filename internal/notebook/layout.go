package notebook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hayaenfj/shikii/internal/store"
)

// The wiki and its sources are kept under the store's name (store.StoreName): the hash of the path the store was first opened at, written
// once and never changed, so moving the folder renames nothing and nothing is moved or merged (0.9.7, after the audits kept finding ways
// the moving and merging lost or broke records). legacyPathsFile, in the sources area, lists the paths the project had before, which the
// source ids captured before 0.9.7 hold.
const legacyPathsFile = ".legacy-paths.json"

func (b Book) storeName() string { return store.StoreName(b.Root, b.Scope) }

// RememberPath records a path the project had before 0.9.7 (or before a move), so its uncited source ids still check.
func RememberPath(root, scope string) error {
	dir := filepath.Join(root, "records", "wiki-sources", store.StoreName(root, scope))
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

// legacyNamesFile, in the pages area of a store a 0.9.7 build in development moved to "project", lists the names it had before; pages
// written then link their sources through those names, and only those links are read as unchanged (audit B-4).
const legacyNamesFile = ".legacy-names.json"

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
		data = bytes.ReplaceAll(data, []byte("](../../../records/wiki-sources/"+name+"/"), []byte("](../../../records/wiki-sources/"+b.storeName()+"/"))
	}
	return data
}
