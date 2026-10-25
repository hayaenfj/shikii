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

// Notices after the folder was moved or renamed (0.9.2). The store name and the init marker are decided by the project's path, so
// just moving it made the hook stop silently, and fixing only the marker made the wiki silently empty. How to fix it is in the skill's references/wiki.md.

const movedHow = "fix it with the steps in \"Moving or renaming the project folder\" in the skill's references/wiki.md (check with the person first; copy the old store instead of deleting it)."

// movedMarkerNotice is the notice for when this folder's init marker still has the previous path and that path no longer exists (the folder was moved).
// It stays silent when the previous path exists (it was copied) or cannot be read (a copied marker is not made active, as before).
func movedMarkerNotice(cwd string) string {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return ""
	}
	f, err := os.OpenFile(filepath.Join(root, "project.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	var a projectActivation
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&a) != nil || a.Scope == "" || a.Scope == scope || !filepath.IsAbs(a.Scope) {
		return ""
	}
	if _, err := os.Lstat(a.Scope); !os.IsNotExist(err) {
		return ""
	}
	return fmt.Sprintf("shikii: this folder's .shikii/project.json still carries the marker of an init at the previous path (%s), so shikii is not running (lesson delivery, decisions and records have stopped). The folder was moved or renamed. When you finish the current request, %s", a.Scope, movedHow)
}

// otherPathNotice is the notice for when this project's store has a wiki store left under the previous path's name.
func otherPathNotice(agent, scope string) string {
	stores, err := (notebook.Book{Root: agent, Scope: scope}).OtherPathStores()
	if err != nil || len(stores) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: .shikii has a wiki store left under the previous path's name (%s). The folder was moved or renamed, and its pages and sources do not show up in the current wiki. When you finish the current request, %s", strings.Join(stores, ", "), movedHow)
}
