package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// gitPlace is what git says about the repository a folder is in: the top folder of its working tree, where its object folder and
// its info/exclude file are (absolute, whichever folder git was asked from), and the commit HEAD names ("" before the first commit, when
// headKnown).
type gitPlace struct {
	top, objects, exclude, head string
	headKnown                   bool   // head is what git said just now (a place kept from an earlier event has none: every commit changes it)
	gitDir, commonDir           string // where this working tree's HEAD is and where the refs it shares are (layoutAt, and the same as git said); "" when the files do not say (headFromFiles can read HEAD where they do)
}

// placeAnswer is one answer of git, also a "no" (outside git): ok says which.
type placeAnswer struct {
	place gitPlace
	ok    bool
}

// placeMemo is what git answered during one hook event. One event asked git four times for the top folder (the reconnection of a
// moved folder, the project, the working tree, the guards) and once each for the object folder and the exclude file, and every ask is
// a process, and the request's own hook asked for HEAD in one more. The answers do not change inside an event (the agent does not
// commit while a hook runs), so the first one is kept. Only an answer git gave is kept: a call that
// timed out or could not start is asked again by whoever needs it, with what is left of the time the event may wait for git (waited): an
// event whose questions got no answer for hookGitTimeout in all asks no more. Without this a git that never answered made each step of
// the event wait out a limit of its own, one after the other, past the time the agent gives a hook. Every question that can be asked
// again after one that got no answer goes through ask: the place of the repository, and the two questions asked alone when git says
// there is no working tree (the exclude file in excludeProjectStore, HEAD in gitHead).
type placeMemo struct {
	mu     sync.Mutex
	at     map[string]placeAnswer
	waited time.Duration // the time spent on questions that got no answer; the time a question took that was answered is not in it
}

// timeLeft is the time a question to git may still take in this event: hookGitTimeout less what the questions with no answer waited. ok
// is false when less than a tenth of hookGitTimeout is left: git cannot be started and heard from in that, and the few moments that
// remain after a caller's own limit ran out are not a reason to start it again.
func (m *placeMemo) timeLeft() (left time.Duration, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	left = hookGitTimeout - m.waited
	return left, left > hookGitTimeout/10
}

// spend adds the time a question waited without an answer to what the event has spent.
func (m *placeMemo) spend(waited time.Duration) {
	m.mu.Lock()
	m.waited += waited
	m.mu.Unlock()
}

// ask runs one question to git for this event: run is given the caller's limit cut to the time the event has left, and says whether git
// answered. An answer (also a "no": an exit code of git) costs the event nothing, so a later question about another folder is not refused
// because the event has been running for long; a question that ran out of time or could not start adds what it waited to the time the
// event has spent. ask returns false, with git not started, when too little time is left. A nil memo is no event (the CLI, the MCP
// server): run is given the caller's own limit and nothing is counted.
func (m *placeMemo) ask(ctx context.Context, run func(context.Context) (answered bool)) bool {
	if m == nil {
		run(ctx)
		return true
	}
	left, ok := m.timeLeft()
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, left)
	defer cancel()
	started := time.Now()
	if !run(ctx) {
		m.spend(time.Since(started))
	}
	return true
}

// gitAnswered says whether a finished git command is an answer: it ran and exited (with any code), as opposed to a timeout, a limit that
// was over, or a git that did not start.
func gitAnswered(ctx context.Context, err error) bool {
	var exit *exec.ExitError
	return err == nil || errors.As(err, &exit) && ctx.Err() == nil
}

// placeCacheTTL is how long a hook event may rely on the place a hook before it got from git (kept between events).
var placeCacheTTL = 30 * time.Minute

// eventPlaces is the memo of the hook event in progress (nil outside one: the CLI, the MCP server and the page keep no answer in memory,
// as a long-running process must not; they use the kept answer of keptPlace, which is checked against the folders at every use).
var eventPlaces *placeMemo

// beginEventGit starts the memo for one hook event and returns the function that ends it.
func beginEventGit() func() {
	previous := eventPlaces
	eventPlaces = &placeMemo{at: map[string]placeAnswer{}}
	return func() { eventPlaces = previous }
}

