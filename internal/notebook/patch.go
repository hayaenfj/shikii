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

// patchEditFields are the article fields an edit can name, in the order the refusals list them.
const patchEditFields = "title, lesson, applies_to, body, rationale, kind, scene"

// patchEditLimit is the most bytes of one before or after.
const patchEditLimit = 16 << 10

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
			if p.ID == required.ID && p.Revision == required.Revision && !p.Gone() && len(b.sourceIssues(p)) == 0 {
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
	if old.Revision != c.Revision || old.Gone() {
		return Result{}, ErrArticleConflict
	}
	next := old
	fields := map[string]*string{"title": &next.Title, "lesson": &next.Lesson, "applies_to": &next.AppliesTo, "body": &next.Body, "rationale": &next.Rationale, "kind": &next.Kind, "scene": &next.Scene}
	for i, e := range c.Edits {
		target, ok := fields[e.Field]
		if err := patchEditProblem("patch", i+1, e, ok); err != nil {
			return Result{}, err
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
			if n := occurrences(*target, e.Before); n != 1 {
				return Result{}, patchMatchProblem(old.ID, i+1, e.Field, n)
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

// patchEditProblem says why edit number n of a patch or a reconciliation (what) cannot be applied before its text is looked for: the field is missing or is not an article
// field (known is false), or before or after is not UTF-8 or is too long. It returns nil for an edit that can be looked at.
func patchEditProblem(what string, n int, e TextEdit, known bool) error {
	switch {
	case !known && e.Field == "":
		return fmt.Errorf("%s edit %d has no field; each edit is {field,before,after} and field is one of %s", what, n, patchEditFields)
	case !known:
		return fmt.Errorf("%s edit %d has field %.40q, which is not an article field; field is one of %s", what, n, e.Field, patchEditFields)
	}
	for _, side := range []struct{ name, text string }{{"before", e.Before}, {"after", e.After}} {
		switch {
		case !utf8.ValidString(side.text):
			return fmt.Errorf("%s edit %d: %s is not valid UTF-8", what, n, side.name)
		case len(side.text) > patchEditLimit:
			return fmt.Errorf("%s edit %d: %s is %d bytes, over the limit of %d", what, n, side.name, len(side.text), patchEditLimit)
		}
	}
	return nil
}

// occurrences counts where sub is in s, overlapping places included ("aa" is twice in "aaa"), because an edit whose before overlaps itself
// cannot be put on one place either.
func occurrences(s, sub string) int {
	n := 0
	for at := 0; ; n++ {
		i := strings.Index(s[at:], sub)
		if i < 0 {
			return n
		}
		at += i + 1
	}
}

// patchMatchProblem says that before of edit number n of article id is not in the field exactly once, with how many times it is, and what to do. The
// edits are applied one after the other, so from the second on the text looked in is the one the earlier edits left.
func patchMatchProblem(id string, n int, field string, times int) error {
	after := ""
	if n > 1 {
		after = " (after the earlier edits)"
	}
	if times == 0 {
		return fmt.Errorf("patch edit %d: before matches 0 times in %s%s; read the article again with shikii wiki query lesson:%s and use text that its %s has", n, field, after, id, field)
	}
	return fmt.Errorf("patch edit %d: before matches %d times in %s%s; add words around it until it matches once", n, times, field, after)
}
