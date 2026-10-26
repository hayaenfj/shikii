package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hayaenfj/shikii/internal/notebook"
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
// nothing was done). The old path is remembered (source ids captured before 0.9.7 hold it), project.json and the shared index are pointed
// here, and the old path's page is stopped. A copy is left alone (copiedNotice); after shikii --init it is a project of its own.
func relinkMovedProject(cwd string, takeOverCopy bool) string {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return ""
	}
	old, version, ok := previousScope(root, scope)
	if !ok || (!takeOverCopy && (copiedFrom(old, scope) || !registeredHere(old, root))) {
		return "" // a copy, or a store this machine did not initialize at that path: only the person's shikii --init activates it
	}
	if err := notebook.MigrateLayout(root, old); err != nil {
		hookLog("relink: " + err.Error())
		return ""
	}
	if err := notebook.RememberPath(root, old); err != nil {
		hookLog("relink: " + err.Error())
		return ""
	}
	if err := finishProjectInit(root, scope, version); err != nil {
		hookLog("relink: " + err.Error())
		return ""
	}
	if !copiedFrom(old, scope) {
		forgetProject(old)
	}
	return fmt.Sprintf("shikii: this project's folder was moved or renamed from %s. shikii reconnected it here by itself: its wiki, records and settings are kept, and nothing needs fixing. Tell the person this once.", old)
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

// copiedNotice is the notice for a folder whose .shikii was copied from a project that is still there: shikii is not running here.
func copiedNotice(cwd string) string {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return ""
	}
	old, _, ok := previousScope(root, scope)
	if !ok || !copiedFrom(old, scope) {
		return ""
	}
	return fmt.Sprintf("shikii: this folder's .shikii was copied from %s, which is still a shikii project, so shikii is not running here. To use this copy as a project of its own, ask the person, then run `shikii --init` here (its wiki and records are kept).", old)
}

// otherPathNotice is the notice for when this project's store has a wiki store left under the previous path's name.
func otherPathNotice(agent, scope string) string {
	stores, err := (notebook.Book{Root: agent, Scope: scope}).OtherPathStores()
	if err != nil || len(stores) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: .shikii has a wiki store left under the previous path's name (%s). The folder was moved or renamed, and its pages and sources do not show up in the current wiki. When you finish the current request, %s", strings.Join(stores, ", "), movedHow)
}