// gitPlaceAt gives the place of the repository that dir is in. ok is false outside a working tree, as for
// `git rev-parse --show-toplevel` alone. The folders above dir say a good deal without a process (walkUpForGit): that none of them is
// or can be a repository (then git would say no), or that the nearest `.git` is the one git answered about a moment ago (keptPlace,
// checked against the folders at every use, so also a CLI, an MCP server or a page that lives long can use it). Otherwise git is asked,
// once.
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
	walk, walked := walkUpForGit(dir)
	if walked && walk.at == "" {
		memo.keep(dir, gitPlace{}, false)
		return gitPlace{}, false
	}
	if walked {
		if place, ok := keptPlace(walk); ok {
			memo.keep(dir, place, true)
			return place, true
		}
	}
	// what the files say before git is asked: an answer is kept only where git says the same, and is used only while the files still say
	// what they did before git was asked
	var before placeSnapshot
	seen := false
	if walked {
		before, seen = snapshotAt(walk.at)
	}
	// git answers a relative path from the folder it runs in, which is the real folder whichever path reached it (a link to a subfolder)
	base := physicalFolder(dir)
	var place gitPlace
	found, answered := false, false
	if !memo.ask(ctx, func(ctx context.Context) bool {
		place, found, answered = askPlace(ctx, dir, base)
		return answered
	}) {
		return gitPlace{}, false // no time left for another question without an answer: not asked, and nothing is kept
	}
	// what the files say is trusted (kept for the next events, and HEAD read from them) only where it is what git said just now
	trusted := false
	if found && seen && sameFolder(walk.at, place.top) {
		// (the snapshot is the one taken before git was asked: a write while git answered makes the next use differ, and git is asked again)
		if layoutAgrees(before.layout, place) {
			place.gitDir, place.commonDir = before.layout.gitDir, before.layout.commonDir
			trusted = true
		}
	}
	if memo != nil && answered {
		memo.keep(dir, place, found)
	}
	if trusted {
		keepPlace(walk, place, before)
	}
	return place, found
}

// askPlace asks git where the repository is, once, from the folder dir (base is its real path). answered is true when git answered, also
// with "no": not a timeout and not a git that did not start.
func askPlace(ctx context.Context, dir, base string) (place gitPlace, found, answered bool) {
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
	switch {
	case err == nil && len(lines) == 4: // a commit
		place, found = gitPlace{top: strings.TrimSpace(lines[0]), objects: atFolder(base, strings.TrimSpace(lines[1])), exclude: atFolder(base, strings.TrimSpace(lines[2])), head: strings.TrimSpace(lines[3]), headKnown: true}, true
	case ran && exit.ExitCode() == 1 && len(lines) == 3: // none yet
		place, found = gitPlace{top: strings.TrimSpace(lines[0]), objects: atFolder(base, strings.TrimSpace(lines[1])), exclude: atFolder(base, strings.TrimSpace(lines[2])), headKnown: true}, true
	case err == nil || ran && exit.ExitCode() == 1:
		// git answered, but not in the lines expected: a folder name with a line end in it. Asked one by one, as before they were put together.
		place, found = placeApart(ctx, dir, base)
	}
	return place, found, found || ran && exit.ExitCode() != 1
}

// keep remembers an answer for the rest of the event (nil: outside one, nothing is kept), under dir and, as it is the same repository,
// under the top folder too.
func (m *placeMemo) keep(dir string, place gitPlace, found bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.at[dir] = placeAnswer{place, found}
	if found {
		m.at[place.top] = placeAnswer{place, true}
	}
}

func atFolder(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// absFolder is dir as an absolute path, not cleaned: `..` after a link names the folder above what the link points to, which is not the
// folder above the link (filepath.Abs would fold the two away). EvalSymlinks then walks the path the way the system does.
func absFolder(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return cwd + string(filepath.Separator) + dir, nil
}

// physicalFolder is dir as an absolute path with its links resolved: the folder git itself works in.
func physicalFolder(dir string) string {
	if abs, err := absFolder(dir); err == nil {
		dir = abs
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return dir
}

// placeApart asks git the three questions one by one (the top folder, the object folder, the exclude file), for a folder whose name has a
// line end in it: the lines of the answer put together cannot be told apart then. HEAD is left unknown.
func placeApart(ctx context.Context, dir, base string) (gitPlace, bool) {
	ask := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"rev-parse"}, args...)...)
		cmd.Dir = dir
		cmd.Stderr = io.Discard
		out, err := cmd.Output()
		return strings.TrimSuffix(string(out), "\n"), err == nil
	}
	top, ok1 := ask("--show-toplevel")
	objects, ok2 := ask("--git-path", "objects")
	exclude, ok3 := ask("--git-path", "info/exclude")
	if !ok1 || !ok2 || !ok3 || top == "" {
		return gitPlace{}, false
	}
	return gitPlace{top: top, objects: atFolder(base, objects), exclude: atFolder(base, exclude)}, true
}

// gitWalk is what the folders from a folder upward say without a process.
type gitWalk struct {
	at string // the nearest folder that holds a `.git` (or the files of a bare repository); "" when none does, up to the top
}

