package notebook

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// KnowledgePassage is the host agent's sourced interpretation of an exact
// article passage. It does not establish truth or current authorization.
type KnowledgePassage struct {
	Role       string `json:"role" yaml:"role"`
	Quote      string `json:"quote" yaml:"quote"`
	Conditions string `json:"conditions" yaml:"conditions"`
	Reason     string `json:"reason" yaml:"reason"`
}

func validatePassages(body string, passages []KnowledgePassage) error {
	if len(passages) > 8 {
		return errors.New("an article accepts at most eight interpreted passages")
	}
	ranges := [][2]int{}
	for _, p := range passages {
		if !slices.Contains([]string{"current", "history", "recheck", "counterexample"}, p.Role) {
			return errors.New("passage role must be current, history, recheck or counterexample")
		}
		for i, text := range []string{p.Quote, p.Conditions, p.Reason} {
			limit := 400
			if i == 0 {
				limit = 600
			}
			if strings.TrimSpace(text) == "" || len(text) > limit || !utf8.ValidString(text) {
				return errors.New("passages need a UTF-8 quote (1-600 bytes), conditions and reason (1-400 bytes each)")
			}
		}
		start := strings.Index(body, p.Quote)
		if start < 0 || start != strings.LastIndex(body, p.Quote) {
			return errors.New("each interpreted quote must occur exactly once in the article body; update passages with changed text")
		}
		end := start + len(p.Quote)
		for _, span := range ranges {
			if start < span[1] && span[0] < end {
				return errors.New("interpreted passages must not overlap or assign competing roles to the same text")
			}
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return nil
}

type InterpretedExcerpt struct {
	TextExcerpt
	Conditions string `json:"conditions"`
	Reason     string `json:"reason"`
}

// KnowledgeOrientation keeps the current summary separate from interpreted
// chronology. Unclassified text and changed premises remain explicit.
type KnowledgeOrientation struct {
	ID                  string               `json:"id"`
	Revision            string               `json:"revision"`
	Summary             string               `json:"summary"`
	Conditions          string               `json:"conditions"`
	Current             []InterpretedExcerpt `json:"current"`
	History             []InterpretedExcerpt `json:"history"`
	Recheck             []InterpretedExcerpt `json:"recheck"`
	Counterexamples     []InterpretedExcerpt `json:"counterexamples"`
	Unclassified        []TextExcerpt        `json:"unclassified"`
	NeedsInterpretation bool                 `json:"needs_interpretation"`
	PremiseChanged      bool                 `json:"premise_changed"`
	Omitted             bool                 `json:"omitted"`
}

// Orient prioritizes the agent's explicit roles, never guessing validity from
// dates or heading words. All supplied text stays within the excerpt budget.
func Orient(p Page, context string, budget int) KnowledgeOrientation {
	v := KnowledgeOrientation{ID: p.ID, Revision: p.Revision, Summary: p.Lesson, Conditions: p.AppliesTo,
		Current: []InterpretedExcerpt{}, History: []InterpretedExcerpt{}, Recheck: []InterpretedExcerpt{}, Counterexamples: []InterpretedExcerpt{}, Unclassified: []TextExcerpt{}, PremiseChanged: len(p.Recheck) > 0}
	covered := make([]bool, len(p.Body))
	// Classification belongs to the saved revision, even when its quotation
	// does not fit this delivery. Omitted known text must stay classified.
	for _, part := range p.Passages {
		if start := strings.Index(p.Body, part.Quote); start >= 0 {
			for i := start; i < start+len(part.Quote); i++ {
				covered[i] = true
			}
		}
	}
	for _, role := range []string{"current", "recheck", "counterexample", "history"} {
		for _, part := range p.Passages {
			if part.Role != role {
				continue
			}
			start := strings.Index(p.Body, part.Quote)
			if start < 0 || len(part.Quote) > budget {
				v.Omitted = true
				continue
			}
			end := start + len(part.Quote)
			e := InterpretedExcerpt{TextExcerpt: TextExcerpt{Start: start, End: end, Text: part.Quote}, Conditions: part.Conditions, Reason: part.Reason}
			switch role {
			case "current":
				v.Current = append(v.Current, e)
			case "history":
				v.History = append(v.History, e)
			case "recheck":
				v.Recheck = append(v.Recheck, e)
			case "counterexample":
				v.Counterexamples = append(v.Counterexamples, e)
			}
			budget -= len(part.Quote)
		}
	}
	for from := 0; from < len(p.Body); {
		if covered[from] {
			from++
			continue
		}
		end := from
		for end < len(p.Body) && !covered[end] {
			end++
		}
		if strings.TrimSpace(p.Body[from:end]) != "" {
			v.NeedsInterpretation = true
		}
		from = end
	}
	// Select whole original units before filtering classified spans. Cutting a
	// unit around a saved phrase would return a severed sentence or join text
	// that was never adjacent. Keep that unit for exact full-text recovery.
	for _, x := range FocusExcerpts(p.Body, context, max(0, budget)) {
		overlaps := false
		for i := x.Start; i < x.End; i++ {
			overlaps = overlaps || covered[i]
		}
		if overlaps {
			continue
		}
		v.Unclassified = append(v.Unclassified, x)
	}
	v.NeedsInterpretation = v.NeedsInterpretation || len(v.Current) == 0 || v.Omitted
	return v
}

func (v KnowledgeOrientation) Excerpts() []TextExcerpt {
	out := append([]TextExcerpt{}, v.Unclassified...)
	for _, group := range [][]InterpretedExcerpt{v.Current, v.History, v.Recheck, v.Counterexamples} {
		for _, e := range group {
			out = append(out, e.TextExcerpt)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
