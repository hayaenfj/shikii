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

// contentClusters は依頼文の中で terms が占める「連なり」の数。重なる 2 文字の組(表示+示す は「表示す」の 1 語)は 1 つに数え、
// 識別子は 1 語ずつ数える。2 文字の組を 2 語に数えると、1 語の依頼で「内容語 2 語」を満たして関係ない記事が届いた(監査 R3)。
func contentClusters(text string, terms map[string]bool) int {
	n := 0
	runes := []rune(strings.ToLower(text))
	covered := make([]bool, len(runes))
	for term := range terms {
		if isIdentifierTerm(term) {
			n++
			continue
		}
		tr := []rune(term)
		for i := 0; i+len(tr) <= len(runes); i++ {
			if string(runes[i:i+len(tr)]) == term {
				for j := i; j < i+len(tr); j++ {
					covered[j] = true
				}
			}
		}
	}
	for i := range covered {
		if !covered[i] || (i > 0 && covered[i-1]) {
			continue
		}
		end, kana := i, false
		for end < len(covered) && covered[end] {
			kana = kana || unicode.In(runes[end], unicode.Hiragana)
			end++
		}
		// 仮名を含まない 3 文字以上の連なり(テスト、データ、日本語)は 1 つの内容語として 2 語分。
		// 仮名で終わる連なり(表示す、更新す)は動詞の語幹なので 1 語分(それだけで届けない)
		if end-i >= 3 && !kana {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// hasKanaFreeCluster は、terms が依頼文の中で占める連なりのうち、仮名を含まない 2 文字以上のもの(漢字・カタカナの語)があるか。
// 仮名で終わる連なり(表示す、更新す)は動詞の語幹なので数えない(それだけで関係ない記事が届いた。監査 R3)。
func hasKanaFreeCluster(text string, terms map[string]bool) bool {
	runes := []rune(strings.ToLower(text))
	covered := make([]bool, len(runes))
	for term := range terms {
		if isIdentifierTerm(term) {
			continue
		}
		tr := []rune(term)
		for i := 0; i+len(tr) <= len(runes); i++ {
			if string(runes[i:i+len(tr)]) == term {
				for j := i; j < i+len(tr); j++ {
					covered[j] = true
				}
			}
		}
	}
	for i := range covered {
		if !covered[i] || (i > 0 && covered[i-1]) {
			continue
		}
		end, kana := i, false
		for end < len(covered) && covered[end] {
			kana = kana || unicode.In(runes[end], unicode.Hiragana)
			end++
		}
		if end-i >= 2 && !kana {
			return true
		}
	}
	return false
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
			rarePageTerms := 0
			requestContent, rareRequest := map[string]bool{}, map[string]bool{}
			for term := range terms {
				if isContentTerm(term) {
					requestContent[term] = true
					if rare(term) {
						rareRequest[term] = true
					}
				}
			}
			requestTerms, rareRequestTerms := contentClusters(query, requestContent), contentClusters(query, rareRequest)
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
			matchedContent, rareMatched := contentClusters(query, matchedContentSet), contentClusters(query, rareMatchedSet)
			// 「監査して」のような 1〜2 語の依頼は、仮名を含まない語(監査)が記事の題に合えば届く。適用条件だけの一致(説明、表示)では届けない
			titleRare := map[string]bool{}
			for term := range rareMatchedSet {
				if d.fields[0][term] {
					titleRare[term] = true
				}
			}
			strongTitle := requestTerms <= 2 && hasKanaFreeCluster(query, titleRare)
			coverage, minMatched = 0, 2
			switch {
			case strongTitle, strong && (requestTerms <= 3 || (rareRequestTerms > 0 && float64(rareMatched)/float64(rareRequestTerms) >= .35)):
				// 珍しい識別子 1 語で届くのは、依頼の内容語が少ない時か、依頼側の一致率が足りる時だけ。
				// 長い依頼の中の hook のような 1 語だけで関係ない記事が届いた(2026-10-23 の実測)
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
