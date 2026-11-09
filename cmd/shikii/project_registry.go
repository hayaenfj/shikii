package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/hayaenfj/shikii/internal/store"
)

// registerProject writes this project's entry in the shared index, named by its store's id (0.9.7): the entry follows the store, so a
// path reused by another project, two folders swapped, or another spelling of the same path never mixes two stores' entries (re-audit 1).
// A store whose id is already the entry of another store folder that is still in place (a copy, for instance put back where the original
// was) gets an id of its own first. When the store gets its id here, the entry a released version wrote for this path (named by the path)
// is converted: it is removed. One written later, by an older shikii next to this one, is left and told (olderShikiiNotice).
func registerProject(scope string) error {
	common, err := agentHome()
	if err != nil {
		return err
	}
	root := filepath.Join(scope, ".shikii")
	dir := filepath.Join(common, "projects")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cannot open the project index")
	}
	if e, ok := ownEntry(root); ok && e.StoreDir != "" && e.StoreDir != storeDir(root) && storeDir(filepath.Join(e.Path, ".shikii")) == e.StoreDir {
		_ = os.Remove(filepath.Join(root, "records", "store-id")) // another store folder holds this id and is still in place
	}
	converting := storeID(root, false) == ""
	entry := projectEntry{ID: projectID(scope), Name: filepath.Base(scope), Path: scope, URL: "/p/" + projectID(scope) + "/", StoreID: storeID(root, true), StoreDir: storeDir(root)}
	name := entry.ID + ".json"
	if entry.StoreID != "" {
		name = entry.StoreID + ".json"
	}
	data, _ := json.Marshal(entry)
	if !unchangedProjectEntry(filepath.Join(dir, name), data) {
		tmp, err := os.CreateTemp(dir, ".project-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		_, err = tmp.Write(data)
		closeErr := tmp.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	if legacy := filepath.Join(dir, entry.ID+".json"); converting && legacy != filepath.Join(dir, name) {
		if p, ok := readEntry(legacy); ok && p.StoreID == "" && p.Path == scope {
			_ = os.Remove(legacy)
		}
	}
	return nil
}

// Only a regular, unchanged entry can avoid the atomic replacement. Links and
// files exchanged while being opened continue through the normal repair path.
func unchangedProjectEntry(path string, wanted []byte) bool {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() != int64(len(wanted)) {
		return false
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() != int64(len(wanted)) {
		return false
	}
	actual, err := io.ReadAll(io.LimitReader(f, int64(len(wanted))+1))
	if err != nil || !bytes.Equal(actual, wanted) {
		return false
	}
	current, err := os.Lstat(path)
	return err == nil && current.Mode().IsRegular() && os.SameFile(info, current) && current.Size() == info.Size() && current.ModTime().Equal(info.ModTime())
}

// upgradeIndex converts, once when shikii is updated (hook install --yes), the entries released versions wrote, named by the path (0.9.6
// and earlier): every such project gets its store id, store folder and store name now, so a move after the update is reconnected by
// itself. An entry by the path for a store that already has its id was written by an older shikii on this machine: it is left as it is,
// and its project is returned so the update can say so.
func upgradeIndex(common string) (older []string) {
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, path := range files {
		p, ok := readEntry(path)
		if !ok || p.StoreID != "" || filepath.Base(path) != p.ID+".json" || projectID(p.Path) != p.ID {
			continue
		}
		root := filepath.Join(p.Path, ".shikii")
		if active, err := projectActive(root, p.Path); err != nil || !active || checkProjectStore(root, false) != nil {
			continue
		}
		if storeID(root, false) != "" {
			older = append(older, p.Path)
			continue
		}
		store.StoreName(root, p.Path)
		if err := registerProject(p.Path); err != nil {
			fmt.Fprintf(stdout, "%s: %v\n", p.Path, err)
		}
	}
	return older
}

// olderShikiiNote says what to do when an older shikii (0.9.6 or earlier) registered a project again next to this one.
const olderShikiiNote = "an older shikii (0.9.6 or earlier) registered this project again on this machine, and it does not know this version's store and index. Update every shikii on this machine and reopen the agent sessions that use it; shikii left that entry alone"

// olderShikiiNotice is the hook's notice for olderShikiiNote: this project has its store id, and the index has an entry by its path again.
func olderShikiiNotice(scope string) string {
	common, err := agentHome()
	if err != nil {
		return ""
	}
	p, ok := readEntry(filepath.Join(common, "projects", projectID(scope)+".json"))
	if !ok || p.StoreID != "" || p.Path != scope || storeID(filepath.Join(scope, ".shikii"), false) == "" {
		return ""
	}
	return "shikii: " + olderShikiiNote + " (update every shikii). Tell the person this once, in your final reply."
}

// readEntry reads one index file (not through a link).
func readEntry(path string) (projectEntry, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return projectEntry{}, false
	}
	defer f.Close()
	var p projectEntry
	if json.NewDecoder(io.LimitReader(f, 8192)).Decode(&p) != nil || !filepath.IsAbs(p.Path) {
		return projectEntry{}, false
	}
	return p, true
}

// ownEntry is the shared index's entry for the store at root, named by its id.
func ownEntry(root string) (projectEntry, bool) {
	id := storeID(root, false)
	common, err := agentHome()
	if err != nil || id == "" {
		return projectEntry{}, false
	}
	if p, ok := readEntry(filepath.Join(common, "projects", id+".json")); ok && p.StoreID == id {
		return p, true
	}
	return projectEntry{}, false
}

// sameFolder reports whether two paths reach the same folder (another spelling, a link, another case on a case-insensitive disk).
func sameFolder(a, b string) bool {
	if a == b {
		return true
	}
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}

// storeID is the store's random id (records/store-id), made the first time the project is registered. The shared index keeps it with the
// path, so after a move the store is reconnected only on the machine where it was initialized: a .shikii brought in from elsewhere (an
// archive, another machine) never matches and stays inactive until the person runs shikii --init (0.9.7).
func storeID(root string, create bool) string {
	path := filepath.Join(root, "records", "store-id")
	read := func() string {
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return ""
		}
		defer f.Close()
		b, _ := io.ReadAll(io.LimitReader(f, 64))
		if id := strings.TrimSpace(string(b)); storeIDPattern.MatchString(id) {
			return id
		}
		return ""
	}
	if id := read(); id != "" || !create {
		return id
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	id := hex.EncodeToString(raw[:])
	for try := 0; try < 2; try++ {
		// written in full before it is in place, so a stop never leaves an empty id (confirmation audit 1, G1; 0.9.9)
		if placed, err := store.PlaceOnce(path, []byte(id+"\n"), 0o600); err != nil {
			return ""
		} else if placed {
			return id
		}
		if existing := read(); existing != "" {
			return existing // made by another process just now
		}
		if !store.LeftOver(path) {
			return "" // being written by another process just now: it wins
		}
		_ = os.Remove(path) // an id file without a valid id, left by a stop in an earlier version
	}
	return ""
}

var storeIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// storeDir is the .shikii folder's device and inode. A folder moved with mv on the same disk keeps it; a copy (cp -R) or a .shikii brought
// in from elsewhere never has the registered one.
func storeDir(root string) string {
	info, err := os.Lstat(root)
	if err != nil {
		return ""
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}

// entryFileName reports whether an index file is named as shikii names it: by the store's id (0.9.7), or by the path (before).
func entryFileName(path string, p projectEntry) bool {
	base := filepath.Base(path)
	return (p.StoreID != "" && base == p.StoreID+".json") || base == p.ID+".json"
}

func registeredProjects(common string) []projectEntry {
	projects := []projectEntry{}
	seen := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, path := range files {
		p, ok := readEntry(path)
		if !ok || seen[p.Path] || !registeredEntryActive(path, p) {
			continue
		}
		p.Name, p.URL = filepath.Base(p.Path), "/p/"+p.ID+"/"
		p.InitVersion = projectInitVersion(filepath.Join(p.Path, ".shikii"))
		seen[p.Path] = true
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Name == projects[j].Name {
			return projects[i].Path < projects[j].Path
		}
		return projects[i].Name < projects[j].Name
	})
	return projects
}

// A named request verifies only its candidate entry. This is deliberately not
// a cache: replacing the store, deleting registration, or moving the project
// must be observed before a retained handler is reused.
func registeredProjectAt(common, scope string) bool {
	if scope == "" {
		return false
	}
	root := filepath.Join(scope, ".shikii")
	id := storeID(root, false)
	if id == "" {
		id = projectID(scope)
	}
	path := filepath.Join(common, "projects", id+".json")
	p, ok := readEntry(path)
	return ok && p.Path == scope && registeredEntryActive(path, p)
}

func registeredEntryActive(path string, p projectEntry) bool {
	if feedbackScope(p.Path) != p.Path || projectID(p.Path) != p.ID {
		return false
	}
	root := filepath.Join(p.Path, ".shikii")
	id := storeID(root, false)
	if p.StoreID != "" {
		if filepath.Base(path) != p.StoreID+".json" || id != p.StoreID {
			return false
		}
		if p.StoreDir != "" && p.StoreDir != storeDir(root) {
			return false
		}
	} else if filepath.Base(path) != p.ID+".json" || id != "" {
		return false
	}
	if checkProjectStore(root, false) != nil {
		return false
	}
	active, err := projectActive(root, p.Path)
	return err == nil && active
}

// projectPageURL is the URL of this project's page (if running). If not running, "" (the hook starts it on the next request).
func projectPageURL(root string) string {
	if url := livePage(filepath.Dir(root)); url != "" {
		return url + "/"
	}
	return ""
}

// Do not accept an arbitrary store in the URL. Route only to projects the hook registered.
func newProjectHub(common string, options serveOpts) http.Handler {
	var mu sync.Mutex
	handlers := map[string]http.Handler{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" && r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"projects": registeredProjects(common), "specs": []any{}})
			return
		}
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") {
			(&server{}).handleUI(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/p/") {
			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/p/"), "/", 2)
			for _, p := range registeredProjects(common) {
				if p.ID != parts[0] {
					continue
				}
				if len(parts) == 1 {
					target := p.URL
					if r.URL.RawQuery != "" {
						target += "?" + r.URL.RawQuery
					}
					http.Redirect(w, r, target, http.StatusTemporaryRedirect)
					return
				}
				mu.Lock()
				h := handlers[p.ID]
				if h == nil {
					o := options
					o.storeDir, o.specsDir, o.wikiDir, o.projectScope = filepath.Join(p.Path, ".shikii"), "", "", p.Path
					h = http.StripPrefix(strings.TrimSuffix(p.URL, "/"), newServer(o))
					handlers[p.ID] = h
				}
				mu.Unlock()
				h.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})
}
