package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The lines a forbidden-words guard stopped. A stopped line that was not removed or committed is never a line "from before the
// request", whichever session, turn or start compares the file and however the content at the start is read (the blob the
// listing names, a staged file or a stash included; the kept table of the uncommitted lines; the request's HEAD). So the
// record is one per repository (scope), outside the sessions: records/stopped-lines/<sha256 of the scope>.stopped, holding per
// file the keyed hash (HMAC-SHA256, with a random key made for that record alone) of each stopped line and how many copies were
// stopped, never the line itself, written 0600.
//
// Every comparison counts, of the lines of the current file, the copies the record holds as added lines: the person's lines
// are only the lines that were there at the start and are not in the record (a line there n times at the start and stopped m
// times is the person's n-m times). A record entry counts at most the copies of the current file beyond those in the commit
// that was HEAD at the start, so a stopped line goes once it is removed from the file or was committed before the turn; a
// commit the turn itself makes does not clear it for that turn.
//
// Only Stops write the record (a request's, a slash command's stretch's): under the record's lock they read it, add the lines
// the failing forbidden-words guards stopped, drop the copies that were removed or committed and write it. Nothing else drops
// an entry: a Stop that passes, or that did not compare the file, keeps it. When the lock cannot be taken the Stop writes
// nothing and goes on. Past keptLinesMax the entries already kept stay and the new ones that do not fit are dropped, the files
// with the fewest new entries first in, so one huge file does not push out the small ones.

const stoppedMarker = "#stopped2 "

// stoppedLinesSeam, when set (tests), is called at the steps of a Stop's write: "before-lock" after the comparison and before
// the lock is taken, "locked" right after the record was read under the lock.
var stoppedLinesSeam func(step string)

// stoppedLinesPath is the record of the lines forbidden-words guards stopped in the repository scope.
func stoppedLinesPath(root, scope string) string {
	return filepath.Join(root, "records", "stopped-lines", projectID(scope)+".stopped")
}

// stoppedTable is a record read back: its repository, key and, per file (the bytes of the path, as gitPath gives them), how
// many copies of each keyed hash were stopped.
type stoppedTable struct {
	scope string
	key   []byte
	files map[string]map[[16]byte]int
	raw   []byte // the file as it was read (nil for a new record)
}

// readStoppedTable reads the record at p when it belongs to scope; nil when there is none, it is not a regular file, it is
// larger than any record written, or it is another repository's.
func readStoppedTable(p, scope string) *stoppedTable {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > keptLinesMax {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, keptLinesMax+1))
	if err != nil || len(b) > keptLinesMax || !bytes.HasPrefix(b, []byte(stoppedMarker)) {
		return nil
	}
	head, body, found := bytes.Cut(b[len(stoppedMarker):], []byte("\n"))
	if !found {
		return nil
	}
	scopeHex, keyHex, found := strings.Cut(string(head), " ")
	if s, err := hex.DecodeString(scopeHex); !found || err != nil || string(s) != scope || scope == "" {
		return nil
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return nil
	}
	t := &stoppedTable{scope: scope, key: key, files: map[string]map[[16]byte]int{}, raw: b}
	for _, line := range strings.Split(string(body), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		name, err := hex.DecodeString(f[0])
		var d [16]byte
		n, nerr := strconv.Atoi(f[2])
		if err != nil || len(name) == 0 || len(f[1]) != 2*len(d) || nerr != nil || n <= 0 {
			continue
		}
		if _, err := hex.Decode(d[:], []byte(f[1])); err != nil {
			continue
		}
		t.add(string(name), d, n)
	}
	return t
}

func newStoppedTable(scope string) *stoppedTable {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil
	}
	return &stoppedTable{scope: scope, key: key, files: map[string]map[[16]byte]int{}}
}

func (t *stoppedTable) add(p string, d [16]byte, n int) {
	if t.files[p] == nil {
		t.files[p] = map[[16]byte]int{}
	}
	t.files[p][d] += n
}

