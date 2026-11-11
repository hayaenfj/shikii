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
// file the keyed hash (HMAC-SHA256, with a random key made for that record alone) of each stopped line, how many copies were
// stopped, and how many copies of that line the commit that was HEAD at the start of the stopped turn held; never the line
// itself. It is written 0600.
//
// Every comparison counts, of the lines of the current file, the copies the record holds as added lines (stoppedEntry.left):
// the copies committed after the line was stopped (the comparing turn's start HEAD holding more copies than the HEAD the line
// was stopped at) are no longer stopped, and no more copies are stopped than the file holds beyond the comparing turn's start
// HEAD. So a stopped line goes once it is removed from the file or committed before the comparing turn started; a commit the
// turn itself makes does not clear it for that turn. The person's lines are the lines there at the start that are not stopped
// (a line there n times at the start and stopped m times is the person's n-m times).
//
// Only Stops write the record (a request's, a slash command's stretch's): under the record's lock they read it, add the lines
// the failing forbidden-words guards stopped and, for the files the Stop compared alone (the files its turn changed, and those
// with lines stopped now), drop the copies that were removed or committed, and the file's entries when the file is gone. The
// entries of the other files are kept as they are, unread, so a Stop costs in proportion to the files its turn changed, and a
// stopped line out of sight for a while (stashed) is kept until a Stop compares its file. When the lock cannot be taken the
// Stop writes nothing and goes on. Past keptLinesMax the entries already kept stay and the new ones that do not fit are
// dropped, the files with the fewest new entries first in, so one huge file does not push out the small ones.
//
// A record stays while its repository is there: a Stop that reads it, and the sessions' pruning that finds the repository,
// set its time to now; the pruning removes a record only when its repository has not been seen for hookSessionTTL.

const stoppedMarker = "#stopped3 "

// stoppedLinesSeam, when set (tests), is called at the steps of a Stop's write: "before-lock" after the comparison and before
// the lock is taken, "locked" right after the record was read under the lock.
var stoppedLinesSeam func(step string)

// stoppedLinesPath is the record of the lines forbidden-words guards stopped in the repository scope.
func stoppedLinesPath(root, scope string) string {
	return filepath.Join(root, "records", "stopped-lines", projectID(scope)+".stopped")
}

// stoppedEntry is one stopped line of a file: n copies were stopped while the commit that was HEAD at the start of the stopped
// turn held head copies of it.
type stoppedEntry struct {
	n, head int
}

// left is how many copies of the entry are still stopped lines, for a file holding now copies of the line whose comparing turn
// started at a commit holding head copies: the copies committed since the line was stopped (head beyond e.head) are no longer
// stopped, and no more copies are stopped than the file holds beyond that commit.
func (e stoppedEntry) left(now, head int) int {
	return min(max(0, e.n-max(0, head-e.head)), max(0, now-head))
}

// stoppedTable is a record read back: its repository, key and, per file (the bytes of the path, as gitPath gives them), the
// entry of each keyed hash.
type stoppedTable struct {
	scope string
	key   []byte
	files map[string]map[[16]byte]stoppedEntry
}

// stoppedHeader reads the first line of a record: the repository it belongs to and its key, and the entries after it. ok is
// false for anything else (an older form of the record included).
func stoppedHeader(b []byte) (scope string, key, body []byte, ok bool) {
	if !bytes.HasPrefix(b, []byte(stoppedMarker)) {
		return "", nil, nil, false
	}
	head, body, found := bytes.Cut(b[len(stoppedMarker):], []byte("\n"))
	if !found {
		return "", nil, nil, false
	}
	scopeHex, keyHex, found := strings.Cut(string(head), " ")
	s, err := hex.DecodeString(scopeHex)
	if !found || err != nil || len(s) == 0 {
		return "", nil, nil, false
	}
	key, err = hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return "", nil, nil, false
	}
	return string(s), key, body, true
}

// readStoppedTable reads the record at p when it belongs to scope; nil when there is none, it is not a regular file, it is
// larger than any record written, it is in an older form, or it is another repository's.
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
	if err != nil || len(b) > keptLinesMax {
		return nil
	}
	s, key, body, ok := stoppedHeader(b)
	if !ok || s != scope || scope == "" {
		return nil
	}
	t := &stoppedTable{scope: scope, key: key, files: map[string]map[[16]byte]stoppedEntry{}}
	for _, line := range strings.Split(string(body), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			continue
		}
		name, err := hex.DecodeString(f[0])
		var d [16]byte
		n, nerr := strconv.Atoi(f[2])
		h, herr := strconv.Atoi(f[3])
		if err != nil || len(name) == 0 || len(f[1]) != 2*len(d) || nerr != nil || n <= 0 || herr != nil || h < 0 {
			continue
		}
		if _, err := hex.Decode(d[:], []byte(f[1])); err != nil {
			continue
		}
		t.add(string(name), d, stoppedEntry{n, h})
	}
	return t
}

