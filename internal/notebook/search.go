package notebook

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Match explains retrieval, not confidence in the lesson's meaning.
type Match struct {
	ID       string   `json:"id"`
	Revision string   `json:"revision"`
	Fields   []string `json:"fields"`
	Terms    []string `json:"terms"`
}

// SearchResult keeps a small shortlist separate from the two full articles.
// Total counts valid matches, not the completeness or correctness of retrieval.
type SearchResult struct {
	Total      int               `json:"total"`
	Candidates []SearchCandidate `json:"candidates"`
}

type SearchCandidate struct {
	Match
	Title     string `json:"title"`
	AppliesTo string `json:"applies_to"`
}

func searchTerms(text string) map[string]bool {
	out := map[string]bool{}
	var word, ja []rune
	flush := func() {
		if len(word) >= 2 {
			out[string(word)] = true
		}
		for i := 1; i < len(ja); i++ {
			out[string(ja[i-1:i+1])] = true
		}
		word, ja = nil, nil
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
			if len(word) > 0 {
				flush()
			}
			ja = append(ja, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			if len(ja) > 0 {
				flush()
			}
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	for _, term := range []string{"the", "and", "for", "with", "this", "that", "please"} {
		delete(out, term)
	}
	return out
}

type searchDocument struct {
	page   Page
	fields []map[string]bool
}

func rankPages(pages []Page, query string, recall bool) ([]Page, []Match) {
	names, weights := []string{"title", "applies_to", "lesson", "body"}, []float64{4, 3, 2, 1}
	if recall {
		names, weights = names[:2], weights[:2]
	}
	terms := searchTerms(query)
	docs, frequency := []searchDocument{}, map[string]int{}
	for _, p := range pages {
		d := searchDocument{page: p}
		seen := map[string]bool{}
		texts := []string{p.Title, p.AppliesTo, p.Lesson, p.Body}
		for _, text := range texts[:len(names)] {
			field := searchTerms(text)
			d.fields = append(d.fields, field)
			for term := range field {
				seen[term] = true
			}
		}
		for term := range seen {
			frequency[term]++
		}
		docs = append(docs, d)
	}
	type ranked struct {
		page  Page
		match Match
		score float64
	}
	results := []ranked{}
	for _, d := range docs {
		m := Match{ID: d.page.ID, Revision: d.page.Revision, Fields: []string{}, Terms: []string{}}
		matched, score, fields := map[string]bool{}, 0.0, map[string]bool{}
		for term := range terms {
			best := 0.0
			for i, f := range d.fields {
				if f[term] {
					best = math.Max(best, weights[i])
					fields[names[i]] = true
				}
			}
			if best > 0 {
				matched[term] = true
				score += best * (1 + math.Log(1+float64(len(docs))/float64(1+frequency[term])))
			}
		}
		threshold := .18
		if recall {
			threshold = .35
		}
		if len(terms) == 0 || (len(terms) > 1 && len(matched) < 2) || float64(len(matched))/float64(len(terms)) < threshold {
			continue
		}
		for term := range matched {
			m.Terms = append(m.Terms, term)
		}
		sort.Strings(m.Terms)
		for _, name := range names {
			if fields[name] {
				m.Fields = append(m.Fields, name)
			}
		}
		results = append(results, ranked{d.page, m, score / float64(len(terms))})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	out, matches := []Page{}, []Match{}
	for _, r := range results {
		out = append(out, r.page)
		matches = append(matches, r.match)
	}
	return out, matches
}
