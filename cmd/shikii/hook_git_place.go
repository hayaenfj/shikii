package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// gitPlace is what git says about the repository a folder is in: the top folder of its working tree, where its object folder and
// its info/exclude file are (absolute, whichever folder git was asked from), and the commit HEAD names ("" before the first commit).
type gitPlace struct{ top, objects, exclude, head string }

// placeAnswer is one answer of git, also a "no" (outside git): ok says which.
type placeAnswer struct {
	place gitPlace
	ok    bool
}

// placeMemo is what git answered during one hook event. One event asked git four times for the top folder (the reconnection of a
// moved folder, the project, the working tree, the guards) and once each for the object folder and the exclude file, and every ask is
// a process, and the request's own hook asked for HEAD in one more. The answers do not change inside an event (the agent does not
// commit while a hook runs), so the first one is kept. Only an answer git gave is kept: a call that
// timed out or could not start is asked again by whoever needs it, with its own time limit.
type placeMemo struct {
	mu sync.Mutex
	at map[string]placeAnswer
}

// eventPlaces is the memo of the hook event in progress (nil outside one: the CLI, the MCP server and the page ask git every time,
// as a long-running process must not keep an answer between requests).
var eventPlaces *placeMemo

// beginEventGit starts the memo for one hook event and returns the function that ends it.
func beginEventGit() func() {
	previous := eventPlaces
	eventPlaces = &placeMemo{at: map[string]placeAnswer{}}
	return func() { eventPlaces = previous }
}

// gitPlaceAt asks git (one process) for the place of the repository that dir is in. ok is false outside a working tree, as for
// `git rev-parse --show-toplevel` alone.
func gitPlaceAt(ctx context.Context, dir string) (gitPlace, bool) {
	memo := eventPlaces
	if memo != nil {
		memo.mu.Lock()
		a, known := memo.at[dir]
		memo.mu.Unlock()
		if known {
			return a.place, a.ok
		}
	}
	// --verify -q HEAD goes last: with a commit the answer is a fourth line and git exits 0; before the first commit git prints the
	// three lines before it, says nothing and exits 1; outside a working tree it prints nothing and exits 128.
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel", "--git-path", "objects", "--git-path", "info/exclude", "--verify", "-q", "HEAD")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	var exit *exec.ExitError
	ran := errors.As(err, &exit) && ctx.Err() == nil // git ran and exited with a code; not a timeout or a git that did not start
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	found := err == nil && len(lines) == 4 || ran && exit.ExitCode() == 1 && len(lines) == 3 // a commit; or none yet
	var place gitPlace
	if found {
		// git answers a relative path from the folder it was started in, which is dir here
		place = gitPlace{top: strings.TrimSpace(lines[0]), objects: atFolder(dir, strings.TrimSpace(lines[1])), exclude: atFolder(dir, strings.TrimSpace(lines[2]))}
		if len(lines) == 4 {
			place.head = strings.TrimSpace(lines[3])
		}
	}
	if memo != nil && (found || ran && exit.ExitCode() != 1) {
		memo.mu.Lock()
		memo.at[dir] = placeAnswer{place, found}
		if found {
			memo.at[place.top] = placeAnswer{place, true} // the same repository, so the same answer from its top folder
		}
		memo.mu.Unlock()
	}
	return place, found
}

func atFolder(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
