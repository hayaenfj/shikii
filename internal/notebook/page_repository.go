package notebook

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// sourceCheck checks the sources of the pages of one loop. It lives as long as the loop and no longer: it resolves the sources
// directory the first time a page is checked, and reads each source record and each archived original once however many of the pages
// cite it. Every loop begins with a new one, so a source that changed after the loop began is seen by the next loop.
type sourceCheck struct {
	b       Book
	dir     string
	dirErr  error
	started bool
	hashes  map[string]digestRead // by the path of the source record
	texts   map[string]digestRead // by the path of the archived original
}

// digestRead is what reading a file for its hash gave.
type digestRead struct {
	hash string
	err  error
}

func (b Book) sourceCheck() *sourceCheck {
	return &sourceCheck{b: b.scopedRead(), hashes: map[string]digestRead{}, texts: map[string]digestRead{}}
}

func (c *sourceCheck) issues(p Page) []Issue {
	if !c.started {
		c.dir, c.dirErr = c.b.directory(false, "sources")
		c.started = true
	}
	issues := []Issue{}
	for _, s := range p.Sources {
		if !idPattern.MatchString(s.ID) {
			issues = append(issues, Issue{Kind: "invalid-source", Page: p.ID})
			continue
		}
		record := c.record(s.ID)
		if record.err != nil {
			issues = append(issues, Issue{Kind: "missing-source", Page: p.ID, Source: s.ID})
		} else if record.hash != s.Hash {
			issues = append(issues, Issue{Kind: "changed-source", Page: p.ID, Source: s.ID})
		}
		if s.TextHash != "" {
			if text := c.original(s.ID); text.err != nil || text.hash != s.TextHash {
				issues = append(issues, Issue{Kind: "changed-source-text", Page: p.ID, Source: s.ID})
			}
		}
	}
	return issues
}

// record is the hash of the source record id (or why it cannot be read), read once for the loop.
func (c *sourceCheck) record(id string) digestRead {
	if c.dirErr != nil {
		return digestRead{err: c.dirErr}
	}
	path := filepath.Join(c.dir, id+".json")
	got, ok := c.hashes[path]
	if !ok {
		got.hash, got.err = c.b.sharedDigest(path)
		c.hashes[path] = got
	}
	return got
}

// original is the hash of the archived long text of the source id (or why it cannot be read), read once for the loop.
func (c *sourceCheck) original(id string) digestRead {
	if c.dirErr != nil {
		return digestRead{err: c.dirErr}
	}
	path := filepath.Join(c.dir, id+".text.json")
	got, ok := c.texts[path]
	if !ok {
		text, err := c.b.sharedOriginals(path)
		got = digestRead{digest(text), err}
		c.texts[path] = got
	}
	return got
}