// stoppedIn works out, for the file p whose current content is now, how many copies of each line are stopped lines: the copies
// the record holds, at most the copies of now beyond those in head (the content of p in the commit that was HEAD at the start;
// nil when there is none; read only when a line of now is in the record). byHash is keyed by the record's hashes, byLine by
// the lines themselves.
func (t *stoppedTable) stoppedIn(p string, now []byte, head func() []byte) (byHash map[[16]byte]int, byLine map[string]int) {
	if t == nil || len(t.files[p]) == 0 {
		return nil, nil
	}
	kept := t.files[p]
	h := newLineHasher(t.key)
	count := map[[16]byte]int{}
	text := map[[16]byte]string{}
	seen := map[string][16]byte{}
	eachLine(now, func(l []byte) {
		if blankLine(l) {
			return
		}
		d, ok := seen[string(l)]
		if !ok {
			d = h.hash(l)
			seen[string(l)] = d
		}
		if kept[d] > 0 {
			count[d]++
			text[d] = string(l)
		}
	})
	if len(count) > 0 && head != nil {
		seen = map[string][16]byte{}
		eachLine(head(), func(l []byte) {
			if blankLine(l) {
				return
			}
			d, ok := seen[string(l)]
			if !ok {
				d = h.hash(l)
				seen[string(l)] = d
			}
			if _, ok := count[d]; ok {
				count[d]--
			}
		})
	}
	byHash, byLine = map[[16]byte]int{}, map[string]int{}
	for d, n := range count {
		if n = min(n, kept[d]); n > 0 {
			byHash[d], byLine[text[d]] = n, n
		}
	}
	return byHash, byLine
}

// bytes is the record to write, or nil when it holds no line. The entries in old (kept before this write) come first; then the
// new ones, the files with the fewest new entries first. An entry that does not fit in max bytes is dropped, and the next ones
// are still tried.
func (t *stoppedTable) bytes(old map[string]map[[16]byte]bool, max int) []byte {
	type entry struct {
		path string
		d    [16]byte
		n    int
	}
	var kept []entry
	fresh := map[string][]entry{}
	for p, lines := range t.files {
		for d, n := range lines {
			if old[p][d] {
				kept = append(kept, entry{p, d, n})
			} else {
				fresh[p] = append(fresh[p], entry{p, d, n})
			}
		}
	}
	less := func(a, b entry) bool {
		if a.path != b.path {
			return a.path < b.path
		}
		return bytes.Compare(a.d[:], b.d[:]) < 0
	}
	sort.Slice(kept, func(i, j int) bool { return less(kept[i], kept[j]) })
	paths := make([]string, 0, len(fresh))
	for p, es := range fresh {
		sort.Slice(es, func(i, j int) bool { return less(es[i], es[j]) })
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if a, b := len(fresh[paths[i]]), len(fresh[paths[j]]); a != b {
			return a < b
		}
		return paths[i] < paths[j]
	})
	for _, p := range paths {
		kept = append(kept, fresh[p]...)
	}
	var out bytes.Buffer
	out.WriteString(stoppedMarker + hex.EncodeToString([]byte(t.scope)) + " " + hex.EncodeToString(t.key) + "\n")
	written := 0
	for _, e := range kept {
		line := hex.EncodeToString([]byte(e.path)) + " " + hex.EncodeToString(e.d[:]) + " " + strconv.Itoa(e.n) + "\n"
		if out.Len()+len(line) > max {
			continue
		}
		out.WriteString(line)
		written++
	}
	if written == 0 {
		return nil
	}
	return out.Bytes()
}

// lockPathWithin takes the lock file p, waiting at most wait (0: one try). ok is false when it was not taken (unlock is then a
// no-op). busy, when set (tests), is called each time the lock is found held.
func lockPathWithin(p string, wait time.Duration, busy func()) (unlock func(), ok bool) {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return func() {}, false
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return func() {}, false
	}
	if info, e := f.Stat(); e != nil || !info.Mode().IsRegular() {
		f.Close()
		return func() {}, false
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return func() {}, false
		}
		if busy != nil {
			busy()
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true
}

// headContent reads the content of p in the commit head (nil when head is not known or p is not in it).
func headContent(ctx context.Context, top, head, p string) []byte {
	if head == "" {
		return nil
	}
	if s, ok := gitRun(ctx, top, "show", head+":"+p); ok {
		return []byte(s)
	}
	return nil
}

