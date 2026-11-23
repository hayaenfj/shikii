package notebook

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

func (b Book) pages() ([]Page, []Issue, error) {
	b = b.scopedRead()
	if pages, issues, ok := b.snapshotPages(); ok {
		return pages, issues, nil
	}
	pages, issues := []Page{}, []Issue{}
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return pages, issues, nil
	}
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	// Cover every generated page; splitting the reply is done by QueryPage.
	entries, err := f.ReadDir(-1)
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	b.snapshotDirectory(dir, entries)
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".md")
		if !idPattern.MatchString(id) || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		data, err := b.sharedArticle(filepath.Join(dir, e.Name()))
		if err != nil {
			issues = append(issues, Issue{Kind: "unreadable-page", Page: id})
			continue
		}
		p, err := b.readPage(filepath.Join(dir, e.Name()), data)
		if err != nil || p.ID != id {
			issues = append(issues, Issue{Kind: "changed-page", Page: id})
			continue
		}
		pages = append(pages, p)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	b.rememberSnapshotPages(pages, issues)
	return pages, issues, nil
}

// sourceIssues checks the sources one page cites (see sourceCheck, which a loop over many pages uses instead).
func (b Book) sourceIssues(p Page) []Issue {
	return b.sourceCheck().issues(p)
}

// sourceReads is what one loop over pages has read of the sources they cite: the sources directory, the hash of each source record and
// the hash of each archived original, each once, a read that failed kept as it failed (so the loop is of one mind: a source that is
// missing is missing for every page that cites it). Inside a view it also takes from, and gives to, the view's keptSources.
type sourceReads struct {
	mu     sync.Mutex
	dirs   map[string]dirRead    // by the project and the store the directory belongs to
	hashes map[string]digestRead // by the path of the source record
	texts  map[string]digestRead // by the path of the archived original
	kept   *keptSources          // the view's, or nil when the Book shares no view
}

func newSourceReads(kept *keptSources) *sourceReads {
	return &sourceReads{dirs: map[string]dirRead{}, hashes: map[string]digestRead{}, texts: map[string]digestRead{}, kept: kept}
}

// keptSources is what a view keeps of the sources its loops read, for the loops that come after them in the request: only the reads that
// succeeded. A source is written once and never rewritten, so what was read stays true; a source that could not be read (it is missing,
// its folder is missing, it cannot be opened) may be saved a moment later in the same request, and then the next loop must see it. The
// failure is remembered by the loop that met it (sourceReads) and by no other.
type keptSources struct {
	mu     sync.Mutex
	dirs   map[string]string
	hashes map[string]string
	texts  map[string]string
}

func newKeptSources() *keptSources {
	return &keptSources{dirs: map[string]string{}, hashes: map[string]string{}, texts: map[string]string{}}
}

// dirRead is what resolving the sources directory gave.
type dirRead struct {
	dir string
	err error
}

// digestRead is what reading a file for its hash gave.
type digestRead struct {
	hash string
	err  error
}

// directory is the sources directory of b, resolved once for the loop (and once for the view when it was found). Each read below is made
// under the lock, so it is made once however many callers ask.
func (r *sourceReads) directory(b Book) (string, error) {
	key := b.Root + "\x00" + b.Scope + "\x00" + b.name
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := r.dirs[key]
	if !ok {
		if r.kept != nil {
			r.kept.mu.Lock()
			got.dir, ok = r.kept.dirs[key]
			r.kept.mu.Unlock()
		}
		if !ok {
			got.dir, got.err = b.directory(false, "sources")
			if r.kept != nil && got.err == nil {
				r.kept.mu.Lock()
				r.kept.dirs[key] = got.dir
				r.kept.mu.Unlock()
			}
		}
		r.dirs[key] = got
	}
	return got.dir, got.err
}

// record is the hash of the source record at path (or why it cannot be read), read once.
func (r *sourceReads) record(b Book, path string) digestRead {
	return r.hashOf(r.hashes, func(k *keptSources) map[string]string { return k.hashes }, path, func() (string, error) { return b.sharedDigest(path) })
}

// original is the hash of the archived long text at path (or why it cannot be read), read once.
func (r *sourceReads) original(b Book, path string) digestRead {
	return r.hashOf(r.texts, func(k *keptSources) map[string]string { return k.texts }, path, func() (string, error) {
		text, err := b.sharedOriginals(path)
		return digest(text), err
	})
}

// hashOf is the read of path from the loop's reads, else the view's kept ones, else the file (read made under the lock, so once).
func (r *sourceReads) hashOf(loop map[string]digestRead, kept func(*keptSources) map[string]string, path string, read func() (string, error)) digestRead {
	r.mu.Lock()
	defer r.mu.Unlock()
	got, ok := loop[path]
	if ok {
		return got
	}
	if r.kept != nil {
		r.kept.mu.Lock()
		got.hash, ok = kept(r.kept)[path]
		r.kept.mu.Unlock()
	}
	if !ok {
		got.hash, got.err = read()
		if r.kept != nil && got.err == nil {
			r.kept.mu.Lock()
			kept(r.kept)[path] = got.hash
			r.kept.mu.Unlock()
		}
	}
	loop[path] = got
	return got
}

// sourceCheck checks the sources of the pages of one loop. Every loop begins with a new one, so a read that failed is made again by the
// next loop, and a source saved after the loop began is seen by the next loop. Inside a view (a Book that shares one: ReadView,
// sharingParses) the loops of the request share the reads that succeeded: each source record and each archived original that was read is
// read once for the whole request, however many pages and however many steps of it cite it, so a source that changes after the request
// read it is seen by the next request, not by a later step of this one. A snapshot keeps its own sharing (sharedCheck, which ValidateRead
// checks again), so a Book with one reads as it did.
type sourceCheck struct {
	b     Book
	reads *sourceReads
}

func (b Book) sourceCheck() *sourceCheck {
	b = b.scopedRead()
	var kept *keptSources
	if b.snapshot == nil && b.read != nil {
		kept = b.read.sources
	}
	return &sourceCheck{b: b, reads: newSourceReads(kept)}
}

func (c *sourceCheck) issues(p Page) []Issue {
	dir, dirErr := c.reads.directory(c.b)
	issues := []Issue{}
	for _, s := range p.Sources {
		if !idPattern.MatchString(s.ID) {
			issues = append(issues, Issue{Kind: "invalid-source", Page: p.ID})
			continue
		}
		record := digestRead{err: dirErr}
		if dirErr == nil {
			record = c.reads.record(c.b, filepath.Join(dir, s.ID+".json"))
		}
		if record.err != nil {
			issues = append(issues, Issue{Kind: "missing-source", Page: p.ID, Source: s.ID})
		} else if record.hash != s.Hash {
			issues = append(issues, Issue{Kind: "changed-source", Page: p.ID, Source: s.ID})
		}
		if s.TextHash != "" {
			text := digestRead{err: dirErr}
			if dirErr == nil {
				text = c.reads.original(c.b, filepath.Join(dir, s.ID+".text.json"))
			}
			if text.err != nil || text.hash != s.TextHash {
				issues = append(issues, Issue{Kind: "changed-source-text", Page: p.ID, Source: s.ID})
			}
		}
	}
	return issues
}