// deviceOfFolder is the device a folder is on; a folder on another device than the one below it is a mount point. It is a variable so a
// test can put a mount point where a test cannot mount one.
var deviceOfFolder = func(_ string, info os.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

// walkUpForGit looks for the repository as git does first: a `.git` in the folder and in each one above it (the physical path), up to
// the top, a filesystem boundary or a ceiling. ok is false where git has to say: the environment tells git where the repository is, a
// folder cannot be read, or a boundary is crossed.
func walkUpForGit(dir string) (gitWalk, bool) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY"} {
		if _, set := os.LookupEnv(name); set { // also set to nothing: git reads an empty one of these as an error
			return gitWalk{}, false
		}
	}
	for _, name := range []string{"GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_NAMESPACE"} {
		if os.Getenv(name) != "" { // set to nothing, these are no setting to git (some tools export them so)
			return gitWalk{}, false
		}
	}
	ceilings, ok := gitCeilings()
	if !ok {
		return gitWalk{}, false
	}
	abs, err := absFolder(dir)
	if err != nil {
		return gitWalk{}, false
	}
	start, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return gitWalk{}, false
	}
	var device uint64
	for d := start; ; d = filepath.Dir(d) {
		if d != start && ceilings[d] {
			return gitWalk{}, true // git does not search a ceiling folder, nor any above it (the folder asked about is always searched)
		}
		info, err := os.Lstat(d)
		if err != nil {
			return gitWalk{}, false
		}
		if dev, ok := deviceOfFolder(d, info); ok {
			if d != start && dev != device {
				return gitWalk{}, false // git stops at a mount point unless told otherwise: left to git
			}
			device = dev
		}
		switch _, err := os.Lstat(filepath.Join(d, ".git")); {
		case err == nil:
			return gitWalk{at: d}, true
		case !errors.Is(err, fs.ErrNotExist):
			return gitWalk{}, false
		}
		// HEAD may be a link (an old way of writing it, to a branch that has no commit yet), and objects may be one: git follows them
		if head, err := os.Lstat(filepath.Join(d, "HEAD")); err == nil && (head.Mode().IsRegular() || head.Mode()&os.ModeSymlink != 0) {
			if objects, err := os.Stat(filepath.Join(d, "objects")); err == nil && objects.IsDir() {
				return gitWalk{at: d}, true // the files of a bare repository: git says what that is
			}
		}
		if parent := filepath.Dir(d); parent == d {
			return gitWalk{}, true
		}
	}
}

// gitCeilings is GIT_CEILING_DIRECTORIES as git reads it: absolute folders, resolved. ok is false for a list git reads another way (an
// empty entry says the ones after it are not to be resolved).
func gitCeilings() (map[string]bool, bool) {
	list := os.Getenv("GIT_CEILING_DIRECTORIES")
	out := map[string]bool{}
	if list == "" {
		return out, true
	}
	for _, c := range strings.Split(list, string(os.PathListSeparator)) {
		if c == "" {
			return nil, false
		}
		if !filepath.IsAbs(c) {
			continue
		}
		if r, err := filepath.EvalSymlinks(c); err == nil {
			out[r] = true
		}
	}
	return out, true
}

// placeEntry is what git answered about a repository, as kept between events: the top folder, when, and what the files said when git
// answered: the git folders, and the state of the configurations (placeSnapshot). A kept answer is used only while the files still say so.
type placeEntry struct {
	Top      string `json:"top"`
	At       int64  `json:"at"`
	Git      string `json:"git,omitempty"`
	Common   string `json:"common,omitempty"`
	Config   fileID `json:"config"`
	WtConfig fileID `json:"wt_config"`
}

func placeEntryFile(at string) (string, bool) {
	agent, err := agentHome()
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(at))
	return filepath.Join(agent, "places", hex.EncodeToString(sum[:])[:16]+".json"), true
}

// keptPlace is the answer git gave about the repository whose `.git` the walk found, when it can still be trusted: the files say where
// the object folder and the exclude file are (layoutAt: a `.git` folder, or a `.git` file that names the git folder of a worktree, a
// submodule or a working tree made with --separate-git-dir), the git folders are the ones the answer was given for, git's answer is
// less than placeCacheTTL old, and the folders as they were. What the answer cannot hold, HEAD, is left unknown.
func keptPlace(w gitWalk) (gitPlace, bool) {
	file, ok := placeEntryFile(w.at)
	if !ok {
		return gitPlace{}, false
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return gitPlace{}, false
	}
	var e placeEntry
	if json.Unmarshal(raw, &e) != nil || e.Top == "" {
		return gitPlace{}, false
	}
	now, ok := snapshotAt(w.at)
	if !ok || !keptEntryAgrees(e, w.at, now) {
		// not the repository git answered about, an answer too old, or a configuration written since git answered (core.bare,
		// core.worktree and the like may have changed what it would say)
		return gitPlace{}, false
	}
	layout := now.layout
	return gitPlace{top: e.Top, objects: layout.objects(), exclude: layout.exclude(), gitDir: layout.gitDir, commonDir: layout.commonDir}, true
}

