package notebook

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

// Lookups are bounded receipts for actual manual reads, not usefulness assessments.
type Lookup struct {
	ID    string                     `json:"id"`
	Query string                     `json:"query"`
	Pages []store.KnowledgeReference `json:"pages"`
}

type Discovery struct {
	LookupID string `json:"lookup_id"`
	PageID   string `json:"page_id"`
	Revision string `json:"revision"`
	Context  string `json:"context"`
	Decision string `json:"decision"`
	Evidence string `json:"evidence"`
}

func (p Page) DiscoveryContext() string { return p.discovered }

type searchJournal struct {
	Lookups     []Lookup    `json:"lookups"`
	Discoveries []Discovery `json:"discoveries"`
}

func readSearchJournal(dir string) (searchJournal, error) {
	var j searchJournal
	f, err := os.OpenFile(filepath.Join(dir, ".searches.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return j, errors.New("the search journal must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return j, errors.New("cannot read the bounded search journal")
	}
	if json.Unmarshal(data, &j) != nil || len(j.Lookups) > 64 || len(j.Discoveries) > 128 {
		return j, errors.New("cannot read the wiki search records")
	}
	for _, l := range j.Lookups {
		id := l.ID
		l.ID = ""
		data, _ := json.Marshal(l)
		if digest(data)[:16] != id || len(l.Pages) > 2 || len(l.Query) > 4000 {
			return j, errors.New("invalid wiki lookup receipt")
		}
		for _, p := range l.Pages {
			if !idPattern.MatchString(p.ID) || !idPattern.MatchString(p.Revision) {
				return j, errors.New("invalid wiki lookup revision")
			}
		}
	}
	for _, d := range j.Discoveries {
		if !idPattern.MatchString(d.LookupID) || !idPattern.MatchString(d.PageID) || !idPattern.MatchString(d.Revision) {
			return j, errors.New("invalid saved discovery")
		}
		for _, s := range []string{d.Context, d.Decision, d.Evidence} {
			if strings.TrimSpace(s) == "" || len(s) > 1000 || !utf8.ValidString(s) {
				return j, errors.New("invalid saved discovery text")
			}
		}
	}
	return j, nil
}

func saveSearchJournal(dir string, j searchJournal) error {
	for {
		data, err := json.Marshal(j)
		if err != nil {
			return err
		}
		if len(data) <= 1<<20 {
			return writeFile(filepath.Join(dir, ".searches.json"), data)
		}
		// JSON escaping can expand otherwise valid text. Trim older records from
		// the larger collection, keeping the latest lookup and discovery intact.
		lookups, _ := json.Marshal(j.Lookups)
		discoveries, _ := json.Marshal(j.Discoveries)
		if len(j.Lookups) > 1 && (len(j.Discoveries) <= 1 || len(lookups) >= len(discoveries)) {
			j.Lookups = j.Lookups[1:]
		} else if len(j.Discoveries) > 1 {
			j.Discoveries = j.Discoveries[1:]
		} else {
			return errors.New("the latest search records exceed the journal budget")
		}
	}
}

// RecordLookup is called at the public query boundary, after pagination. Internal searches do not create receipts.
func (b Book) RecordLookup(query string, pages []Page) (*Lookup, error) {
	if strings.TrimSpace(query) == "" || len(pages) == 0 {
		return nil, nil
	}
	if len(pages) > 2 {
		return nil, errors.New("a lookup records at most two returned article bodies")
	}
	if len(query) > 4000 || !utf8.ValidString(query) {
		return nil, errors.New("lookup query must be UTF-8, at most 4000 bytes")
	}
	l := Lookup{Query: query}
	for _, p := range pages {
		l.Pages = append(l.Pages, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
	}
	data, _ := json.Marshal(l)
	l.ID = digest(data)[:16]
	dir, err := b.directory(true, "searches")
	if err != nil {
		return nil, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	kept := []Lookup{}
	for _, old := range j.Lookups {
		if old.ID != l.ID {
			kept = append(kept, old)
		}
	}
	j.Lookups = append(kept, l)
	if len(j.Lookups) > 64 {
		j.Lookups = j.Lookups[len(j.Lookups)-64:]
	}
	if err = saveSearchJournal(dir, j); err != nil {
		return nil, err
	}
	return &l, nil
}

// Discover records an explicit, evidenced use of a returned revision. It never creates a human truth or an automatic evaluation.
func (b Book) Discover(d Discovery) error {
	if !idPattern.MatchString(d.LookupID) || !idPattern.MatchString(d.PageID) || !idPattern.MatchString(d.Revision) {
		return errors.New("discovery needs a lookup, article and revision")
	}
	for _, text := range []string{d.Context, d.Decision, d.Evidence} {
		if strings.TrimSpace(text) == "" || len(text) > 1000 || !utf8.ValidString(text) {
			return errors.New("discovery context, decision and evidence must each be 1 to 1000 UTF-8 bytes")
		}
	}
	// Use the same page lock as editing, so a deletion or replacement cannot pass between validation and saving.
	pageDir, err := b.directory(false, "pages")
	if err != nil {
		return err
	}
	unlockPages, err := lock(pageDir)
	if err != nil {
		return err
	}
	defer unlockPages()
	live, err := b.Live()
	if err != nil {
		return err
	}
	found := false
	for _, p := range live {
		found = found || p.ID == d.PageID && p.Revision == d.Revision
	}
	if !found {
		return errors.New("the discovery's article revision is no longer live; read it again")
	}
	dir, err := b.directory(false, "searches")
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := readSearchJournal(dir)
	if err != nil {
		return err
	}
	// Successful retries do not require an older lookup to remain in the rolling read window.
	for _, old := range j.Discoveries {
		if old.LookupID == d.LookupID && old.PageID == d.PageID {
			if old == d {
				return nil
			}
			return errors.New("a saved discovery cannot be rewritten")
		}
	}
	found = false
	for _, l := range j.Lookups {
		if l.ID == d.LookupID {
			for _, p := range l.Pages {
				found = found || p.ID == d.PageID && p.Revision == d.Revision
			}
		}
	}
	if !found {
		return errors.New("the article revision was not returned by this lookup; query it again")
	}
	j.Discoveries = append(j.Discoveries, d)
	if len(j.Discoveries) > 128 {
		j.Discoveries = j.Discoveries[len(j.Discoveries)-128:]
	}
	return saveSearchJournal(dir, j)
}

// The extra search text comes only from an evidenced use of this exact revision. Normal ranking and the delivery gate still apply.
func (b Book) discoveryContexts() (map[string]string, error) {
	out := map[string]string{}
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	for i := len(j.Discoveries) - 1; i >= 0; i-- {
		d := j.Discoveries[i]
		key := d.PageID + ":" + d.Revision
		if len(out[key])+len(d.Context)+1 <= 4004 {
			out[key] += " " + d.Context
		}
	}
	return out, nil
}
