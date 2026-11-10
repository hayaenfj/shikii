package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The lines of the files that were not committed at the request (an uncommitted change to a tracked file, a file Git does
// not track yet). The listing keeps no content and the blobs of those files leave with the temporary store the listing was
// taken in, so at Stop the content at the request cannot be read back. To keep a line the person wrote before the request
// from counting as a line the turn added, the request keeps, for those files only, the keyed hashes (HMAC-SHA256, with a key
// made for this table alone) of their lines and how many times each occurs, never the lines themselves. The table sits next
// to the listing (sessions/<id>.lines, sessions/<id>.guard.lines for a slash command's stretch), starts with the listing's
// own header (which request or stretch it belongs to), is written 0600, replaced by the next request and pruned with the
// listings. Only forbidden-words reads added lines, so the table is kept only while such a guard is enabled.
//
// A line a forbidden-words guard stopped is not a line "from before the request" either. The Stop that stops it (or records
// the failure again) keeps those very lines, per file, as keyed hashes with a key of their own (sessions/<id>.stopped, no
// content, 0600, pruned with the listings); the next request's table leaves out the lines of the current files that match
// them and carries the ones still there to a new .stopped. So a stopped line stays an added line, turn after turn, until it is
// removed or committed (its file then equals HEAD and is not kept), while every other uncommitted line (the person's drafts,
// in the same file or in another one) is kept as before.

// keptLinesMax is the upper limit (bytes) of one table. Files past it are not kept, as files past guardFilesMax are not:
// they are compared with the request's HEAD as before.
const keptLinesMax = 16 << 20

// keptKeyMarker starts the line after the header that holds the table's key. In the file, the paths are written as the hex of
// their bytes (a path need not be UTF-8, and encoding/json would replace such bytes). A table written before that, with the
// paths themselves after "#key ", is not read: its files are compared with HEAD, as without a table.
const keptKeyMarker = "#key2 "

func sessionLinesPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".lines"
}

func sessionGuardLinesPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".guard.lines"
}

// keptFile is one file of the table: the blob id the listing names for it (the table is used only for that same content)
// and the count of each line's keyed hash. Blank lines are not kept (they are never added lines).
type keptFile struct {
	Blob  string         `json:"blob"`
	Lines map[string]int `json:"lines"`
}

// keptLines is a table read back: its key and its files by path (the bytes of the path, as gitPath gives them).
type keptLines struct {
	key   []byte
	files map[string]keptFile
}

// lineHasher keys the hashes of one table's lines. One HMAC is made and reset for each line: making it again for every line
// costs more than the hash itself.
type lineHasher struct {
	mac hash.Hash
	sum []byte
}

func newLineHasher(key []byte) *lineHasher {
	return &lineHasher{mac: hmac.New(sha256.New, key), sum: make([]byte, 0, sha256.Size)}
}

func (h *lineHasher) hash(line []byte) [16]byte {
	h.mac.Reset()
	h.mac.Write(line)
	h.sum = h.mac.Sum(h.sum[:0])
	var d [16]byte
	copy(d[:], h.sum)
	return d
}

// lineHash is the keyed hash of one line as the table writes it.
func lineHash(key []byte, line string) string {
	d := newLineHasher(key).hash([]byte(line))
	return hex.EncodeToString(d[:])
}

// eachLine calls fn with every line of b, split at "\n" as strings.Split splits it, without copying them.
func eachLine(b []byte, fn func([]byte)) {
	for {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			fn(b)
			return
		}
		fn(b[:i])
		b = b[i+1:]
	}
}

func blankLine(l []byte) bool { return len(bytes.TrimSpace(l)) == 0 }