// keepStoppedLines is a Stop's write of the record of the repository it compared (in.repo): the added lines the failing
// forbidden-words guards stopped (their except and allow applied as they apply them) are added, and the copies removed from
// their file or in the commit that was HEAD at the start (head) are dropped. All of it under the record's lock.
func keepStoppedLines(root string, failing []guard, in guardInput, head string) {
	if in.repo == "" {
		return
	}
	scope := feedbackScope(in.repo)
	if scope == "" {
		return
	}
	hits := map[string]map[string]int{}
	for p, lines := range in.Added {
		for _, l := range lines {
			for _, g := range failing {
				if !guardExcepts(g, p) && len(forbiddenWordsIn(g, l)) > 0 {
					if hits[p] == nil {
						hits[p] = map[string]int{}
					}
					hits[p][l]++
					break
				}
			}
		}
	}
	p := stoppedLinesPath(root, scope)
	if _, err := os.Lstat(p); len(hits) == 0 && err != nil {
		return // nothing kept and nothing to keep
	}
	if stoppedLinesSeam != nil {
		stoppedLinesSeam("before-lock")
	}
	unlock, ok := lockPathWithin(p+".lock", hookLockWait, nil)
	if !ok {
		hookLog("guard: the lines a check stopped were not kept: the record's lock is unavailable")
		return
	}
	defer unlock()
	t := readStoppedTable(p, scope)
	if stoppedLinesSeam != nil {
		stoppedLinesSeam("locked")
	}
	if t == nil {
		if len(hits) == 0 {
			return
		}
		if t = newStoppedTable(scope); t == nil {
			return
		}
	}
	old := map[string]map[[16]byte]bool{}
	for f, lines := range t.files {
		old[f] = map[[16]byte]bool{}
		for d := range lines {
			old[f][d] = true
		}
	}
	h := newLineHasher(t.key)
	for f, lines := range hits {
		for l, n := range lines {
			d := h.hash([]byte(l))
			if t.files[f][d] < n { // the copies stopped now include those the record already held
				t.add(f, d, n-t.files[f][d])
			}
		}
	}
	top, err := os.OpenRoot(in.repo)
	if err != nil {
		return
	}
	defer top.Close()
	ctx, cancel := context.WithTimeout(context.Background(), hookGitTimeout)
	defer cancel()
	for f := range t.files {
		now, ok := readGuardFile(top, f)
		if !ok {
			if _, err := top.Lstat(filepath.FromSlash(f)); errors.Is(err, os.ErrNotExist) {
				delete(t.files, f) // the file is gone, and its lines with it
			}
			continue // unreadable now (a link, past the size limit): kept until it can be read
		}
		if isBinary(now) {
			continue
		}
		if left, _ := t.stoppedIn(f, now, func() []byte { return headContent(ctx, in.repo, head, f) }); len(left) > 0 {
			t.files[f] = left
		} else {
			delete(t.files, f)
		}
	}
	b := t.bytes(old, keptLinesMax)
	if bytes.Equal(b, t.raw) {
		return
	}
	if err := keepLines(p, b); err != nil {
		hookLog("guard: the lines a check stopped were not kept: " + err.Error())
	}
}

// pruneStoppedLines removes the records of repositories that are gone (moved or deleted) and were not written for longer than
// the sessions are kept. A record whose repository is still there stays: its lines go only when removed or committed.
func pruneStoppedLines(root string) {
	dir := filepath.Join(root, "records", "stopped-lines")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".stopped") || !e.Type().IsRegular() {
			continue
		}
		p := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) <= hookSessionTTL {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil || !bytes.HasPrefix(b, []byte(stoppedMarker)) {
			continue
		}
		head, _, _ := bytes.Cut(b[len(stoppedMarker):], []byte("\n"))
		scopeHex, _, _ := strings.Cut(string(head), " ")
		scope, err := hex.DecodeString(scopeHex)
		if err != nil || len(scope) == 0 || projectID(string(scope))+".stopped" != name {
			continue
		}
		if _, err := os.Stat(string(scope)); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		unlock, ok := lockPathWithin(p+".lock", 0, nil)
		if !ok {
			continue
		}
		// no Stop writes for a repository that is not there, so the lock file goes too
		_ = os.Remove(p)
		_ = os.Remove(p + ".lock")
		unlock()
	}
}
