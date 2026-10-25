package notebook

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

func ordinaryDirectory(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("cannot check where wiki revisions are stored")
	}
	return path, nil
}

func (b Book) References() ([]store.KnowledgeReference, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	out := []store.KnowledgeReference{}
	for _, p := range pages {
		if len(b.sourceIssues(p)) == 0 {
			out = append(out, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
		}
	}
	return out, nil
}

func (b Book) readRevision(query string) (Page, error) {
	parts := strings.Split(strings.TrimPrefix(query, "lesson:"), "@")
	if len(parts) != 2 || !idPattern.MatchString(parts[0]) || !idPattern.MatchString(parts[1]) {
		return Page{}, errors.New("invalid page ID or revision reference")
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		return Page{}, err
	}
	history, err := ordinaryDirectory(filepath.Join(dir, "revisions"))
	if err != nil {
		return Page{}, err
	}
	data, err := readFile(filepath.Join(history, parts[0]+"-"+parts[1]+".md"))
	if err != nil {
		return Page{}, err
	}
	p, err := parsePage(data)
	if err != nil || p.ID != parts[0] || p.Revision != parts[1] || revision(p) != p.Revision || len(b.sourceIssues(p)) > 0 {
		return Page{}, errors.New("cannot check the page's past revision or its sources")
	}
	expected, err := b.pageBytes(p)
	if err != nil || !bytes.Equal(data, expected) {
		return Page{}, errors.New("the page's past revision has been modified")
	}
	return p, nil
}
