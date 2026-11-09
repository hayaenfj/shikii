package notebook

import (
	"sort"
	"strings"
	"unicode"
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
	if budget <= 0 {
		return []TextExcerpt{}
	}
	type passage struct {
		TextExcerpt
		score int
	}
	terms := searchTerms(context)
	passages := []passage{}
	// Sentence and line boundaries keep exact quotes understandable. A unit
	// too large for the budget is omitted, with full recovery left to the caller.
	for _, unit := range excerptUnits(text) {
		start, end := unit[0], unit[1]
		for start < end && (text[start] == ' ' || text[start] == '\n' || text[start] == '\t') {
			start++
		}
		for end > start && (text[end-1] == ' ' || text[end-1] == '\n' || text[end-1] == '\t') {
			end--
		}
		chunk := text[start:end]
		if chunk == "" || strings.HasPrefix(strings.TrimSpace(chunk), "#") {
			continue
		}
		score := 0
		for term := range searchTerms(chunk) {
			if terms[term] && isContentTerm(term) {
				score++
			}
		}
		passages = append(passages, passage{TextExcerpt{start, end, chunk}, score})
	}
	sort.SliceStable(passages, func(i, j int) bool { return passages[i].score > passages[j].score })
	out := []TextExcerpt{}
	for _, p := range passages {
		if budget == 0 || len(out) == 3 {
			break
		}
		if len(p.Text) > budget {
			continue
		}
		out = append(out, p.TextExcerpt)
		budget -= len(p.Text)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// Keep up to 600 bytes of whole sentences per candidate. Newlines also bound
// list items and code; headings never become a quote by themselves.
func excerptUnits(text string) [][2]int {
	units := [][2]int{}
	start, part := 0, 0
	flush := func(end int) {
		if end-start > 600 && part > start {
			units = append(units, [2]int{start, part})
			start = part
		}
		part = end
		if end-start >= 600 || end == len(text) {
			units = append(units, [2]int{start, end})
			start = end
			part = end
		}
	}
	for i, r := range text {
		end := i + utf8.RuneLen(r)
		boundary := r == '\n' || r == '\u3002' || r == '\uff01' || r == '\uff1f'
		if r == '.' || r == '!' || r == '?' {
			next, _ := utf8.DecodeRuneInString(text[end:])
			boundary = end == len(text) || unicode.IsSpace(next)
		}
		if boundary {
			flush(end)
			if r == '\n' {
				if part > start {
					units = append(units, [2]int{start, part})
					start = part
				}
			}
		}
	}
	if start < len(text) {
		flush(len(text))
	}
	return units
}