func newStoppedTable(scope string) *stoppedTable {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil
	}
	return &stoppedTable{scope: scope, key: key, files: map[string]map[[16]byte]stoppedEntry{}}
}

// add adds e to the entry of d in the file p (the copies add up; the larger count in HEAD is kept).
func (t *stoppedTable) add(p string, d [16]byte, e stoppedEntry) {
	if t.files[p] == nil {
		t.files[p] = map[[16]byte]stoppedEntry{}
	}
	old := t.files[p][d]
	t.files[p][d] = stoppedEntry{old.n + e.n, max(old.head, e.head)}
}

// countLines counts the copies in content of the non-blank lines whose keyed hash want takes; text, when not nil, gets the
// text of each line counted.
func countLines(h *lineHasher, content []byte, want func([16]byte) bool, text map[[16]byte]string) map[[16]byte]int {
	count := map[[16]byte]int{}
	seen := map[string][16]byte{}
	eachLine(content, func(l []byte) {
		if blankLine(l) {
			return
		}
		d, ok := seen[string(l)]
		if !ok {
			d = h.hash(l)
			seen[string(l)] = d
		}
		if want(d) {
			count[d]++
			if text != nil {
				text[d] = string(l)
			}
		}
	})
	return count
}

// stoppedIn works out, for the file p whose current content is now, how many copies of each line are stopped lines (by line):
// stoppedEntry.left of each entry, with the copies in head (the content of p in the commit that was HEAD at the start of the
// comparing turn; nil when there is none; read only when a line of now is in the record).
func (t *stoppedTable) stoppedIn(p string, now []byte, head func() []byte) map[string]int {
	if t == nil || len(t.files[p]) == 0 {
		return nil
	}
	entries := t.files[p]
	h := newLineHasher(t.key)
	text := map[[16]byte]string{}
	inNow := countLines(h, now, func(d [16]byte) bool { _, ok := entries[d]; return ok }, text)
	if len(inNow) == 0 {
		return nil
	}
	var inHead map[[16]byte]int
	if head != nil {
		inHead = countLines(h, head(), func(d [16]byte) bool { return inNow[d] > 0 }, nil)
	}
	byLine := map[string]int{}
	for d, c := range inNow {
		if n := entries[d].left(c, inHead[d]); n > 0 {
			byLine[text[d]] = n
		}
	}
	return byLine
}

// update brings the entries of the file f, whose current content is now, up to this Stop: the lines stopped now (by line, with
// their copies; the copies the record already held are among them) are added, the copies removed or committed are dropped, and
// what is kept is counted from the commit that was HEAD at the start of this turn (head, read only when a line of now is
// concerned). It reports whether the entries changed.
func (t *stoppedTable) update(f string, now []byte, stopped map[string]int, head func() []byte) bool {
	entries := t.files[f]
	h := newLineHasher(t.key)
	hits := map[[16]byte]int{}
	for l, n := range stopped {
		hits[h.hash([]byte(l))] = n
	}
	if len(entries) == 0 && len(hits) == 0 {
		return false
	}
	inNow := countLines(h, now, func(d [16]byte) bool { _, ok := entries[d]; return ok || hits[d] > 0 }, nil)
	var inHead map[[16]byte]int
	if len(inNow) > 0 {
		inHead = countLines(h, head(), func(d [16]byte) bool { return inNow[d] > 0 }, nil)
	}
	next := map[[16]byte]stoppedEntry{}
	set := func(d [16]byte) {
		n, base := 0, inHead[d]
		if e, ok := entries[d]; ok {
			// counted from here on against this turn's start (never against an older commit than the one it was stopped at)
			n, base = e.left(inNow[d], inHead[d]), max(e.head, inHead[d])
		}
		if n = min(max(n, hits[d]), max(0, inNow[d]-inHead[d])); n > 0 {
			next[d] = stoppedEntry{n, base}
		}
	}
	for d := range entries {
		set(d)
	}
	for d := range hits {
		set(d)
	}
	changed := len(next) != len(entries)
	for d, e := range next {
		if old, ok := entries[d]; !ok || old != e {
			changed = true
		}
	}
	if len(next) > 0 {
		t.files[f] = next
	} else {
		delete(t.files, f)
	}
	return changed
}

