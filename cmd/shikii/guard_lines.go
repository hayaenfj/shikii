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
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// keptLinesMax is the upper limit (bytes) of one table. Files past it are not kept, as files past guardFilesMax are not:
// they are compared with the request's HEAD as before.
const keptLinesMax = 16 << 20

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

type keptLines struct {
	key   []byte
	files map[string]keptFile
}

func lineHash(key []byte, line string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(line))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

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

// guardNeedsLines reports whether an enabled guard reads added lines.
func guardNeedsLines(root string) bool {
	guards, _ := enabledGuards(root)
	for _, g := range guards {
		if g.Kind == guardKindForbiddenWords {
			return true
		}
	}
	return false
}

// uncommittedLines builds the table for the listing just taken at cwd (header is the listing's first line), or nil when no
// file differs from HEAD, no guard reads added lines, or nothing could be kept. A file is kept only while its content on disk
// is still the blob the listing names (a file changed since, or rewritten by a clean filter, is compared with HEAD as before).
// It stops at guardFilesMax files, at keptLinesMax bytes and at the hook's git time limit; the rest are not kept.
func uncommittedLines(root, cwd, header, listing string) []byte {
	if cwd == "" || !guardNeedsLines(root) {
		return nil
	}
	deadline := time.Now().Add(hookGitTimeout)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	top, ok := gitRun(ctx, cwd, "rev-parse", "--show-toplevel")
	if !ok {
		return nil
	}
	top = strings.TrimSpace(top)
	head := map[string]string{}
	if out, ok := gitRun(ctx, top, "ls-tree", "-r", "-z", "--full-tree", "HEAD"); ok { // no HEAD yet: every file differs
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
	files := map[string]keptFile{}
	size := 0
	for _, line := range strings.Split(listing, "\n") {
		if len(files) >= guardFilesMax || time.Now().After(deadline) {
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
		b, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(p)))
		if err != nil || len(b) > guardFileBytesMax || isBinary(b) || gitBlobID(b, len(meta[1])) != meta[1] {
			continue
		}
		counts := map[string]int{}
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) != "" {
				counts[lineHash(key, l)]++
			}
		}
		size += len(p) + len(meta[1]) + len(counts)*42
		if size > keptLinesMax {
			break
		}
		files[p] = keptFile{Blob: meta[1], Lines: counts}
	}
	if len(files) == 0 {
		return nil
	}
	body, err := json.Marshal(files)
	if err != nil {
		return nil
	}
	return append([]byte(header+"#key "+hex.EncodeToString(key)+"\n"), body...)
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
	if !found || !bytes.HasPrefix(keyLine, []byte("#key ")) {
		return nil
	}
	key, err := hex.DecodeString(string(keyLine[len("#key "):]))
	if err != nil || len(key) == 0 {
		return nil
	}
	k := &keptLines{key: key}
	if json.Unmarshal(body, &k.files) != nil {
		return nil
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

// addedAgainst returns the lines of now that the kept file does not have (a line kept n times is matched n times).
func (k *keptLines) addedAgainst(f keptFile, now []byte) []string {
	count := make(map[string]int, len(f.Lines))
	for h, n := range f.Lines {
		count[h] = n
	}
	return linesNotIn(now, func(l string) string { return lineHash(k.key, l) }, count)
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
