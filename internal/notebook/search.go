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

// isContentTerm は内容を担う語: 識別子か、仮名だけでもなく助詞(を に の が は で と も へ)も含まない 2 文字の組。
// 「する」「える」「を直」「直す」「の説」のような文法の境界の組は数えない(監査 R2: それだけで関係ない記事が届いた)。
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

// isIdentifierTerm は英字を含む英数字だけの 3 文字以上の語(ファイル名・コマンド名・識別子)。数字だけ(年など)や日本語の 2 文字の組は含めない。
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
			// 自動配送の門(0.7.4): 数えるのは「内容を担う語」(識別子か、仮名だけでも助詞を含むでもない 2 文字の組)だけ。
			// 依頼側の一致率(内容語の 0.35)か記事側の一致率(題と適用条件の内容語の 0.15)で通し、どちらも内容語 2 語以上。
			// 珍しい識別子(CHANGELOG, Makefile: 記事の半分以下にしか無い英字の語)は 1 語でも通す。プロジェクト名のようにどの記事にも
			// ある識別子は 1 語では通さない(記事が 1 本の wiki では比べようが無いので通す)。
			// 0.7.3 までは依頼の全部の語の割合で決めていて、長い依頼には届かず、短い依頼には遍在する語と文法の組で関係ない記事が届いた。
			// 「珍しい語」= 記事の半分以下にしか無い語(記事が 1 本なら全部)。プロジェクト名のようにどの記事にもある語は、
			// 一致しても根拠にしない(それだけで関係ない記事が届いた)。ただし依頼の内容語がほぼ全部(0.8 以上)記事に合うなら、
			// 遍在する語でも届ける(全記事が同じ話題の wiki で、その話題の依頼が来た時)。
			rare := func(term string) bool { return len(docs) == 1 || frequency[term]*2 <= len(docs) }
			requestTerms, rareRequestTerms, rarePageTerms := 0, 0, 0
			matchedContent, rareMatched := 0, 0
			for term := range terms {
				if isContentTerm(term) {
					requestTerms++
					if rare(term) {
						rareRequestTerms++
					}
				}
			}
			for _, f := range d.fields {
				for term := range f {
					if isContentTerm(term) && rare(term) {
						rarePageTerms++
					}
				}
			}
			strong := false
			for term := range matched {
				if !isContentTerm(term) {
					continue
				}
				matchedContent++
				if rare(term) {
					rareMatched++
					if isIdentifierTerm(term) {
						strong = true
					}
				}
			}
			coverage, minMatched = 0, 2
			switch {
			case strong:
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
