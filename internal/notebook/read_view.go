package notebook

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

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
	raw, err := b.sharedRead(path, readFile)
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
	page []byte
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
			// A fresh decode prevents mutations of a returned page's nested data
			// from changing the validated value used by another read.
			var p Page
			if err := json.Unmarshal(old.page, &p); err != nil {
				return Page{}, err
			}
			return p, nil
		}
	}
	p, err := parsePage(raw)
	if err != nil || p.Revision != revision(p) || !b.matchesPage(raw, p) {
		return Page{}, errors.New("the generated wiki article has changed")
	}
	if b.read != nil {
		data, err := json.Marshal(p)
		if err != nil {
			return Page{}, err
		}
		b.read.mu.Lock()
		old := b.read.pages[path]
		cost := len(raw) + len(data)
		if b.read.bytes-len(old.raw)-len(old.page)+cost <= 8<<20 {
			b.read.pages[path] = readPage{raw: raw, page: data}
			b.read.bytes += cost - len(old.raw) - len(old.page)
		}
		b.read.mu.Unlock()
	}
	return p, nil
}