// bytes is the record to write, or nil when it holds no line. The entries in old (kept before this write) come first; then the
// new ones, the files with the fewest new entries first. An entry that does not fit in max bytes is dropped, and the next ones
// are still tried.
func (t *stoppedTable) bytes(old map[string]map[[16]byte]bool, max int) []byte {
	type entry struct {
		path string
		d    [16]byte
		e    stoppedEntry
	}
	var kept []entry
	fresh := map[string][]entry{}
	for p, lines := range t.files {
		for d, e := range lines {
			if old[p][d] {
				kept = append(kept, entry{p, d, e})
			} else {
				fresh[p] = append(fresh[p], entry{p, d, e})
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
		line := hex.EncodeToString([]byte(e.path)) + " " + hex.EncodeToString(e.d[:]) + " " + strconv.Itoa(e.e.n) + " " + strconv.Itoa(e.e.head) + "\n"
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
// forbidden-words guards stopped (their except and allow applied as they apply them) are added and, in the files the Stop
// compared (in.Changed as far as the comparison reads them, and the files with lines stopped now), the copies removed from
// the file or in the commit that was HEAD at the start (head) are dropped, and the entries of a file that is gone. The other
// files' entries are not read again. All of it under the record's lock.
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
	} else {
		now := time.Now()
		_ = os.Chtimes(p, now, now) // its repository is in use: the record is not pruned for hookSessionTTL from now
	}
	old := map[string]map[[16]byte]bool{}
	for f, lines := range t.files {
		old[f] = map[[16]byte]bool{}
		for d := range lines {
			old[f][d] = true
		}
	}
	compared := map[string]bool{}
	for i, f := range in.Changed {
		if i >= guardFilesMax {
			break
		}
		compared[f] = true
	}
	for f := range hits {
		compared[f] = true
	}
	top, err := os.OpenRoot(in.repo)
	if err != nil {
		return
	}
	defer top.Close()
	ctx, cancel := context.WithTimeout(context.Background(), hookGitTimeout)
	defer cancel()
	changed := false
	for f := range compared {
		now, ok := readGuardFile(top, f)
		if !ok {
			if _, err := top.Lstat(filepath.FromSlash(f)); errors.Is(err, os.ErrNotExist) && len(t.files[f]) > 0 {
				delete(t.files, f) // the file is gone, and its lines with it
				changed = true
			}
			continue // unreadable now (a link, past the size limit): kept until it can be read
		}
		if isBinary(now) {
			continue
		}
		if t.update(f, now, hits[f], func() []byte { return headContent(ctx, in.repo, head, f) }) {
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := keepLines(p, t.bytes(old, keptLinesMax)); err != nil {
		hookLog("guard: the lines a check stopped were not kept: " + err.Error())
	}
}

// stoppedRecordScope reads the repository a record at p belongs to from its first line ("" for anything else).
func stoppedRecordScope(p string) string {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10)) // the first line: the marker, the path in hex and the key
	if err != nil {
		return ""
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[:i+1]
	}
	scope, _, _, ok := stoppedHeader(b)
	if !ok {
		return ""
	}
	return scope
}

// pruneStoppedLines keeps the records of the repositories that are there and removes those of repositories not seen for
// longer than the sessions are kept: the time of a record whose repository is there is set to now (as a Stop that reads it
// does), and a record whose repository is gone (moved or deleted) goes once that time is older than hookSessionTTL. So a
// repository away for a moment (renamed, a disk not mounted) keeps its record, and its lines go only when removed or committed.
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
		scope := stoppedRecordScope(p)
		if scope == "" || projectID(scope)+".stopped" != name {
			continue
		}
		if _, err := os.Stat(scope); err == nil {
			now := time.Now()
			_ = os.Chtimes(p, now, now)
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if info, err := e.Info(); err != nil || time.Since(info.ModTime()) <= hookSessionTTL {
			continue
		}
		unlock, ok := lockPathWithin(p+".lock", 0, nil)
		if !ok {
			continue
		}
		// looked at again under the lock: a Stop may have read or written it since
		if info, err := os.Lstat(p); err == nil && time.Since(info.ModTime()) > hookSessionTTL {
			// no Stop writes for a repository that is not there, so the lock file goes too
			_ = os.Remove(p)
			_ = os.Remove(p + ".lock")
		}
		unlock()
	}
}
