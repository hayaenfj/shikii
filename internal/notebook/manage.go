package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrArticleConflict = errors.New("the article changed or is no longer active; read it again before saving")
var ErrArticleMissing = errors.New("the article does not exist in this project")

// Change contains only the fields the Knowledge page can change (edit, delete, and restore for an article a work withdrew). Evidence,
// relationships and history are always taken from the current saved article.
type Change struct {
	ID        string              `json:"id"`
	Revision  string              `json:"revision"`
	Action    string              `json:"action"`
	Title     string              `json:"title,omitempty"`
	Lesson    string              `json:"lesson,omitempty"`
	AppliesTo string              `json:"applies_to,omitempty"`
	Body      string              `json:"body,omitempty"`
	Rationale *string             `json:"rationale,omitempty"`
	Kind      string              `json:"kind,omitempty"`
	Scene     string              `json:"scene,omitempty"`
	Passages  *[]KnowledgePassage `json:"passages,omitempty"`
	// A withdrawal (Withdraw) deletes with its own check and source; no JSON a page or an agent sends can set them.
	withdrawal *Withdrawal
	check      func(Page) error
}

func (b Book) Manage(c Change) (Page, error) {
	if !idPattern.MatchString(c.ID) || !idPattern.MatchString(c.Revision) {
		return Page{}, errors.New("article id and revision must be 16 lowercase hexadecimal characters")
	}
	if c.Action != "edit" && c.Action != "delete" && c.Action != "restore" && c.Action != "withdraw" {
		return Page{}, errors.New("article action must be edit, delete or restore")
	}
	// Only Withdraw (the program that made the article, from the command line) takes an article back, and it always carries its own
	// withdrawal; a change a page or an agent sends cannot ask for it by name.
	if (c.Action == "withdraw") != (c.withdrawal != nil) {
		return Page{}, errors.New("article action must be edit, delete or restore")
	}
	dir, err := b.directory(true, "pages")
	if err != nil {
		return Page{}, err
	}
	unlock, err := lock(dir)
	if err != nil {
		return Page{}, err
	}
	defer unlock()
	pages, issues, err := b.pages()
	if err != nil {
		return Page{}, err
	}
	if len(issues) > 0 {
		return Page{}, errors.New("wiki has unreadable or changed pages; inspect wiki lint before saving")
	}
	var old Page
	for _, p := range pages {
		if p.ID == c.ID {
			old = p
		}
		for _, id := range p.Replaces {
			if id == c.ID {
				return Page{}, ErrArticleConflict
			}
		}
	}
	if old.ID == "" {
		return Page{}, ErrArticleMissing
	}
	if old.Revision != c.Revision {
		return Page{}, ErrArticleConflict
	}
	switch c.Action {
	case "restore": // the person takes a withdrawn article back: only a withdrawn one has anything to restore
		if old.Status != StatusWithdrawn {
			return Page{}, ErrArticleConflict
		}
	case "delete": // a withdrawn article is deleted by the person like a live one; a deleted one is gone
		if old.Status == "deleted" {
			return Page{}, ErrArticleConflict
		}
	default: // edit and withdraw take a live article only
		if old.Gone() {
			return Page{}, ErrArticleConflict
		}
	}
	if c.check != nil {
		if err := c.check(old); err != nil {
			return Page{}, err
		}
	}
	d := Draft{ID: old.ID, Title: old.Title, Lesson: old.Lesson, AppliesTo: old.AppliesTo, Kind: old.Kind, Scene: old.Scene, baseRevision: old.Revision, change: &c}
	for _, src := range old.Sources {
		d.Sources = append(d.Sources, src.ID)
	}
	if c.Action == "edit" {
		if c.Lesson == "" || !validKindScene(c.Kind, c.Scene) {
			return Page{}, errors.New("an edit needs a summary, kind and scene")
		}
		d.Title, d.Lesson, d.AppliesTo, d.Body, d.Kind, d.Scene = c.Title, c.Lesson, c.AppliesTo, &c.Body, c.Kind, c.Scene
		d.Rationale = c.Rationale
		d.Passages = c.Passages
	}
	d.ChangeReason = fmt.Sprintf("%s from the Knowledge page", c.Action)
	if c.withdrawal != nil {
		d.ChangeReason = "withdrawn by the work that made it: " + c.withdrawal.Reason
	}
	result, err := b.ingest(d, true)
	if err != nil {
		return Page{}, err
	}
	result.Page.Recheck = b.premiseChanges(*result.Page)
	return *result.Page, nil
}

// A submitted change is a source snapshot, not an authenticated identity or a
// calibration truth. Capture it only after validation, while holding the page lock.
func changeObservation(c Change) Observation {
	if c.withdrawal != nil {
		return withdrawalObservation(*c.withdrawal)
	}
	data, _ := json.Marshal(c)
	return Observation{Kind: "human", Title: "Knowledge page " + c.Action, Content: string(data), Reference: "lesson:" + c.ID + "@" + c.Revision, Version: c.Revision, ObservedAt: time.Now().UTC()}
}
