package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The shared side holds only the entries to projects. Conversations, scores and lines live in each .shikii/.
type projectEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	URL         string `json:"url"`
	StoreID     string `json:"store_id,omitempty"`  // the store's own id (records/store-id): a move is reconnected only for the store initialized here (0.9.7)
	StoreDir    string `json:"store_dir,omitempty"` // the .shikii folder itself (device:inode): mv on one disk keeps it, a copy never has it (0.9.7 audit B-1)
	InitVersion int    `json:"-"`                   // project.json version: 1 = --init of 0.7.4 or earlier, 2 = 0.7.5 or later (the wipe crossing 0.7.0 is done)
}

var errProjectInactive = errors.New("run shikii --init in this project")

// Not a setting: a mark that an explicit initialization finished completely. Copying it elsewhere does not activate anything.
type projectActivation struct {
	Version int    `json:"version"`
	Scope   string `json:"scope"`
}

// projectInitVersionNow is the project.json version --init writes. 2 = 0.7.5 or later (the mark that tells each project the wipe crossing 0.7.0 is "done".
// Audit R2: with only the shared mark, one refusal kept the mark from being written, and the next run wiped again a project that had already been wiped and re-initialized).
const projectInitVersionNow = 2

// projectInitVersion is the project.json version (0 if missing).
func projectInitVersion(root string) int {
	b, err := os.ReadFile(filepath.Join(root, "project.json"))
	if err != nil {
		return 0
	}
	var a projectActivation
	if json.Unmarshal(b, &a) != nil {
		return 0
	}
	return a.Version
}

// wipePending reports whether shikii's store (records/ etc.) remains with an old mark (version 1 or lower), i.e. the wipe crossing 0.7.0 has not happened yet.
// During a re-initialization in the middle of a wipe, the store holds only project.json, so it is false.
func wipePending(root string) bool {
	v := projectInitVersion(root)
	if v <= 0 || v >= projectInitVersionNow {
		return false
	}
	for name := range storeEntries {
		if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}

func projectActive(root, scope string) (bool, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("the initialization store must be a regular folder")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer dir.Close()
	opened, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return false, errors.New("the initialization store changed")
	}
	markerInfo, err := dir.Lstat("project.json")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !markerInfo.Mode().IsRegular() {
		return false, errors.New("the initialization mark must be a regular file")
	}
	f, err := dir.OpenFile("project.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || !os.SameFile(markerInfo, stat) || stat.Size() > 4096 {
		return false, errors.New("cannot check the project's initialization mark")
	}
	var activation projectActivation
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&activation); err != nil {
		return false, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return false, errors.New("the project's initialization mark is invalid")
	}
	return activation.Version >= 1 && (activation.Scope == scope || sameFolder(activation.Scope, scope)), nil
}

// version is the version written to project.json. If a new binary's --init was only rerun on a store with an old mark (version 1, 0.7.4 or earlier) and records,
// the caller (cmdInit) passes the original version: it neither raises the version nor writes the shared mark (raising it would make the next hook install --yes skip it as "already initialized by this version",
// and the wipe crossing 0.7.0 would be skipped forever. Audit R3)
func finishProjectInit(root, scope string, version int) error {
	// Even if the index is created first, the project shows up on neither the page nor the hook until the final mark exists.
	if _, err := os.UserHomeDir(); err == nil {
		if err := registerProject(scope); err != nil {
			return err
		}
	}
	// So that someone who installed with go install and ran --init before hook install is not treated as "an update from 0.6.0 or earlier" on their first hook install --yes,
	// write the current version if there is no shared mark (audit R2)
	// But do not write it when other projects are already registered (even if --init is called during the wipe crossing 0.7.0, do not pre-empt the wipe's decision)
	if agent, err := agentHome(); err == nil && version == projectInitVersionNow {
		if _, err := os.Stat(installedVersionPath(agent)); os.IsNotExist(err) {
			others := 0
			for _, p := range registeredProjects(agent) {
				if p.Path != scope {
					others++
				}
			}
			if others == 0 {
				_ = writeInstalledVersion(agent, currentVersion())
			}
		}
	}
	marker := filepath.Join(root, "project.json")
	if info, err := os.Lstat(marker); err == nil && !info.Mode().IsRegular() {
		return errors.New("the initialization mark cannot be a link")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, _ := json.Marshal(projectActivation{Version: version, Scope: scope})
	f, err := os.CreateTemp(root, ".project-init-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), marker)
}

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

func validateStoreDirectories(root string) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions", "guards", "handoff"} {
		info, err := os.Lstat(filepath.Join(root, part))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("the project store cannot be a link to a shared location")
		}
	}
	return nil
}

