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

func (b Book) sourceIssues(p Page) []Issue {
	b = b.scopedRead()
	issues := []Issue{}
	dir, dirErr := b.directory(false, "sources")
	for _, s := range p.Sources {
		if !idPattern.MatchString(s.ID) {
			issues = append(issues, Issue{Kind: "invalid-source", Page: p.ID})
			continue
		}
		var hash string
		err := dirErr
		if err == nil {
			hash, err = b.sharedDigest(filepath.Join(dir, s.ID+".json"))
		}
		if err != nil {
			issues = append(issues, Issue{Kind: "missing-source", Page: p.ID, Source: s.ID})
		} else if hash != s.Hash {
			issues = append(issues, Issue{Kind: "changed-source", Page: p.ID, Source: s.ID})
		}
		if s.TextHash != "" {
			var text []byte
			err := dirErr
			if err == nil {
				text, err = b.sharedOriginals(filepath.Join(dir, s.ID+".text.json"))
			}
			if err != nil || digest(text) != s.TextHash {
				issues = append(issues, Issue{Kind: "changed-source-text", Page: p.ID, Source: s.ID})
			}
		}
	}
	return issues
}
