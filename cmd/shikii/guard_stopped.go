package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
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
// (a line there n times at the start and stopped m times is the person's n-m times). The record knows a line only by the hash
// of its text, so the same text written again after the stopped line was removed (from the file, or by a later commit or a
// revert) before a Stop compared the file since, cannot be told from the stopped line: it is stopped, as in 0.30.7.
//
// Only Stops write the record (a request's, a slash command's stretch's): under the record's lock they read it, add the lines
// the failing forbidden-words guards stopped and, for the files the Stop compared alone (the files its turn changed, and those
// with lines stopped now), drop the copies that were removed or committed, and the file's entries when the file is gone. The
// entries of the other files are kept as they are, and a stopped line out of sight for a while (stashed) is kept until a Stop
// compares its file. A Stop reads the record once: under the lock it reads the first line of the record alone, and uses the
// table its comparison read when the generation there (a random value, new at every write) is the one it read. When the lock
// cannot be taken the Stop writes nothing and goes on.
//
// Each count of an entry is written with stoppedCountDigits hex digits, so the line of an entry kept before has the same
// length however its counts change. A write is not sorted: the entries kept before are written in the order they were read
// (with their counts brought up to this Stop; those with no copy left are not written), then the new ones, the files with the
// fewest new entries first, so one huge file does not push out the small ones. Past stoppedLinesMax only new entries are
// dropped: the entries kept before take no more than the record they were read from, and are all written.
//
// A record stays while its repository is there: a Stop that reads it, and the sessions' pruning that finds the repository,
// set its time to now; the pruning removes a record only when its repository has not been seen for hookSessionTTL.

const stoppedMarker = "#stopped4 "

// stoppedLinesMax is the upper limit (bytes) of a record. A larger file is not read.
const stoppedLinesMax = 4 << 20

// stoppedCountDigits is the width, in hex digits, of each count of an entry. A count past stoppedCountMax is written as
// stoppedCountMax, and stoppedCountMax is read back as stoppedCountUnbounded: the true count was that or more, and reading it
// as larger only counts more copies as stopped (more copies were stopped; more copies were in HEAD at the stop, so fewer of
// them count as committed since). So a count past the width leans to stopping, never to letting a stopped line through. (A
// file is read up to guardFileBytesMax bytes, far below that count of lines.)
const (
	stoppedCountDigits    = 8
	stoppedCountMax       = 1<<(4*stoppedCountDigits) - 1
	stoppedCountUnbounded = math.MaxInt / 4
	stoppedGenBytes       = 16
)

// stoppedLinesSeam, when set (tests), is called at the steps of a Stop's write: "before-lock" after the comparison and before
// the lock is taken, "locked" right after the record was read under the lock; "read" each time a record is read back
// (readStoppedTable); and at the pruning's "prune-before-lock", after it looked at a record's time and before it takes the
// record's lock.
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

// stoppedRef names an entry: its file (the bytes of the path, as gitPath gives them) and the keyed hash of its line.
type stoppedRef struct {
	path string
	d    [16]byte
}

// stoppedTable is a record read back: its repository, key, generation and, per file, the entry of each keyed hash. read holds
// the entries read, in the order of the record, and fresh the entries added since (add, update), in the order they were added.
type stoppedTable struct {
	scope string
	key   []byte
	gen   string
	files map[string]map[[16]byte]stoppedEntry
	read  []stoppedRef
	fresh []stoppedRef
	size  int // the bytes of the record read
}

// stoppedHeader reads the first line of a record: the repository it belongs to, its key and its generation, and the entries
// after it. ok is false for anything else (an older form of the record included).
func stoppedHeader(b []byte) (scope string, key []byte, gen string, body []byte, ok bool) {
	if !bytes.HasPrefix(b, []byte(stoppedMarker)) {
		return "", nil, "", nil, false
	}
	head, body, found := bytes.Cut(b[len(stoppedMarker):], []byte("\n"))
	if !found {
		return "", nil, "", nil, false
	}
	f := strings.Split(string(head), " ")
	if len(f) != 3 || len(f[2]) != 2*stoppedGenBytes {
		return "", nil, "", nil, false
	}
	s, err := hex.DecodeString(f[0])
	if err != nil || len(s) == 0 {
		return "", nil, "", nil, false
	}
	key, err = hex.DecodeString(f[1])
	if err != nil || len(key) == 0 {
		return "", nil, "", nil, false
	}
	if _, err := hex.DecodeString(f[2]); err != nil {
		return "", nil, "", nil, false
	}
	return string(s), key, f[2], body, true
}

// readStoppedHeader reads the first line of the record at p alone: its repository and generation ("" for anything else).
func readStoppedHeader(p string) (scope, gen string) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return "", ""
	}
	var b []byte
	buf := make([]byte, 4096)
	for len(b) < 64<<10 { // the marker, the path and the key in hex, and the generation
		n, err := f.Read(buf)
		b = append(b, buf[:n]...)
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			scope, _, gen, _, ok := stoppedHeader(b[:i+1])
			if !ok {
				return "", ""
			}
			return scope, gen
		}
		if err != nil {
			return "", ""
		}
	}
	return "", ""
}

