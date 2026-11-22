package notebook

// pageTerms is the terms of the six texts of each page of a list of pages that a ranking uses (title, applies-to, lesson, body,
// rationale, the discovery contexts), split once for the words of several searches over those pages. A ranking of one word needs
// only the terms of that word (searchTermsFor), so the pages are split once for the terms of all the words, and a search of one word
// takes from them the terms of its own: the same terms the ranking would have split for it, so the ranking is the same.
type pageTerms struct {
	wanted map[string]bool     // the terms of all the words the pages were split for
	fields [][]map[string]bool // per page (in the order of the pages given), per text, the terms of the text that are in wanted
}

// splitPages splits the texts of pages once for every term of words (as searchTerms gives them).
func splitPages(pages []Page, words []string) *pageTerms {
	wanted := map[string]bool{}
	for _, word := range words {
		for term := range searchTerms(word) {
			wanted[term] = true
		}
	}
	t := &pageTerms{wanted: wanted, fields: make([][]map[string]bool, len(pages))}
	for i, p := range pages {
		pageTokenizations.Add(1)
		for _, text := range []string{p.Title, p.AppliesTo, p.Lesson, p.Body, p.Rationale, p.discovered} {
			t.fields[i] = append(t.fields[i], searchTermsFor(text, wanted))
		}
	}
	return t
}

// covers reports whether the pages were split for every term of terms.
func (t *pageTerms) covers(terms map[string]bool) bool {
	for term := range terms {
		if !t.wanted[term] {
			return false
		}
	}
	return true
}

// fieldsOf is the terms of the texts of the page at index i that are in terms, as searchTermsFor(text, terms) gives them, or nil
// when some term was not among those the pages were split for (the ranking then splits the page itself).
func (t *pageTerms) fieldsOf(i int, terms map[string]bool) []map[string]bool {
	if i >= len(t.fields) || !t.covers(terms) {
		return nil
	}
	out := make([]map[string]bool, len(t.fields[i]))
	for j, field := range t.fields[i] {
		out[j] = map[string]bool{}
		for term := range terms {
			if field[term] {
				out[j][term] = true
			}
		}
	}
	return out
}

// wordSearcher runs the searches of several words over one reading of the wiki: a wiki query of each word, but the pages are read,
// their sources checked and their texts split once, not once for each word. It is what WordsFoundAlone and the check of the open
// misses ask whether each of their words finds an article.
type wordSearcher struct {
	lessons []Page
	terms   *pageTerms
}

// searcherFor reads the wiki once for the words, which are words of a search (not references such as lesson:<id>: missQuery). It
// fails when a wiki query of the words would fail for what it reads: the pages, their discovery contexts and the pending sources.
func (b Book) searcherFor(words []string) (*wordSearcher, error) {
	b = b.ReadView().scopedRead()
	v, _, err := b.searchPages(words[0], false)
	if err != nil {
		return nil, err
	}
	if _, err := b.PendingSources(); err != nil {
		return nil, err
	}
	return &wordSearcher{lessons: v.Lessons, terms: splitPages(v.Lessons, words)}, nil
}

// searchReading is the reading of the wiki a word search that found no candidate made (View.reading): the pages it ranked, with their
// texts split for the terms of its query. It belongs to the project and the query it was made for.
type searchReading struct {
	root, scope, query string
	searcher           *wordSearcher
}

// searcherAfter is the searcher of words (words of a search, as for searcherFor) over the reading that the search of query made
// (view), so that the wiki is not read again. It reads the wiki (searcherFor) when view carries no reading, or the reading of another
// project or query. When a term of the words is not among those the texts were split for, they are split again for the words, which
// reads nothing.
func (b Book) searcherAfter(query string, view View, words []string) (*wordSearcher, error) {
	r := view.reading
	if r == nil || r.query != query || r.root != b.Root || r.scope != b.Scope {
		return b.searcherFor(words)
	}
	for _, word := range words {
		if !r.searcher.terms.covers(searchTerms(word)) {
			return &wordSearcher{lessons: r.searcher.lessons, terms: splitPages(r.searcher.lessons, words)}, nil
		}
	}
	return r.searcher, nil
}

// finds reports whether a wiki query of the word finds an article (a candidate): word is one of the words searcherFor was given.
func (s *wordSearcher) finds(word string) bool {
	found, _ := rankPagesWith(s.lessons, word, false, s.terms)
	return len(found) > 0
}