func currentProjectStore() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return projectStoreAt(cwd)
}

func defaultProjectStore() (string, error) {
	root, _, err := currentProjectStore()
	if err != nil {
		return "", err
	}
	cwd, _ := os.Getwd()
	if filepath.Dir(root) == feedbackScope(cwd) {
		return ".shikii", nil
	}
	return root, nil
}

// Resolve defaults only after reading all flags. Never fall back to a refused store through a relative path.
func resolveStoreFlag(fs *flag.FlagSet) error {
	f := fs.Lookup("store")
	if f == nil {
		return nil
	}
	path := f.Value.String()
	if path == ".shikii" {
		var err error
		path, err = defaultProjectStore()
		if err != nil {
			return err
		}
	}
	if err := validateStoreDirectories(path); err != nil {
		return err
	}
	return f.Value.Set(path)
}

// Never write individual conversations or calibrations into a symlinked shared location.
func checkProjectStore(root string, create bool) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions", "guards", "handoff"} {
		path := filepath.Join(root, part)
		if create {
			if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) && part != "" && !create {
				continue // a store made by an old version (0.6.0 - 0.7.2 have no guards/) also counts as registered. Missing folders are created when used
			}
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("the project's .shikii and store must be regular folders (links to shared locations are not used)")
		}
	}
	return nil
}

// Keep personal conversations out of git add -A. The handling of already-tracked files does not change.
func excludeProjectStore(scope string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "info/exclude")
	cmd.Dir = scope
	raw, err := cmd.Output()
	if err != nil {
		return nil
	} // outside git
	path := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(path) {
		path = filepath.Join(scope, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	if bytes.Contains(append([]byte("\n"), data...), []byte("\n/.shikii/\n")) {
		return nil
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err = f.WriteString("\n/.shikii/\n")
	return err
}

func prepareHookProject(cwd string) (string, string, error) {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return "", "", err
	}
	active, err := projectActive(root, scope)
	if err != nil {
		return "", "", err
	}
	if !active {
		return "", "", errProjectInactive
	}
	if err := excludeProjectStore(scope); err != nil {
		return "", "", err
	}
	if err := checkProjectStore(root, true); err != nil {
		return "", "", err
	}
	if _, err := seedAgentSpecs(filepath.Join(root, "decisions")); err != nil {
		return "", "", err
	}
	if _, err := seedWiki(filepath.Join(root, "wiki")); err != nil {
		return "", "", err
	}
	if err := registerProject(scope); err != nil {
		return "", "", err
	}
	return root, scope, nil
}

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
	if legacy := filepath.Join(dir, entry.ID+".json"); converting && legacy != filepath.Join(dir, name) {
		if p, ok := readEntry(legacy); ok && p.StoreID == "" && p.Path == scope {
			_ = os.Remove(legacy)
		}
	}
	return nil
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
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			return ""
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
		if !ok || feedbackScope(p.Path) != p.Path || projectID(p.Path) != p.ID || seen[p.Path] {
			continue
		}
		// the store id's entry, or the entry a released version wrote by the path for a store not converted yet (upgradeIndex)
		if byID := p.StoreID != "" && filepath.Base(path) == p.StoreID+".json"; !byID && (p.StoreID != "" || filepath.Base(path) != p.ID+".json" || storeID(filepath.Join(p.Path, ".shikii"), false) != "") {
			continue
		}
		if checkProjectStore(filepath.Join(p.Path, ".shikii"), false) != nil {
			continue
		}
		if active, err := projectActive(filepath.Join(p.Path, ".shikii"), p.Path); err != nil || !active {
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
