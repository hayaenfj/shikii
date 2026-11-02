package notebook

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type OrganizationSource struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Lesson   string `json:"lesson"`
}

type KnowledgeMove struct {
	From  string `json:"from"`
	Quote string `json:"quote"`
	To    string `json:"to"` // the exact title of a new target article
}

type Organization struct {
	Reason  string               `json:"reason"`
	Sources []OrganizationSource `json:"sources"`
	Targets []Draft              `json:"targets"`
	Moves   []KnowledgeMove      `json:"moves"`
}

type OrganizedCreate struct {
	Draft Draft `json:"draft"`
	Page  Page  `json:"page"`
}

type OrganizationPlan struct {
	Creates    []OrganizedCreate `json:"creates"`
	Patches    []Patch           `json:"patches"`
	NextAction string            `json:"next_action"`
	Notice     string            `json:"notice"`
}

// Organize validates the entire move before publishing anything. Destination
// drafts come first; source patches require their exact published revisions.
// A partial execution therefore retains each moved quotation at its source or
// at a checked destination. Existing inbound links can follow the source's
// related articles; history and immutable source records are never removed.
func (b Book) Organize(o Organization) (OrganizationPlan, error) {
	out := OrganizationPlan{Creates: []OrganizedCreate{}, Patches: []Patch{},
		NextAction: "Read all proposed articles and classifications. Publish creates with wiki ingest, keeping create_only, and check each returned revision against creates.page. Then apply the patches in order with wiki patch, retaining requires. On a conflict or failure, read the actual saved revisions and replan; do not overwrite a concurrent edit or delete an original first.",
		Notice:     "This is an unsaved organization plan, not a semantic equivalence judgment or permission. Text not selected for a move stays in the original. Source records and old revisions stay available."}
	if !learningText(o.Reason, 800) || len(o.Sources) < 1 || len(o.Sources) > 4 || len(o.Targets) < 1 || len(o.Targets) > 4 || len(o.Moves) < 1 || len(o.Moves) > 8 {
		return out, errors.New("organize needs a reason, one to four pinned source articles, one to four new targets and one to eight exact moves")
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		return out, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return out, err
	}
	defer unlock()
	live, err := b.Live()
	if err != nil {
		return out, err
	}
	byID := map[string]Page{}
	for _, p := range live {
		byID[p.ID] = p
	}
	sources := map[string]Page{}
	for _, s := range o.Sources {
		p, ok := byID[s.ID]
		if !idPattern.MatchString(s.ID) || !idPattern.MatchString(s.Revision) || !ok || p.Revision != s.Revision {
			return out, ErrArticleConflict
		}
		if !learningText(s.Lesson, 2000) || sources[s.ID].ID != "" {
			return out, errors.New("each organization source needs a distinct article and its revised retrieval summary")
		}
		sources[s.ID] = p
	}
	targets, titles := append([]Draft(nil), o.Targets...), map[string]int{}
	for i, d := range targets {
		if d.ID != "" || d.Title != strings.TrimSpace(d.Title) || d.Title == "" || d.Body == nil || len(d.Replaces) > 0 || d.Correction != nil || d.Watch != nil {
			return out, errors.New("organization targets are new articles with a title and body; replacements, corrections and watched-file changes remain separate operations")
		}
		if _, ok := titles[d.Title]; ok {
			return out, errors.New("organization target titles must be unique")
		}
		titles[d.Title] = i
		targets[i].CreateOnly = true
		targets[i].ChangeReason = o.Reason
	}
	type span struct {
		start, end int
		target     int
	}
	spans := map[string][]span{}
	targetUsed := map[int]bool{}
	for _, m := range o.Moves {
		p, ok := sources[m.From]
		i, targetOK := titles[m.To]
		if !ok || !targetOK || !learningText(m.Quote, 16<<10) || !uniqueLearningQuote(p.Body, m.Quote) || !uniqueLearningQuote(*targets[i].Body, m.Quote) {
			return out, errors.New("a move must quote one unique exact body passage in its pinned source and named destination")
		}
		start := strings.Index(p.Body, m.Quote)
		for _, prior := range spans[p.ID] {
			if start < prior.end && start+len(m.Quote) > prior.start {
				return out, errors.New("organization moves must not overlap")
			}
		}
		spans[p.ID] = append(spans[p.ID], span{start, start + len(m.Quote), i})
		targetUsed[i] = true
		for _, s := range p.Sources {
			targets[i].Sources = append(targets[i].Sources, s.ID)
		}
		targets[i].Related = append(targets[i].Related, p.ID)
		// A role crossing the selected boundary cannot be silently weakened or
		// lost. Move the whole quotation or choose a different source boundary.
		for _, role := range p.Passages {
			at := strings.Index(p.Body, role.Quote)
			if at < start+len(m.Quote) && at+len(role.Quote) > start {
				if at < start || at+len(role.Quote) > start+len(m.Quote) {
					return out, errors.New("a move cuts an interpreted quotation; include the complete quotation")
				}
				if targets[i].Passages == nil {
					empty := []KnowledgePassage{}
					targets[i].Passages = &empty
				} else {
					copy := append([]KnowledgePassage(nil), (*targets[i].Passages)...)
					targets[i].Passages = &copy
				}
				found := false
				for _, existing := range *targets[i].Passages {
					found = found || existing == role
				}
				if !found {
					*targets[i].Passages = append(*targets[i].Passages, role)
				}
			}
		}
	}
	for i := range targets {
		if !targetUsed[i] {
			return out, errors.New("every target must receive at least one source quotation")
		}
		targets[i].Sources = unique(targets[i].Sources)
		targets[i].Related = unique(targets[i].Related)
		targets[i].preview = true
		preview, err := b.ingest(targets[i], true)
		if err != nil {
			return out, err
		}
		for _, move := range o.Moves {
			if titles[move.To] == i && !uniqueLearningQuote(preview.Page.Body, move.Quote) {
				return out, errors.New("the normalized destination must retain each exact moved quotation; add surrounding context when whitespace is significant")
			}
		}
		targets[i].preview = false
		out.Creates = append(out.Creates, OrganizedCreate{Draft: targets[i], Page: *preview.Page})
	}
	for _, s := range o.Sources {
		p := sources[s.ID]
		if len(spans[p.ID]) == 0 {
			return out, errors.New("every source must contribute at least one move")
		}
		related := append([]string(nil), p.Related...)
		requires := []ArticleReference{}
		ranges := append([]span(nil), spans[p.ID]...)
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
		var assembled strings.Builder
		position := 0
		for _, moved := range ranges {
			target := out.Creates[moved.target].Page
			assembled.WriteString(p.Body[position:moved.start])
			assembled.WriteString("See related article: lesson:" + target.ID)
			position = moved.end
			related = append(related, target.ID)
			ref := ArticleReference{ID: target.ID, Revision: target.Revision}
			seen := false
			for _, r := range requires {
				seen = seen || r == ref
			}
			if !seen {
				requires = append(requires, ref)
			}
		}
		assembled.WriteString(p.Body[position:])
		body := assembled.String()
		kept := []KnowledgePassage{}
		for _, role := range p.Passages {
			at, moved := strings.Index(p.Body, role.Quote), false
			for _, r := range ranges {
				moved = moved || at >= r.start && at+len(role.Quote) <= r.end
			}
			if !moved {
				kept = append(kept, role)
			}
		}
		if len(body) > 16<<10 {
			return out, errors.New("the remaining body exceeds the article limit; choose a larger exact move")
		}
		if err := validatePassages(body, kept); err != nil {
			return out, err
		}
		sourceIDs := []string{}
		for _, r := range p.Sources {
			sourceIDs = append(sourceIDs, r.ID)
		}
		related = unique(related)
		edits := []TextEdit{{Field: "body", Before: p.Body, After: body}}
		if s.Lesson != p.Lesson {
			edits = append(edits, TextEdit{Field: "lesson", Before: p.Lesson, After: s.Lesson})
		}
		out.Patches = append(out.Patches, Patch{ID: p.ID, Revision: p.Revision, Sources: sourceIDs, ChangeReason: o.Reason, Edits: edits, Passages: &kept, Related: &related, Requires: requires})
	}
	// Bound the returned plan before serializing it through any transport.
	if len(out.Creates) == 0 || len(out.Patches) == 0 {
		return out, fmt.Errorf("no source-preserving organization is available")
	}
	return out, nil
}