// keepPlace keeps what git answered for the next events (best effort: a failure only means git is asked again). Only for a repository
// whose top folder is the one with the `.git` (a work tree set elsewhere is told by git every time).
func keepPlace(w gitWalk, place gitPlace, seen placeSnapshot) {
	file, ok := placeEntryFile(w.at)
	if !ok || !sameFolder(place.top, w.at) {
		return
	}
	raw, err := json.Marshal(placeEntry{Top: place.top, At: time.Now().Unix(), Git: seen.layout.gitDir, Common: seen.layout.commonDir, Config: seen.config, WtConfig: seen.wtConfig})
	if err != nil || os.MkdirAll(filepath.Dir(file), 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".place-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(raw)
	if cerr := tmp.Close(); werr != nil || cerr != nil || os.Rename(tmp.Name(), file) != nil {
		os.Remove(tmp.Name())
		return
	}
	forgetOldPlaces(filepath.Dir(file))
}

// placeKeptFor is how long a file of kept answers stays when nothing writes it again: a repository nobody works in any more.
const placeKeptFor = 7 * 24 * time.Hour

// forgetOldPlaces removes the files of kept answers that were last written more than placeKeptFor ago (best effort).
func forgetOldPlaces(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > placeKeptFor {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// outsideGit reports that nothing above dir is, or can be, a repository (git would say so too): no process is needed to hear it.
func outsideGit(dir string) bool {
	w, ok := walkUpForGit(dir)
	return ok && w.at == ""
}

// headFromFiles reads HEAD from the files of a repository, as git does for the files backend: HEAD (in gitDir, the folder of this working
// tree) names a ref (or is a commit, detached); the ref is a file of its own or a line of packed-refs, both in commonDir (where the
// refs of the repository are: the same folder as gitDir unless this is a linked worktree); a ref that is neither is a branch before its
// first commit. known is false where the files are not the whole story (a reftable, a symbolic ref, a name outside refs/, a ref that
// belongs to a worktree alone while the folders differ): git says.
func headFromFiles(gitDir, commonDir string) (head string, known bool) {
	for _, dir := range []string{gitDir, commonDir} {
		if _, err := os.Lstat(filepath.Join(dir, "reftable")); !errors.Is(err, fs.ErrNotExist) {
			return "", false
		}
	}
	if info, err := os.Lstat(filepath.Join(gitDir, "HEAD")); err != nil || !info.Mode().IsRegular() {
		return "", false // a link in its place (an old way of writing it) is read by git another way
	}
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	content := strings.TrimSpace(string(raw))
	if isObjectID(content) {
		return content, true // detached
	}
	name, ok := strings.CutPrefix(content, "ref: ")
	if !ok || !strings.HasPrefix(name, "refs/") || strings.Contains(name, "..") || path.Clean(name) != name {
		return "", false // a name git does not write (a doubled or trailing slash, a dot step): git says
	}
	if gitDir != commonDir && isPerWorktreeRef(name) {
		return "", false // the file is in the folder of the worktree, not the shared one
	}
	ref := filepath.Join(commonDir, filepath.FromSlash(name))
	if info, err := os.Lstat(ref); err == nil && !info.Mode().IsRegular() {
		return "", false // a link (a symbolic ref written the old way, which git follows from the folder it is in) or a folder: git says
	}
	loose, err := os.ReadFile(ref)
	switch {
	case err == nil:
		if v := strings.TrimSpace(string(loose)); isObjectID(v) {
			return v, true
		}
		return "", false
	case !errors.Is(err, fs.ErrNotExist):
		return "", false
	}
	packed, err := os.ReadFile(filepath.Join(commonDir, "packed-refs"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", true // a branch before its first commit
	}
	if err != nil {
		return "", false
	}
	if bytes.Contains(packed, []byte("\r")) {
		return "", false // not written by git here: git may read it another way
	}
	for _, line := range strings.Split(string(packed), "\n") {
		if id, ref, ok := strings.Cut(line, " "); ok && ref == name {
			return id, isObjectID(id) // an id git writes (lower case); any other is left to git
		}
	}
	return "", true
}

// isObjectID reports whether s is the hex name of an object (40 digits, or 64 in a repository that hashes with sha256).
func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
