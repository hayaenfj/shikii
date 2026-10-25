// Package handoff handles each project's .shikii/handoff/ (the place for work records).
//
// The roles of the three places (0.8.1):
//   - handoff: work files rewritten along the way (audit reports, release steps and notes, diffs, images). Both people and agents read and write them
//   - wiki sources (records/wiki-sources): evidence as of when it was checked. Not changed after writing
//   - wiki articles: lessons that help the next request
//
// Fixed rules (there are no options): never read or write outside the place (closed with os.Root). Links are not followed and not listed.
// Hidden files (names starting with .) and names containing control or format characters are not handled. Links in intermediate folders and hard links are not handled either. Only UTF-8 text is written, up to 1 MiB per call. Text is read up to 4 MiB.
// Neither gc nor update deletes the contents (only the wipe across 0.7.0 removes them, together with the other places).
package handoff

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Dir is the name of the work-record place under .shikii/.
const Dir = "handoff"

const (
	writeMax = 1 << 20 // size that can be written in one call
	readMax  = 4 << 20 // size returned as text
	pathMax  = 256

	staleTempAge = 24 * time.Hour // time before a leftover temp file from a crash is cleaned up
)

// Entry is one item of the list. Path is relative to the place (separated by /).
type Entry struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Kind     string    `json:"kind"` // text / image / other
}

// File is one item that was read. For non-text items Content is empty and only Kind and Size are returned.
type File struct {
	Entry
	Content string `json:"content,omitempty"`
	Note    string `json:"note,omitempty"`
}

var textExt = map[string]bool{".md": true, ".txt": true, ".json": true, ".jsonl": true, ".patch": true, ".diff": true, ".log": true, ".csv": true, ".tsv": true,
	".yaml": true, ".yml": true, ".toml": true, ".go": true, ".js": true, ".mjs": true, ".ts": true, ".py": true, ".sh": true, ".html": true, ".css": true, ".svelte": true, ".sql": true, ".xml": true}

// ImageTypes are the extensions and Content-Types the page serves as images. svg can hold scripts, so it is not served as an image.
var ImageTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

// KindOf decides the kind from the extension.
func KindOf(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if _, ok := ImageTypes[ext]; ok {
		return "image"
	}
	if textExt[ext] || ext == "" {
		return "text"
	}
	return "other"
}

// Clean checks a path relative to the place and normalizes it. Absolute paths, .., hidden names, \ and empty segments are refused.
func Clean(p string) (string, error) {
	p = strings.TrimPrefix(p, ".shikii/handoff/") // the form written in a source's reference (.shikii/handoff/<path>) can be read as is
	if p == "" || len(p) > pathMax || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("write a work-record path relative to the place (.shikii/handoff/): %q", p)
	}
	for _, c := range p {
		if unicode.IsControl(c) || unicode.Is(unicode.Bidi_Control, c) || c == '\uFEFF' {
			return "", fmt.Errorf("a work-record path cannot contain control or format characters: %q", p)
		}
	}
	parts := strings.Split(p, "/")
	for _, s := range parts {
		if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") {
			return "", fmt.Errorf("a work-record path cannot contain empty, ., .. or hidden names: %q", p)
		}
	}
	return strings.Join(parts, "/"), nil
}

// Book is the place of one project. Store is the path of .shikii/.
type Book struct{ Store string }

func (b Book) dir() string { return filepath.Join(b.Store, Dir) }

// Ensure creates the place (called by init).
func (b Book) Ensure() error { return os.MkdirAll(b.dir(), 0o700) }

func (b Book) open() (*os.Root, error) {
	if info, err := os.Lstat(b.dir()); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("the work-record place (.shikii/handoff) is a link. Links are not followed")
	}
	r, err := os.OpenRoot(b.dir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fs.ErrNotExist
	}
	return r, err
}