// readStoppedTable reads the record at p when it belongs to scope; nil when there is none, it is not a regular file, it is
// larger than stoppedLinesMax, it is in an older form, or it is another repository's.
func readStoppedTable(p, scope string) *stoppedTable {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > stoppedLinesMax {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, stoppedLinesMax+1))
	if err != nil || len(b) > stoppedLinesMax || int64(len(b)) != info.Size() {
		return nil
	}
	s, key, gen, body, ok := stoppedHeader(b)
	if !ok || s != scope || scope == "" {
		return nil
	}
	if stoppedLinesSeam != nil {
		stoppedLinesSeam("read")
	}
	t := &stoppedTable{scope: scope, key: key, gen: gen, files: map[string]map[[16]byte]stoppedEntry{}, size: len(b)}
	// how many lines each file has, first, so the map of each file is made once at its size (growing them line by line took
	// most of the time of reading a large record)
	sizes := map[string]int{}
	lines := 0
	// every line shikii writes ends in a newline: a last line without one was cut off (or edited by hand) and is not read, so a
	// record written back is never larger than the one read
	if i := bytes.LastIndexByte(body, '\n'); i >= 0 {
		body = body[:i+1]
	} else {
		body = nil
	}
	for rest := body; len(rest) > 0; {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		if i := bytes.IndexByte(line, ' '); i > 0 {
			sizes[string(line[:i])]++
			lines++
		}
	}
	t.read = make([]stoppedRef, 0, lines)
	names := map[string]string{} // the hex of a path, read once per file
	for len(body) > 0 {
		line := body
		if i := bytes.IndexByte(body, '\n'); i >= 0 {
			line, body = body[:i], body[i+1:]
		} else {
			body = nil
		}
		f, ok := stoppedFields(line)
		if !ok {
			continue
		}
		name, known := names[string(f[0])]
		if !known {
			b, err := hex.DecodeString(string(f[0]))
			if err != nil || len(b) == 0 {
				continue
			}
			name = string(b)
			names[string(f[0])] = name
			if t.files[name] == nil {
				t.files[name] = make(map[[16]byte]stoppedEntry, sizes[string(f[0])])
			}
		}
		var d [16]byte
		n, nok := stoppedCount(f[2])
		h, hok := stoppedCount(f[3])
		if len(f[1]) != 2*len(d) || !nok || n <= 0 || !hok {
			continue
		}
		if _, err := hex.Decode(d[:], f[1]); err != nil {
			continue
		}
		entries := t.files[name]
		if old, ok := entries[d]; ok { // the same entry twice: the copies add up, and it is written once, where it was first
			entries[d] = stoppedEntry{min(old.n+n, stoppedCountUnbounded), max(old.head, h)}
			continue
		}
		entries[d] = stoppedEntry{n, h}
		t.read = append(t.read, stoppedRef{name, d})
	}
	return t
}

// stoppedFields splits a line of a record at its runs of spaces and tabs; ok only for exactly 4 fields.
func stoppedFields(line []byte) (f [4][]byte, ok bool) {
	n := 0
	for i := 0; i < len(line); {
		if c := line[i]; c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] != ' ' && line[j] != '\t' && line[j] != '\r' {
			j++
		}
		if n == len(f) {
			return f, false
		}
		f[n] = line[i:j]
		n++
		i = j
	}
	return f, n == len(f)
}

// stoppedCount reads a count of a record: exactly stoppedCountDigits hex digits. stoppedCountMax is read as
// stoppedCountUnbounded (see stoppedCountDigits).
func stoppedCount(b []byte) (int, bool) {
	if len(b) != stoppedCountDigits {
		return 0, false
	}
	n := 0
	for _, c := range b {
		switch {
		case c >= '0' && c <= '9':
			n = n<<4 | int(c-'0')
		case c >= 'a' && c <= 'f':
			n = n<<4 | int(c-'a'+10)
		case c >= 'A' && c <= 'F':
			n = n<<4 | int(c-'A'+10)
		default:
			return 0, false
		}
	}
	if n == stoppedCountMax {
		return stoppedCountUnbounded, true
	}
	return n, true
}

// appendStoppedCount appends n as stoppedCountDigits hex digits: stoppedCountMax for a count past it.
func appendStoppedCount(b []byte, n int) []byte {
	const digits = "0123456789abcdef"
	v := uint64(max(0, n))
	if v > stoppedCountMax {
		v = stoppedCountMax
	}
	for i := stoppedCountDigits - 1; i >= 0; i-- {
		b = append(b, digits[(v>>(4*i))&0xf])
	}
	return b
}

func newStoppedTable(scope string) *stoppedTable {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil
	}
	return &stoppedTable{scope: scope, key: key, files: map[string]map[[16]byte]stoppedEntry{}}
}

