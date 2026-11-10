package notebook

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
)

// pageParses counts the full parse-and-verify passes over an article in this process. Reading the bytes is not counted: a view
// shares only the pass, and every read still opens the file and compares all its bytes.
var pageParses atomic.Uint64

// PageParses returns how many times this process parsed and verified an article (tests hold a request to one pass per article).
func PageParses() uint64 { return pageParses.Load() }

// ReadView shares validated parsing within one request. Every access still
// opens and reads the regular file, compares all bytes, and rechecks sources and
// watched premises. It is neither a filesystem snapshot nor a persistent cache.
func (b Book) ReadView() Book {
	b.name = ""
	b.snapshot = nil
	b.read = &readView{pages: map[string]readPage{}}
	return b
}

// CurrentPage checks one current article without scanning unrelated articles.
// A missing, modified or uncheckable article is unavailable, as in an exact
// current Query. Historical reads still use their explicit revision.
func (b Book) CurrentPage(id string) (Page, bool, error) {
	if !idPattern.MatchString(id) {
		return Page{}, false, errors.New("invalid page ID")
	}
	b = b.scopedRead()
	dir, err := b.directory(false, "pages")
	if os.IsNotExist(err) {
		return Page{}, false, nil
	}
	if err != nil {
		return Page{}, false, err
	}
	path := filepath.Join(dir, id+".md")
	raw, err := b.sharedArticle(path)
	if err != nil {
		return Page{}, false, nil
	}
	p, err := b.readPage(path, raw)
	if err != nil || p.ID != id || len(b.sourceIssues(p)) > 0 {
		return Page{}, false, nil
	}
	p.Recheck = b.premiseChanges(p)
	return p, true, nil
}

type readPage struct {
	raw  []byte
	page Page
	cost int
}

type readView struct {
	mu    sync.Mutex
	bytes int
	pages map[string]readPage
}

func (b Book) readPage(path string, raw []byte) (Page, error) {
	if b.read != nil {
		b.read.mu.Lock()
		old, ok := b.read.pages[path]
		b.read.mu.Unlock()
		if ok && bytes.Equal(raw, old.raw) {
			return copyReadPage(old.page), nil
		}
	}
	pageParses.Add(1)
	p, err := parsePage(raw)
	if err != nil || p.Revision != revision(p) || !b.matchesPage(raw, p) {
		return Page{}, errors.New("the generated wiki article has changed")
	}
	if b.read != nil {
		cost := len(raw) + readValueSize(reflect.ValueOf(p))
		b.read.mu.Lock()
		old := b.read.pages[path]
		if cost <= readViewLimit {
			if b.read.bytes-old.cost+cost > readViewLimit {
				b.read.pages = map[string]readPage{}
				b.read.bytes = 0
				old.cost = 0
			}
			b.read.pages[path] = readPage{raw: raw, page: copyReadPage(p), cost: cost}
			b.read.bytes += cost - old.cost
		}
		b.read.mu.Unlock()
	}
	return p, nil
}
