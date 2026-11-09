package notebook

import (
	"errors"
	"sort"
)

// PreviewCreate validates a new note and fixes its expected revision without saving it.
func (b Book) PreviewCreate(d Draft) (Result, error) {
	d.CreateOnly = true
	d.preview = true
	return b.Ingest(d)
}

// IngestPreparedCreate publishes the exact validated revision, including its watched premises.
func (b Book) IngestPreparedCreate(d Draft, prepared Page) (Result, error) {
	if !idPattern.MatchString(prepared.ID) || !idPattern.MatchString(prepared.Revision) {
		return Result{}, errors.New("a prepared new article needs its validated id and revision")
	}
	d.CreateOnly = true
	d.preparedCreate = &prepared
	return b.Ingest(d)
}

func (b Book) Ingest(d Draft) (Result, error) {
	return b.ingest(d, false)
}

func (b Book) ingest(d Draft, locked bool) (Result, error) {
	result := Result{Reviewed: []string{}}
	body, err := validateIngestDraft(&d)
	if err != nil {
		return result, err
	}
	dir, err := b.directory(true, "pages")
	if err != nil {
		return result, err
	}
	if !locked {
		unlock, err := lock(dir)
		if err != nil {
			return result, err
		}
		defer unlock()
	}
	sourceDir, err := b.directory(true, "sources")
	if err != nil {
		return result, err
	}
	loaded, err := b.loadIngestSources(sourceDir, d)
	if err != nil {
		return result, err
	}
	pages, refs, raw, texts := loaded.pages, loaded.refs, loaded.raw, loaded.texts
	result.Page, err = b.buildIngestPage(d, body, sourceDir, pages, refs, raw)
	if err != nil {
		return result, err
	}
	if d.preview {
		return result, nil
	}
	return b.publishIngest(dir, sourceDir, d, result, raw, texts)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	sort.Strings(out)
	return out
}

func cyclic(pages map[string]Page) bool {
	state := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, r := range pages[id].Replaces {
			if visit(r) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range pages {
		if visit(id) {
			return true
		}
	}
	return false
}