// gitBlobID is the id Git gives content stored as a blob, in the repository's hash (sha1: 40 digits, sha256: 64).
func gitBlobID(content []byte, digits int) string {
	var h hash.Hash
	switch digits {
	case 40:
		h = sha1.New()
	case 64:
		h = sha256.New()
	default:
		return ""
	}
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// lineGuards are the enabled guards that read added lines (forbidden-words).
func lineGuards(root string) []guard {
	guards, _ := enabledGuards(root)
	var out []guard
	for _, g := range guards {
		if g.Kind == guardKindForbiddenWords {
			out = append(out, g)
		}
	}
	return out
}

// guardNeedsLines reports whether an enabled guard reads added lines.
func guardNeedsLines(root string) bool { return len(lineGuards(root)) > 0 }

// stoppedMarker starts a .stopped file: the hex of the repository (scope) it was kept in, then the hex of its key.
const stoppedMarker = "#stopped1 "

func sessionStoppedPath(agent, id string) string {
	return strings.TrimSuffix(sessionPath(agent, id), ".json") + ".stopped"
}

// stoppedTable is the lines a forbidden-words guard stopped and that were not removed or committed since: per file (the bytes
// of the path), how many times each keyed hash of such a line was stopped. It belongs to one repository (scope).
type stoppedTable struct {
	scope string
	key   []byte
	files map[string]map[string]int
}

// readStoppedTable reads the .stopped at p; nil when there is none or it cannot be read (nothing is then left out).
func readStoppedTable(p string) *stoppedTable {
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > keptLinesMax+1024 {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil || !bytes.HasPrefix(b, []byte(stoppedMarker)) {
		return nil
	}
	head, body, found := bytes.Cut(b[len(stoppedMarker):], []byte("\n"))
	if !found {
		return nil
	}
	scopeHex, keyHex, found := strings.Cut(string(head), " ")
	scope, err := hex.DecodeString(scopeHex)
	if !found || err != nil || len(scope) == 0 {
		return nil
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) == 0 {
		return nil
	}
	var files map[string]map[string]int
	if json.Unmarshal(body, &files) != nil {
		return nil
	}
	t := &stoppedTable{scope: string(scope), key: key, files: make(map[string]map[string]int, len(files))}
	for name, lines := range files {
		if p, err := hex.DecodeString(name); err == nil && len(p) > 0 && len(lines) > 0 {
			t.files[string(p)] = lines
		}
	}
	return t
}

// newStoppedTable starts an empty table for scope, with the key of the table it follows when there is one (its hashes are
// carried as they are) or a new random key.
func newStoppedTable(scope string, from *stoppedTable) *stoppedTable {
	t := &stoppedTable{scope: scope, files: map[string]map[string]int{}}
	if from != nil {
		t.key = from.key
		return t
	}
	t.key = make([]byte, 32)
	if _, err := rand.Read(t.key); err != nil {
		return nil
	}
	return t
}

func (t *stoppedTable) add(p, hash string, n int) {
	if n <= 0 {
		return
	}
	if t.files[p] == nil {
		t.files[p] = map[string]int{}
	}
	t.files[p][hash] += n
}

// bytes is the file to keep, or nil when no line is left (the .stopped is then removed). Files past keptLinesMax are not kept.
func (t *stoppedTable) bytes() []byte {
	if t == nil || len(t.files) == 0 {
		return nil
	}
	files := make(map[string]map[string]int, len(t.files))
	for p, lines := range t.files {
		files[hex.EncodeToString([]byte(p))] = lines
	}
	body, err := json.Marshal(files)
	if err != nil {
		return nil
	}
	out := append([]byte(stoppedMarker+hex.EncodeToString([]byte(t.scope))+" "+hex.EncodeToString(t.key)+"\n"), body...)
	if len(out) > keptLinesMax {
		return nil
	}
	return out
}

// stoppedNext is what a start (a request, a slash command) does with the .stopped: replace it with b (remove it when b is
// nil), or, when set is false, leave it as it is (it belongs to another repository, or no table was built).
type stoppedNext struct {
	set bool
	b   []byte
}

// keepStopped applies n to the .stopped at p.
func keepStopped(p string, n stoppedNext) error {
	if !n.set {
		return nil
	}
	return keepLines(p, n.b)
}

// keepStoppedLines keeps, at a Stop, the added lines the failing forbidden-words guards stopped (their except and allow
// applied as they apply them). The lines kept earlier in this repository stay for the files whose added lines this Stop did
// not compare (the turn did not change them, or they could not be read); for the files it compared, only the lines it stopped
// now remain, so a fixed line goes, and with no line left the .stopped is removed. A .stopped of another repository is left
// alone unless this Stop stopped a line.
func keepStoppedLines(root, session, scope string, failing []guard, in guardInput) {
	if session == "" || scope == "" {
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
	p := sessionStoppedPath(root, session)
	old := readStoppedTable(p)
	if old != nil && old.scope != scope {
		if len(hits) == 0 {
			return
		}
		old = nil
	}
	t := newStoppedTable(scope, old)
	if t == nil {
		return
	}
	if old != nil {
		for f, lines := range old.files {
			if !in.Compared[f] {
				t.files[f] = lines
			}
		}
	}
	h := newLineHasher(t.key)
	for f, lines := range hits {
		for l, n := range lines {
			d := h.hash([]byte(l))
			t.add(f, hex.EncodeToString(d[:]), n)
		}
	}
	if err := keepLines(p, t.bytes()); err != nil {
		hookLog("guard: the lines a check stopped were not kept: " + err.Error())
	}
}

// readGuardFile reads one listed file: a regular file inside top of at most guardFileBytesMax, without following a link in
// its place. A larger file is not read. os.Root follows a link inside it even with O_NOFOLLOW, so the file opened must be the
// one Lstat found there.
func readGuardFile(top *os.Root, p string) ([]byte, bool) {
	name := filepath.FromSlash(p)
	there, err := top.Lstat(name)
	if err != nil || !there.Mode().IsRegular() || there.Size() > guardFileBytesMax {
		return nil, false
	}
	f, err := top.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(there, info) || info.Size() > guardFileBytesMax {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(f, guardFileBytesMax+1))
	if err != nil || len(b) > guardFileBytesMax {
		return nil, false
	}
	return b, true
}

// keptTable writes a table as it is built, so its size is the bytes written: the header, the key line, then
// {"<hex of the path>":{"blob":...,"lines":{...}},...}. A file that does not fit in max is not kept, nor any after it.
type keptTable struct {
	buf bytes.Buffer
	max int
	n   int
}

func newKeptTable(header string, key []byte, max int) *keptTable {
	t := &keptTable{max: max}
	t.buf.WriteString(header + keptKeyMarker + hex.EncodeToString(key) + "\n{")
	return t
}

// add keeps one file, or reports that it does not fit.
func (t *keptTable) add(p string, f keptFile) bool {
	item, err := json.Marshal(f)
	if err != nil {
		return true // nothing is kept for it; the next file may still be
	}
	name := `"` + hex.EncodeToString([]byte(p)) + `":`
	size := len(name) + len(item)
	if t.n > 0 {
		size++ // the comma before it
	}
	if t.buf.Len()+size+1 > t.max { // and the closing brace
		return false
	}
	if t.n > 0 {
		t.buf.WriteByte(',')
	}
	t.buf.WriteString(name)
	t.buf.Write(item)
	t.n++
	return true
}

// bytes is the table, or nil when it keeps no file.
func (t *keptTable) bytes() []byte {
	if t.n == 0 {
		return nil
	}
	return append(t.buf.Bytes(), '}')
}

// uncommittedLines builds the table for the listing just taken at cwd (header is the listing's first line), or nil when no
// file differs from HEAD, no guard reads added lines, or nothing could be kept. A file is kept only while its content on disk
// is still the blob the listing names (a file changed since, or rewritten by a clean filter, is compared with HEAD as before).
// It stops at guardFilesMax files, at keptLinesMax bytes and at the hook's git time limit; the rest are not kept.
func uncommittedLines(root, cwd, header, listing string) []byte {
	table, _ := uncommittedLinesAfter(root, cwd, header, listing, "", "")
	return table
}

// uncommittedLinesAfter is uncommittedLines at a new start of a session (a request, a slash command) in the repository scope:
// the lines the session's .stopped holds for this repository are left out of the table (they stay added lines), and those
// still in the files are carried to the .stopped the start keeps (stoppedNext).
func uncommittedLinesAfter(root, cwd, header, listing, session, scope string) ([]byte, stoppedNext) {
	if cwd == "" {
		return nil, stoppedNext{}
	}
	guards := lineGuards(root)
	if len(guards) == 0 {
		return nil, stoppedNext{}
	}
	deadline := time.Now().Add(hookGitTimeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	out, ok := gitRun(ctx, cwd, "rev-parse", "--show-toplevel")
	if !ok {
		return nil, stoppedNext{}
	}
	top, err := os.OpenRoot(strings.TrimSpace(out))
	if err != nil {
		return nil, stoppedNext{}
	}
	defer top.Close()
	head := map[string]string{}
	if out, ok := gitRun(ctx, top.Name(), "ls-tree", "-r", "-z", "--full-tree", "HEAD"); ok { // no HEAD yet: every file differs
		for _, rec := range strings.Split(out, "\x00") {
			meta, p, found := strings.Cut(rec, "\t")
			if f := strings.Fields(meta); found && len(f) == 3 {
				head[p] = f[2]
			}
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, stoppedNext{}
	}
	var stopped, carried *stoppedTable // the .stopped of this repository, and what of it is still in the files
	var stoppedHasher *lineHasher
	handled := map[string]bool{}
	if session != "" && scope != "" {
		if stopped = readStoppedTable(sessionStoppedPath(root, session)); stopped != nil && stopped.scope != scope {
			stopped = nil // another repository's: left as it is
		}
		if stopped != nil {
			carried = newStoppedTable(scope, stopped)
			stoppedHasher = newLineHasher(stopped.key)
		}
	}
	hasher := newLineHasher(key)
	table := newKeptTable(header, key, keptLinesMax)
	files := 0
	for _, line := range strings.Split(listing, "\n") {
		if files >= guardFilesMax || time.Now().After(deadline) {
			break
		}
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(line[:tab])
		p := gitPath(line[tab+1:])
		if len(meta) < 2 || (meta[0] != "100644" && meta[0] != "100755") || strings.HasPrefix(p, ".shikii/") || head[p] == meta[1] {
			continue
		}
		b, ok := readGuardFile(top, p)
		if !ok || isBinary(b) || gitBlobID(b, len(meta[1])) != meta[1] {
			continue
		}
		counts := map[string]int{} // each distinct line is hashed once
		eachLine(b, func(l []byte) {
			if !blankLine(l) {
				counts[string(l)]++
			}
		})
		var stoppedHere map[string]int
		if stopped != nil {
			stoppedHere = stopped.files[p]
			handled[p] = true
		}
		lines := make(map[string]int, len(counts))
		for l, n := range counts {
			if len(stoppedHere) > 0 { // a stopped line still here is left out and carried, as many times as it was stopped
				sd := stoppedHasher.hash([]byte(l))
				h := hex.EncodeToString(sd[:])
				if left := min(n, stoppedHere[h]); left > 0 {
					carried.add(p, h, left)
					if n -= left; n == 0 {
						continue
					}
				}
			}
			d := hasher.hash([]byte(l))
			lines[hex.EncodeToString(d[:])] += n
		}
		if !table.add(p, keptFile{Blob: meta[1], Lines: lines}) {
			break
		}
		files++
	}
	if stopped == nil {
		return table.bytes(), stoppedNext{}
	}
	if carried == nil {
		return table.bytes(), stoppedNext{} // no key could be made: the .stopped stays as it is
	}
	// The stopped files this start did not read keep their lines while they still differ from HEAD (past a limit, changed
	// since the listing, unreadable: they are compared with HEAD, where the lines are added lines anyway). A file that is gone
	// from the listing or equals HEAD again (removed, committed) drops them.
	var listed map[string]string
	for p, lines := range stopped.files {
		if handled[p] {
			continue
		}
		if listed == nil {
			listed = parseListing(listing)
		}
		meta := strings.Fields(listed[p])
		if len(meta) != 2 || (meta[0] != "100644" && meta[0] != "100755") || head[p] == meta[1] {
			continue
		}
		for h, n := range lines {
			carried.add(p, h, n)
		}
	}
	return table.bytes(), stoppedNext{set: true, b: carried.bytes()}
}

// keepLines writes the table to p, or removes an earlier one when there is none (it must not be read for this listing).
func keepLines(p string, table []byte) error {
	if table == nil {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := writeAtomic(p, table); err != nil {
		_ = os.Remove(p)
		return err
	}
	return nil
}

// readKeptLines reads the table at p when it belongs to the listing with this header; nil otherwise (those files are then
// compared with HEAD as before).
func readKeptLines(p, header string) *keptLines {
	if p == "" || header == "" {
		return nil
	}
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > keptLinesMax+1024 {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil || !bytes.HasPrefix(b, []byte(header)) {
		return nil
	}
	keyLine, body, found := bytes.Cut(b[len(header):], []byte("\n"))
	if !found || !bytes.HasPrefix(keyLine, []byte(keptKeyMarker)) {
		return nil
	}
	key, err := hex.DecodeString(string(keyLine[len(keptKeyMarker):]))
	if err != nil || len(key) == 0 {
		return nil
	}
	var files map[string]keptFile
	if json.Unmarshal(body, &files) != nil {
		return nil
	}
	k := &keptLines{key: key, files: make(map[string]keptFile, len(files))}
	for name, f := range files {
		if p, err := hex.DecodeString(name); err == nil && len(p) > 0 {
			k.files[string(p)] = f
		}
	}
	return k
}

// file is the kept table of p when it was kept for the blob the listing names (nil k: none kept).
func (k *keptLines) file(p, blob string) (keptFile, bool) {
	if k == nil {
		return keptFile{}, false
	}
	f, ok := k.files[p]
	return f, ok && f.Blob == blob
}

// addedAgainst returns the lines of now that the kept file does not have (a line kept n times is matched n times). Only the
// lines that are not blank are hashed, each distinct line once.
func (k *keptLines) addedAgainst(f keptFile, now []byte) []string {
	count := make(map[[16]byte]int, len(f.Lines))
	for h, n := range f.Lines {
		var d [16]byte
		if len(h) == 2*len(d) {
			if _, err := hex.Decode(d[:], []byte(h)); err == nil {
				count[d] += n
			}
		}
	}
	hasher := newLineHasher(k.key)
	seen := map[string][16]byte{}
	var out []string
	eachLine(now, func(l []byte) {
		if blankLine(l) {
			return
		}
		d, ok := seen[string(l)]
		if !ok {
			d = hasher.hash(l)
			seen[string(l)] = d
		}
		if count[d] > 0 {
			count[d]--
			return
		}
		out = append(out, string(l))
	})
	return out
}

// moveKeptHeader rewrites the header of a kept file from one request to another (a regrouped request); nothing is done
// when it is missing or belongs to another request.
func moveKeptHeader(p, from, to string) error {
	b, err := os.ReadFile(p)
	if err != nil || !bytes.HasPrefix(b, []byte(listingHeader(from))) {
		return nil
	}
	return writeAtomic(p, append([]byte(listingHeader(to)), b[len(listingHeader(from)):]...))
}
