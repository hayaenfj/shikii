package notebook

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Patch changes only named text spans. A revision check protects every field,
// including edits another caller made outside those spans.
type Patch struct {
	ID           string              `json:"id"`
	Revision     string              `json:"revision"`
	Sources      []string            `json:"sources"`
	ChangeReason string              `json:"change_reason"`
	Edits        []TextEdit          `json:"edits"`
	Passages     *[]KnowledgePassage `json:"passages,omitempty"`
	Learning     *Learning           `json:"learning,omitempty"`
	Related      *[]string           `json:"related,omitempty"`
	Requires     []ArticleReference  `json:"requires,omitempty"`
}

type TextEdit struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func (b Book) Patch(c Patch) (Result, error)        { return b.patch(c, false) }
func (b Book) PreviewPatch(c Patch) (Result, error) { return b.patch(c, true) }

func (b Book) patch(c Patch, preview bool) (Result, error) {
	if !idPattern.MatchString(c.ID) || !idPattern.MatchString(c.Revision) {
		return Result{}, errors.New("patch needs an article id and revision")
	}
	if strings.TrimSpace(c.ChangeReason) == "" || len(c.Sources) == 0 || len(c.Edits) == 0 && c.Passages == nil && c.Learning == nil && c.Related == nil || len(c.Edits) > 16 {
		return Result{}, errors.New("patch needs sources, a change_reason and 1 to 16 edits or interpreted passages")
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		return Result{}, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	pages, issues, err := b.pages()
	if err != nil {
		return Result{}, err
	}
	if len(issues) > 0 {
		return Result{}, errors.New("wiki has unreadable or changed pages; inspect wiki lint before patching")
	}
	var old Page
	for _, p := range pages {
		if p.ID == c.ID {
			old = p
		}
		for _, id := range p.Replaces {
			if id == c.ID {
				return Result{}, ErrArticleConflict
			}
		}
	}
	if len(c.Requires) > 8 {
		return Result{}, errors.New("a patch accepts at most eight required article revisions")
	}
	for _, required := range c.Requires {
		found := false
		for _, p := range pages {
			if p.ID == required.ID && p.Revision == required.Revision && p.Status != "deleted" && len(b.sourceIssues(p)) == 0 {
				found = true
			}
			for _, id := range p.Replaces {
				if id == required.ID {
					return Result{}, ErrArticleConflict
				}
			}
		}
		if !idPattern.MatchString(required.ID) || !idPattern.MatchString(required.Revision) || !found {
			return Result{}, ErrArticleConflict
		}
	}
	if old.ID == "" {
		return Result{}, ErrArticleMissing
	}
	if old.Revision != c.Revision || old.Status == "deleted" {
		return Result{}, ErrArticleConflict
	}
	next := old
	fields := map[string]*string{"title": &next.Title, "lesson": &next.Lesson, "applies_to": &next.AppliesTo, "body": &next.Body, "rationale": &next.Rationale, "kind": &next.Kind, "scene": &next.Scene}
	for i, e := range c.Edits {
		target, ok := fields[e.Field]
		if !ok || !utf8.ValidString(e.Before) || !utf8.ValidString(e.After) || len(e.Before) > 16<<10 || len(e.After) > 16<<10 {
			return Result{}, fmt.Errorf("invalid patch edit %d", i+1)
		}
		if e.Before == e.After {
			return Result{}, errors.New("a patch edit must change its text")
		}
		if e.Before == "" {
			if *target != "" {
				return Result{}, errors.New("an empty before can only fill an empty field")
			}
			*target = e.After
		} else {
			if strings.Index(*target, e.Before) < 0 || strings.Index(*target, e.Before) != strings.LastIndex(*target, e.Before) {
				return Result{}, fmt.Errorf("patch edit %d must match exactly once in %s", i+1, e.Field)
			}
			*target = strings.Replace(*target, e.Before, e.After, 1)
		}
	}
	// An empty kind or scene is inherited from the saved page by the save below,
	// so an edit that empties one would be reported as saved and not applied.
	if old.Kind != "" && strings.TrimSpace(next.Kind) == "" || old.Scene != "" && strings.TrimSpace(next.Scene) == "" {
		return Result{}, errors.New("patch cannot clear kind or scene; replace it with another value")
	}
	d := Draft{ID: old.ID, Title: next.Title, Lesson: next.Lesson, AppliesTo: next.AppliesTo, Body: &next.Body, Rationale: &next.Rationale, Kind: next.Kind, Scene: next.Scene, Sources: c.Sources, ChangeReason: c.ChangeReason, baseRevision: old.Revision, preview: preview}
	d.Passages = c.Passages
	d.Learning = c.Learning
	if c.Related != nil {
		d.Related = *c.Related
	}
	if strings.TrimSpace(next.Lesson) == "" {
		return Result{}, errors.New("patch cannot clear the lesson")
	}
	result, err := b.ingest(d, true)
	if err != nil && result.committed {
		result.Warnings = []string{"The article was saved at page.revision, but derived index, log or source-review records need recovery. Run wiki lint, reread the article, and submit its sources alone through wiki ingest; do not resend the stale patch."}
		err = nil
	}
	if err == nil && result.Page != nil {
		result.Page.Recheck = b.premiseChanges(*result.Page)
	}
	return result, err
}
