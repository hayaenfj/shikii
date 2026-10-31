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
	ID           string     `json:"id"`
	Revision     string     `json:"revision"`
	Sources      []string   `json:"sources"`
	ChangeReason string     `json:"change_reason"`
	Edits        []TextEdit `json:"edits"`
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
	if strings.TrimSpace(c.ChangeReason) == "" || len(c.Sources) == 0 || len(c.Edits) < 1 || len(c.Edits) > 16 {
		return Result{}, errors.New("patch needs sources, a change_reason and 1 to 16 edits")
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
			if strings.Count(*target, e.Before) != 1 {
				return Result{}, fmt.Errorf("patch edit %d must match exactly once in %s", i+1, e.Field)
			}
			*target = strings.Replace(*target, e.Before, e.After, 1)
		}
	}
	d := Draft{ID: old.ID, Title: next.Title, Lesson: next.Lesson, AppliesTo: next.AppliesTo, Body: &next.Body, Rationale: &next.Rationale, Kind: next.Kind, Scene: next.Scene, Sources: c.Sources, ChangeReason: c.ChangeReason, baseRevision: old.Revision, preview: preview}
	if strings.TrimSpace(next.Lesson) == "" {
		return Result{}, errors.New("patch cannot clear the lesson")
	}
	result, err := b.ingest(d, true)
	if err == nil && result.Page != nil {
		result.Page.Recheck = b.premiseChanges(*result.Page)
	}
	return result, err
}
