package notebook

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (b Book) publishIngest(dir, sourceDir string, d Draft, result Result, raw, texts map[string][]byte) (Result, error) {
	for id, data := range raw {
		if err := immutable(filepath.Join(sourceDir, id+".json"), data); err != nil {
			return result, err
		}
		if text := texts[id]; len(text) > 0 {
			if err := immutableLarge(filepath.Join(sourceDir, id+".text.json"), text); err != nil {
				return result, err
			}
		}
	}
	if p := result.Page; p != nil {
		data, err := b.pageBytes(*p)
		if err != nil {
			return result, err
		}
		history := filepath.Join(dir, "revisions")
		if err := os.Mkdir(history, 0o700); err != nil && !os.IsExist(err) {
			return result, err
		}
		if info, err := os.Lstat(history); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return result, errors.New("wiki revisions must be an ordinary directory")
		}
		// The same content is the same revision. A revision file written before 0.9.2 holds that page with the old fixed words; it is the same
		// page (matchesPage), so it is kept as it is instead of being reported as changed (audit 0.9.2, low 3). Any other difference is refused.
		revisionFile := filepath.Join(history, p.ID+"-"+p.Revision+".md")
		if old, err := readFile(revisionFile); err != nil || !b.matchesPage(old, *p) {
			if err := immutable(revisionFile, data); err != nil {
				return result, err
			}
		}
		if err := writeFile(filepath.Join(dir, p.ID+".md"), data); err != nil {
			return result, err
		}
		result.committed = true
	}
	pages, _, err := b.pages()
	if err != nil {
		return result, err
	}
	if err := writeFile(filepath.Join(dir, "index.md"), index(pages)); err != nil {
		return result, err
	}
	key := "review " + strings.Join(unique(d.Sources), ",")
	if p := result.Page; p != nil {
		key = "ingest " + p.ID + "@" + p.Revision
	}
	if err := logEvent(dir, key); err != nil {
		return result, err
	}
	for id := range raw {
		if err := immutable(filepath.Join(sourceDir, id+".reviewed"), []byte("reviewed as conversation; not a truth label\n")); err != nil {
			return result, err
		}
		result.Reviewed = append(result.Reviewed, id)
	}
	sort.Strings(result.Reviewed)
	return result, nil
}