// add adds e to the entry of d in the file p (the copies add up; the larger count in HEAD is kept). An entry the table did not
// hold is a new one.
func (t *stoppedTable) add(p string, d [16]byte, e stoppedEntry) {
	if t.files[p] == nil {
		t.files[p] = map[[16]byte]stoppedEntry{}
	}
	old, ok := t.files[p][d]
	if !ok {
		t.fresh = append(t.fresh, stoppedRef{p, d})
	}
	t.files[p][d] = stoppedEntry{min(old.n+e.n, stoppedCountUnbounded), max(old.head, e.head)}
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
// concerned). The entries it adds are new ones (stoppedTable.fresh). It reports whether the entries changed.
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
		if old, ok := entries[d]; !ok {
			t.fresh = append(t.fresh, stoppedRef{f, d})
			changed = true
		} else if old != e {
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

// bytes is the record to write, with a new generation, or nil when it holds no line. The entries read come first, in the order
// they were read, with the counts they hold now (an entry with no copy left is not written): each one's line has the length it
// had in the record read, so together they take no more than that record (which was within its limit), and none of them is
// dropped. The new entries follow, the files with the fewest new entries first and, in a file, by hash, while the record stays
// within max bytes (an entry that does not fit is dropped, and the next ones are still tried). Only the new entries are sorted,
// and each line is made once.
func (t *stoppedTable) bytes(max int) []byte {
	gen := make([]byte, stoppedGenBytes)
	if _, err := rand.Read(gen); err != nil {
		return nil
	}
	t.gen = hex.EncodeToString(gen)
	lineSize := func(pathHex []byte) int { return len(pathHex) + 2*16 + 2*stoppedCountDigits + 4 }
	out := make([]byte, 0, min(max, t.size+96*len(t.fresh)+256))
	out = append(out, stoppedMarker...)
	out = hex.AppendEncode(out, []byte(t.scope))
	out = append(out, ' ')
	out = hex.AppendEncode(out, t.key)
	out = append(out, ' ')
	out = append(out, t.gen...)
	out = append(out, '\n')
	line := func(out, pathHex []byte, d [16]byte, e stoppedEntry) []byte {
		out = append(out, pathHex...)
		out = append(out, ' ')
		out = hex.AppendEncode(out, d[:])
		out = append(out, ' ')
		out = appendStoppedCount(out, e.n)
		out = append(out, ' ')
		out = appendStoppedCount(out, e.head)
		return append(out, '\n')
	}
	// the new entries still held, by file. An entry read, dropped and added again is written once, where it was read: the
	// entries of the files with new ones are noted as they are written.
	fresh := map[string][][16]byte{}
	for _, r := range t.fresh {
		if _, ok := t.files[r.path][r.d]; ok {
			fresh[r.path] = append(fresh[r.path], r.d)
		}
	}
	written := map[stoppedRef]bool{}
	count := 0
	var pathHex []byte
	last, noted := "", false
	for i, r := range t.read {
		e, ok := t.files[r.path][r.d]
		if !ok {
			continue
		}
		if i == 0 || r.path != last {
			last, pathHex = r.path, hex.AppendEncode(pathHex[:0], []byte(r.path))
			_, noted = fresh[r.path]
		}
		if noted {
			if written[r] {
				continue
			}
			written[r] = true
		}
		out = line(out, pathHex, r.d, e)
		count++
	}
	paths := make([]string, 0, len(fresh))
	for p, ds := range fresh {
		slices.SortFunc(ds, func(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) })
		paths = append(paths, p)
	}
	slices.SortFunc(paths, func(a, b string) int {
		if c := cmp.Compare(len(fresh[a]), len(fresh[b])); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	for _, p := range paths {
		pathHex = hex.AppendEncode(pathHex[:0], []byte(p))
		for _, d := range fresh[p] {
			r := stoppedRef{p, d}
			if written[r] || len(out)+lineSize(pathHex) > max {
				continue
			}
			written[r] = true
			out = line(out, pathHex, d, t.files[p][d])
			count++
		}
	}
	if count == 0 {
		return nil
	}
	return out
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
	// the record as this Stop's comparison read it, while it is the same record: every write of the record gives it a new
	// generation (or removes it), so another Stop's write since the comparison is read again here, under the lock. Only the
	// first line is read to tell; a Stop reads the whole record once.
	var t *stoppedTable
	if in.stopped != nil && in.stopped.scope == scope && in.stopped.gen != "" {
		if s, gen := readStoppedHeader(p); s == scope && gen == in.stopped.gen {
			t = in.stopped
		}
	}
	if t == nil {
		t = readStoppedTable(p, scope)
	}
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
	if err := keepStoppedRecord(p, t.bytes(stoppedLinesMax)); err != nil {
		hookLog("guard: the lines a check stopped were not kept: " + err.Error())
	}
}

// keepStoppedRecord writes the record to p, or removes it when it holds no line. A write that fails leaves the record as it
// was (unlike the kept tables, which are removed then): the lines it held were stopped and must still stop.
func keepStoppedRecord(p string, b []byte) error {
	if b == nil {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeAtomic(p, b)
}

// stoppedRecordScope reads the repository a record at p belongs to from its first line ("" for anything else).
func stoppedRecordScope(p string) string {
	scope, _ := readStoppedHeader(p)
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
		if stoppedLinesSeam != nil {
			stoppedLinesSeam("prune-before-lock")
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
