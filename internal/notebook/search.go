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

// SearchResult exposes all matching candidates; QueryPage divides large responses.
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
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) || r == 'ー': // the long-vowel mark (ー) has Script Common, so keep it inside katakana words (audit R4)
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

// isContentTerm reports a term that carries content: an identifier, or a 2-character pair that is not kana only and contains no particle (を に の が は で と も へ).
// Pairs on grammatical boundaries such as 「する」「える」「を直」「直す」「の説」 are not counted (audit R2: they alone delivered unrelated pages).
func isContentTerm(term string) bool {
	if isIdentifierTerm(term) {
		return true
	}
	content := false
	for _, r := range term {
		if strings.ContainsRune("をにのがはでともへ", r) {
			return false
		}
		if !unicode.In(r, unicode.Hiragana) {
			content = true
		}
	}
	return content
}

// cluster is a run in the request text covered by matched 2-character pairs (overlapping pairs form one). kana reports whether it contains hiragana.
type cluster struct {
	start, end int
	kana       bool
}

// termClusters returns the runs that terms (2-character pairs) cover in runes (the lowercased request text). Identifiers are excluded. Compares without building strings (audit R4: performance).
func termClusters(runes []rune, terms map[string]bool) []cluster {
	covered := make([]bool, len(runes))
	for term := range terms {
		if isIdentifierTerm(term) {
			continue
		}
		tr := []rune(term)
		if len(tr) == 0 || len(tr) > len(runes) {
			continue
		}
		for i := 0; i+len(tr) <= len(runes); i++ {
			match := true
			for j, r := range tr {
				if runes[i+j] != r {
					match = false
					break
				}
			}
			if match {
				for j := range tr {
					covered[i+j] = true
				}
			}
		}
	}
	var out []cluster
	for i := 0; i < len(covered); {
		if !covered[i] {
			i++
			continue
		}
		c := cluster{start: i}
		for i < len(covered) && covered[i] {
			c.kana = c.kana || unicode.In(runes[i], unicode.Hiragana)
			i++
		}
		c.end = i
		out = append(out, c)
	}
	return out
}

