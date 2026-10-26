package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// After the project folder is moved or renamed (0.9.7). Its .shikii keeps everything under fixed names, so only project.json (the path it
// was initialized at) and the shared index (~/.shikii/agent) still point at the old path. Before, this stopped shikii until a person followed
// eleven manual steps; now the request's hook (and shikii --init) reconnects the project by itself.

const movedHow = "copy its missing files into the store's project folder as \"Moving or renaming the project folder\" in the skill's references/wiki.md says (check with the person first; keep the old store, do not delete it)."

// previousScope is the path in this store's project.json when it differs from the current one, and the project.json version.
func previousScope(root, scope string) (string, int, bool) {
	f, err := os.OpenFile(filepath.Join(root, "project.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", 0, false
	}
	defer f.Close()
	var a projectActivation
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&a) != nil || a.Version < 1 || a.Scope == "" || a.Scope == scope || !filepath.IsAbs(a.Scope) {
		return "", 0, false
	}
	return a.Scope, a.Version, true
}

// copiedFrom reports whether the previous path is still that same project: the folder was copied, not moved. The same folder reached by
// another path (a link) is not a copy.
func copiedFrom(old, scope string) bool {
	if a, err := os.Stat(old); err == nil {
		if b, err := os.Stat(scope); err == nil && os.SameFile(a, b) {
			return false
		}
	}
	active, err := projectActive(filepath.Join(old, ".shikii"), old)
	return err == nil && active
}

// relinkMovedProject reconnects the project at cwd when its folder was moved or renamed, and returns what to tell the person once ("" when
// nothing was done). It acts only when this machine's index has the old path as this very store (registeredHere) and the old path is not a
// shikii project any more. The old path is remembered (source ids captured before 0.9.7 hold it), project.json and the shared index are
// pointed here, and the old path's page is stopped. Anything else (a copy, a project moved before 0.9.7, a .shikii brought in) is left to
// the person: notRunningNotice says so, and shikii --init (takeOver) makes it a project here.
func relinkMovedProject(cwd string, takeOver bool) string {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return ""
	}
	if _, _, ok := previousScope(root, scope); !ok {
		return ""
	}
	// one reconnection at a time per store: two hooks (two sessions) at once told the move twice (0.9.7 full audit 1-3, 3-3)
	unlock, err := lockRelink(root)
	if err != nil {
		return ""
	}
	defer unlock()
	old, version, ok := previousScope(root, scope) // read again under the lock: another hook may have reconnected it
	if !ok {
		return ""
	}
	copied := copiedFrom(old, scope)
	moved := !copied && registeredHere(old, root)
	// A move to another disk (mv copies there, so the store folder changed) cannot be told from a copy of a moved original that has not
	// been reconnected yet: both have this store's id and a gone old path. --init there makes a project of its own with a new id and keeps
	// the old entry, so the original, wherever it is, can still reconnect (the property test found the takeover, 0.9.7 full audit 1-6).
	if !moved && !takeOver {
		return "" // only the person's shikii --init activates it
	}
	if err := notebook.MigrateLayout(root, old); err != nil {
		hookLog("relink: " + err.Error())
		return ""
	}
	(store.Store{Dir: root}).MigrateLayout(old)
	if err := notebook.RememberPath(root, old); err != nil {
		hookLog("relink: " + err.Error())
		return ""
	}
	if !moved {
		// a copy or a .shikii from elsewhere becomes a project of its own: a new store id, so it never matches the original's entry
		_ = os.Remove(filepath.Join(root, "records", "store-id"))
	}
	if err := finishProjectInit(root, scope, version); err != nil {
		hookLog("relink: " + err.Error())
		return ""
	}
	if !moved {
		return fmt.Sprintf("Made this folder a project of its own (its .shikii came from %s; the wiki and records are kept)", old)
	}
	forgetProject(old)
	return fmt.Sprintf("shikii: this project's folder was moved or renamed from %s. shikii reconnected it here by itself: its wiki, records and settings are kept, and nothing needs fixing. Tell the person this once, in your final reply.", old)
}

// lockRelink takes the reconnection lock of the store at root, waiting for another hook. It lives on the shared side
// (~/.shikii/agent/relink-<id>.lock), since nothing is written into a store that may be a copy.
func lockRelink(root string) (func(), error) {
	common, err := agentHome()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(common, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(common, "relink-"+projectID(root)+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// forgetProject removes the old path's entry from the shared index and stops the page shikii started for it (only that serve, checked by its command line).
func forgetProject(old string) {
	common, err := agentHome()
	if err != nil {
		return
	}
	entry := filepath.Join(common, "projects", projectID(old)+".json")
	if b, err := os.ReadFile(entry); err == nil {
		var p projectEntry
		if json.Unmarshal(b, &p) == nil && p.Path == old {
			_ = os.Remove(entry)
		}
	}
	if r, ok := readPageRecord(common, old); ok && r.Scope == old {
		if r.PID > 0 && r.PID != os.Getpid() && processAlive(r.PID) && isShikiiServe(processCommand(r.PID), filepath.Join(old, ".shikii")) {
			_ = stopProcess(r.PID)
		}
		_ = os.Remove(pagePath(common, old))
	}
}

// copiedNotice is the notice for a folder whose .shikii names another path that relinkMovedProject did not take: a copy whose original
// is still there, or a store this machine cannot confirm as that project moved here. shikii is not running in it.
func copiedNotice(cwd string) string {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return ""
	}
	old, _, ok := previousScope(root, scope)
	if !ok {
		return ""
	}
	if copiedFrom(old, scope) {
		return fmt.Sprintf("shikii: this folder's .shikii was copied from %s, which is still a shikii project, so shikii is not running here (its records keep going to the original). Tell the person this in your final reply, and ask once whether to make this copy a project of its own with `shikii --init` here (its wiki and records are kept); do not ask again in later replies.", old)
	}
	return fmt.Sprintf("shikii: this folder's .shikii was initialized at %s, and shikii cannot confirm that this folder is that project moved here (it may be a copy, or brought in from elsewhere), so shikii is not running here. Tell the person this in your final reply, and ask once whether to make it a project here with `shikii --init` (its wiki and records are kept); do not ask again in later replies.", old)
}

// otherPathNotice is the notice for when this project's store has a wiki store left under the previous path's name.
func otherPathNotice(agent, scope string) string {
	stores, err := (notebook.Book{Root: agent, Scope: scope}).OtherPathStores()
	if err != nil || len(stores) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: .shikii has a wiki store under another name (%s), left by a copy made by hand before 0.9.7 or written by an older shikii; some of its pages and sources do not show up in the current wiki. When you finish the current request, %s Tell the person this in your final reply.", strings.Join(stores, ", "), movedHow)
}

// noticeOnce reports whether this session has not been told yet about the folder at scope (a copy or an unconfirmed store). That folder is
// not a project, so nothing is written into its store: the shared side keeps the last few hundred (session, folder) pairs (0.9.7 real-agent check).
func noticeOnce(session, scope string) bool {
	common, err := agentHome()
	if err != nil || session == "" {
		return true
	}
	path := filepath.Join(common, "told.json")
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return true
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		return true
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	sum := sha256.Sum256([]byte(session + "\n" + scope))
	key := hex.EncodeToString(sum[:8])
	var told []string
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &told)
	}
	for _, k := range told {
		if k == key {
			return false
		}
	}
	told = append(told, key)
	if len(told) > 500 {
		told = told[len(told)-500:]
	}
	b, _ := json.Marshal(told)
	_ = writeAtomic(path, b)
	return true
}
