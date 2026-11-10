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
// A line a forbidden-words guard stopped is not a line "from before the request" either. The table keeps every line as it is;
// the record of the stopped lines (guard_stopped.go, one per repository) is applied when the Stop compares, to this table as
// to the other ways of reading the content at the request.

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
	if cwd == "" {
		return nil
	}
	guards := lineGuards(root)
	if len(guards) == 0 {
		return nil
	}
	deadline := time.Now().Add(hookGitTimeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	out, ok := gitRun(ctx, cwd, "rev-parse", "--show-toplevel")
	if !ok {
		return nil
	}
	top, err := os.OpenRoot(strings.TrimSpace(out))
	if err != nil {
		return nil
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
		return nil
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
		lines := make(map[string]int, len(counts))
		for l, n := range counts {
			d := hasher.hash([]byte(l))
			lines[hex.EncodeToString(d[:])] += n
		}
		if !table.add(p, keptFile{Blob: meta[1], Lines: lines}) {
			break
		}
		files++
	}
	return table.bytes()
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
// lines that are not blank are hashed, each distinct line once. stopped are the copies of lines of now that a guard stopped
// (guard_stopped.go): a line kept n times and stopped m times is matched n-m times.
func (k *keptLines) addedAgainst(f keptFile, now []byte, stopped map[string]int) []string {
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
			if s := stopped[string(l)]; s > 0 {
				count[d] = max(0, count[d]-s)
			}
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