// contentClusters is the number of "runs" terms cover in the request text. Overlapping 2-character pairs (表示 + 示す form the one word 「表示す」) count as one,
// and identifiers count one word each. Counting 2-character pairs as 2 words let a one-word request meet "2 content words" and deliver unrelated pages (audit R3).
// A run of 3 or more characters without hiragana (テスト, データ, 日本語) is one content word worth 2 words. A run ending in hiragana (表示す, 更新す)
// is a verb stem, so it is worth 1 word (not enough to deliver alone).
func contentClusters(runes []rune, terms map[string]bool) int {
	n := 0
	for term := range terms {
		if isIdentifierTerm(term) {
			n++
		}
	}
	for _, c := range termClusters(runes, terms) {
		if c.end-c.start >= 3 && !c.kana {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// hasKanaFreeCluster reports whether, among the runs terms cover in the request text, one of 2 or more characters has no hiragana (a kanji or katakana word).
// Runs containing hiragana (表示す, 更新し) are verb stems and are not counted (they alone delivered unrelated pages; audit R3).
func hasKanaFreeCluster(runes []rune, terms map[string]bool) bool {
	for _, c := range termClusters(runes, terms) {
		if c.end-c.start >= 2 && !c.kana {
			return true
		}
	}
	return false
}

// isIdentifierTerm reports a word of 3 or more characters made only of letters and digits and containing a letter (file names, command names, identifiers). Digits only (such as years) and Japanese 2-character pairs are excluded.
func isIdentifierTerm(term string) bool {
	if len(term) < 3 {
		return false
	}
	letter := false
	for _, r := range term {
		if !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')) {
			return false
		}
		if unicode.IsLetter(r) {
			letter = true
		}
	}
	return letter
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
	// The request-side count for the automatic-delivery gate (runs of content words) does not depend on the page, so count it once before the loop (audit R4: performance)
	queryRunes := []rune(strings.ToLower(query))
	rare := func(term string) bool { return len(docs) == 1 || frequency[term]*2 <= len(docs) }
	requestContent, rareRequest := map[string]bool{}, map[string]bool{}
	for term := range terms {
		if isContentTerm(term) {
			requestContent[term] = true
			if rare(term) {
				rareRequest[term] = true
			}
		}
	}
	requestTerms, rareRequestTerms := contentClusters(queryRunes, requestContent), contentClusters(queryRunes, rareRequest)
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
		coverage := float64(len(matched)) / float64(max(len(terms), 1))
		minMatched := 2
		if recall {
			// Automatic-delivery gate (0.7.4): count only "terms that carry content" (identifiers, or 2-character pairs that are neither kana only nor contain a particle).
			// Pass on the request-side match rate (0.35 of content words) or the page-side match rate (0.15 of content words in title and applies-to), both with 2 or more content words.
			// A rare identifier (CHANGELOG, Makefile: an alphabetic word found in at most half the pages) passes even alone. An identifier found in every page,
			// such as the project name, does not pass alone (in a wiki with a single page there is nothing to compare, so it passes).
			// Up to 0.7.3 the gate used the share of all words in the request: long requests got nothing, and short ones got unrelated pages through ubiquitous words and grammatical pairs.
			// "Rare word" = a word found in at most half the pages (all of them if there is one page). A word found in every page, such as the project name,
			// is not evidence even when it matches (it alone delivered unrelated pages). But if almost all (0.8 or more) of the request's content words match the page,
			// deliver even with ubiquitous words (when every page in the wiki is on the same topic and a request on that topic comes in).
			rarePageTerms := 0
			for _, f := range d.fields {
				for term := range f {
					if isContentTerm(term) && rare(term) {
						rarePageTerms++
					}
				}
			}
			strong := false
			matchedContentSet, rareMatchedSet := map[string]bool{}, map[string]bool{}
			for term := range matched {
				if !isContentTerm(term) {
					continue
				}
				matchedContentSet[term] = true
				if rare(term) {
					rareMatchedSet[term] = true
					if isIdentifierTerm(term) {
						strong = true
					}
				}
			}
			matchedContent, rareMatched := contentClusters(queryRunes, matchedContentSet), contentClusters(queryRunes, rareMatchedSet)
			// A 1–2 word request such as 「監査して」 delivers when a word without kana (監査) matches the page title. A match only in applies-to (説明, 表示) does not deliver
			titleRare := map[string]bool{}
			for term := range rareMatchedSet {
				if d.fields[0][term] {
					titleRare[term] = true
				}
			}
			strongTitle := requestTerms <= 2 && hasKanaFreeCluster(queryRunes, titleRare)
			coverage, minMatched = 0, 2
			switch {
			case strongTitle, strong && (requestTerms <= 3 || (rareRequestTerms > 0 && float64(rareMatched)/float64(rareRequestTerms) >= .35)):
				// A single rare identifier delivers only when the request has few content words, or the request-side match rate is enough.
				// A single word like hook inside a long request delivered an unrelated page (measured 2026-10-23)
				coverage, minMatched = threshold, 1
			case rareMatched >= 2 && ((rareRequestTerms > 0 && float64(rareMatched)/float64(rareRequestTerms) >= .35) || (rarePageTerms > 0 && float64(rareMatched)/float64(rarePageTerms) >= .15)):
				coverage = threshold
			case matchedContent >= 2 && requestTerms > 0 && float64(matchedContent)/float64(requestTerms) >= .8:
				coverage = threshold
			}
			if len(matched) < minMatched || coverage < threshold {
				continue
			}
		} else if len(terms) == 0 || (len(terms) > 1 && len(matched) < minMatched) || coverage < threshold {
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
