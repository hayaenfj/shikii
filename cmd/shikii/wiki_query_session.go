package main

import (
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/hayaenfj/shikii/internal/rootfile"
)

// The session of a word search run from the command line. An agent puts the id of its current session in the environment of the
// commands it runs: Claude Code in CLAUDE_CODE_SESSION_ID (the id of the hook's session_id, the new one after a summary too), Codex
// in CODEX_THREAD_ID (the id its own output names), OpenCode in OPENCODE_SESSION_ID (the id its plugin passes to the hook as
// session_id). Only the command line reads them. The MCP body and the daemon behind the HTTP handler were started earlier, so what
// they hold is the session that started them (or none), not the session that asks now; their searches keep no session.
var agentSessionVariables = []string{"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID", "OPENCODE_SESSION_ID"}

// sessionVariableID is what a session id the environment holds may look like: the characters the hook's path of a session state
// keeps as they are (sessionPath turns every other character into an underscore, so two ids that differ in them would share one
// path), and no longer than a session id the hook takes (hookWorkReceipt).
var sessionVariableID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// commandMissSession is missSession of the session this command was run in, or "" when that is not certain: no variable holds an
// id of the form above that the hook kept a state for in this project (root is the project's store), or any two of the variables
// name two different sessions (none is chosen: a guess would be kept as the session). A search that finds nothing keeps it with its miss.
func commandMissSession(root string) string {
	adopted := ""
	for _, variable := range agentSessionVariables {
		id := os.Getenv(variable)
		if !sessionVariableID.MatchString(id) || !hookKeptSession(root, id) {
			continue
		}
		if adopted != "" && adopted != id {
			return ""
		}
		adopted = id
	}
	if adopted == "" {
		return ""
	}
	return missSession(adopted)
}

// hookKeptSession reports whether the hook kept the state of the session in this project's store: a regular file at the path the
// hook writes it to, opened the way every reader of that state opens it (no link is followed, and a store whose sessions folder is a
// link keeps none). It reads nothing from the file and makes no folder, file or lock.
func hookKeptSession(root, id string) bool {
	sessions, err := planningSessionRoot(root)
	if err != nil {
		return false
	}
	defer sessions.Close()
	f, err := rootfile.OpenNoFollow(sessions, filepath.Base(sessionPath(root, id)), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	return err == nil && info.Mode().IsRegular()
}