// List returns every file in the place, newest update first (no limit on the count). Empty if the place does not exist.
func (b Book) List() ([]Entry, error) {
	out := []Entry{}
	r, err := b.open()
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	err = fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() && !d.IsDir() {
			return nil // links and special files are not listed (not followed)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Entry{Path: p, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(p)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// parentsAreFolders reports whether none of the intermediate folders is a link (links pointing inside the place are not followed either).
func parentsAreFolders(r *os.Root, clean string) bool {
	dir := path.Dir(clean)
	if dir == "." {
		return true
	}
	parts := strings.Split(dir, "/")
	for i := range parts {
		info, err := r.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return errors.Is(err, fs.ErrNotExist) // folders that do not exist yet are created when writing
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

// beforeOpen is a hook for tests to swap the file between Lstat and Open (nil in production).
var beforeOpen func()

// Open opens one item (used when the page serves it as is). Links and anything outside the place are not opened.
func (b Book) Open(p string) (*os.File, Entry, error) {
	clean, err := Clean(p)
	if err != nil {
		return nil, Entry{}, err
	}
	r, err := b.open()
	if err != nil {
		return nil, Entry{}, fmt.Errorf("work record not found: %s", clean)
	}
	defer r.Close()
	info, err := r.Lstat(clean)
	if err != nil || !info.Mode().IsRegular() || linkCount(info) > 1 || !parentsAreFolders(r, clean) {
		return nil, Entry{}, fmt.Errorf("work record not found: %s", clean)
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	f, err := r.OpenFile(clean, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return nil, Entry{}, fmt.Errorf("work record not found: %s", clean)
	}
	opened, err := f.Stat() // whether the opened item is the one that was checked (not served if swapped in between). The size is that of the opened item
	if err != nil || !os.SameFile(opened, info) {
		f.Close()
		return nil, Entry{}, fmt.Errorf("work record not found: %s", clean)
	}
	return f, Entry{Path: clean, Size: opened.Size(), Modified: opened.ModTime().UTC(), Kind: KindOf(clean)}, nil
}

// Read reads one item. Text returns its contents; images and others return only kind and size (view them on the page).
func (b Book) Read(p string) (File, error) {
	f, e, err := b.Open(p)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	out := File{Entry: e}
	if e.Kind != "text" {
		out.Note = "Not text, so the contents are not returned. View it under \"Work records\" in the knowledge section of the page, or open the file directly"
		return out, nil
	}
	if e.Size > readMax {
		return File{}, fmt.Errorf("work record %s is %d bytes, more than the %d bytes that can be returned as text. Read the file directly", e.Path, e.Size, readMax)
	}
	data, err := io.ReadAll(io.LimitReader(f, readMax+1))
	if err != nil {
		return File{}, err
	}
	if !utf8.Valid(data) {
		out.Note = "Not UTF-8 text, so the contents are not returned"
		return out, nil
	}
	out.Content = string(data)
	return out, nil
}

// Write writes one item (appends to the end in appendMode). Parent folders are created. Rewrites replace the file from a temp file.
func (b Book) Write(p, content string, appendMode bool) (Entry, error) {
	clean, err := Clean(p)
	if err != nil {
		return Entry{}, err
	}
	if len(content) > writeMax {
		return Entry{}, fmt.Errorf("a work record can be written up to %d bytes per call (got %d bytes). Split it and append", writeMax, len(content))
	}
	if !utf8.ValidString(content) {
		return Entry{}, errors.New("only UTF-8 text can be written to a work record. Put images and other files under .shikii/handoff/ as files")
	}
	if err := b.Ensure(); err != nil {
		return Entry{}, errors.New("cannot create the work-record place (.shikii/handoff)")
	}
	r, err := b.open()
	if err != nil {
		return Entry{}, err
	}
	defer r.Close()
	if !parentsAreFolders(r, clean) {
		return Entry{}, fmt.Errorf("an intermediate folder of work record %s is a link or a file (links are not followed)", clean)
	}
	if dir := path.Dir(clean); dir != "." {
		if err := r.MkdirAll(dir, 0o700); err != nil {
			return Entry{}, fmt.Errorf("cannot create the work-record folder %s", dir)
		}
	}
	removeStaleTemps(r, path.Dir(clean))
	if info, err := r.Lstat(clean); err == nil && (!info.Mode().IsRegular() || linkCount(info) > 1) {
		return Entry{}, fmt.Errorf("work record %s is not a regular file (links, hard links and folders are not written)", clean)
	}
	if appendMode {
		f, err := r.OpenFile(clean, os.O_CREATE|os.O_WRONLY|os.O_APPEND|noFollow, 0o600)
		if err != nil {
			return Entry{}, err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			return Entry{}, err
		}
		if err := f.Close(); err != nil {
			return Entry{}, err
		}
	} else {
		var rnd [8]byte // the temp name differs every time (concurrent writes do not mix, a link placed in advance is not followed, and long names stay within the limit)
		if _, err := rand.Read(rnd[:]); err != nil {
			return Entry{}, err
		}
		tmp := path.Join(path.Dir(clean), ".write-"+hex.EncodeToString(rnd[:]))
		f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Entry{}, err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			_ = r.Remove(tmp)
			return Entry{}, err
		}
		if err := f.Close(); err != nil {
			_ = r.Remove(tmp)
			return Entry{}, err
		}
		if err := r.Rename(tmp, clean); err != nil {
			_ = r.Remove(tmp)
			return Entry{}, err
		}
	}
	info, err := r.Stat(clean)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: clean, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(clean)}, nil
}

// isTempName reports whether name is a replacement temp file name (.write- and 16 hex digits, made by Write with rand).
// Hidden files placed by a person, such as .write-up.md, are not treated as temp files.
func isTempName(name string) bool {
	hexPart, ok := strings.CutPrefix(name, ".write-")
	if !ok || len(hexPart) != 16 {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil && strings.ToLower(hexPart) == hexPart
}

// removeStaleTemps deletes temp files (isTempName) older than one day that were left by a crash during a rewrite.
// They have hidden names, so they appear neither in the list nor on the page, and gc does not touch them; they are cleaned up the next time that folder is written.
// Temp files being written (new ones) and links are not touched.
func removeStaleTemps(r *os.Root, dir string) {
	entries, err := fs.ReadDir(r.FS(), dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleTempAge)
	for _, e := range entries {
		if !isTempName(e.Name()) || !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = r.Remove(path.Join(dir, e.Name()))
		}
	}
}
