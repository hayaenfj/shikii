package notebook

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// TextExcerpt is an exact byte range of the cited revision, never a new summary.
type TextExcerpt struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// FocusExcerpts chooses up to three exact passages within a fixed byte budget.
// Matching selects what to read, not the truth or semantic role of a passage.
func FocusExcerpts(text, context string, budget int) []TextExcerpt {
	type passage struct {
		TextExcerpt
		score int
	}
	terms := searchTerms(context)
	passages := []passage{}
	for from := 0; from < len(text); {
		end := len(text)
		if i := strings.Index(text[from:], "\n\n"); i >= 0 {
			end = from + i + 2
		}
		for start := from; start < end; {
			to := min(start+600, end)
			for to > start && !utf8.ValidString(text[start:to]) {
				to--
			}
			if to == start {
				break
			}
			chunk := text[start:to]
			score := 0
			for term := range searchTerms(chunk) {
				if terms[term] && isContentTerm(term) {
					score++
				}
			}
			if strings.TrimSpace(chunk) != "" {
				passages = append(passages, passage{TextExcerpt{Start: start, End: to, Text: chunk}, score})
			}
			start = to
		}
		from = end
	}
	sort.SliceStable(passages, func(i, j int) bool { return passages[i].score > passages[j].score })
	out := []TextExcerpt{}
	for _, p := range passages {
		if budget == 0 || len(out) == 3 {
			break
		}
		end := min(p.End, p.Start+budget)
		for end > p.Start && !utf8.ValidString(text[p.Start:end]) {
			end--
		}
		if end == p.Start {
			continue
		}
		out = append(out, TextExcerpt{Start: p.Start, End: end, Text: text[p.Start:end]})
		budget -= end - p.Start
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
