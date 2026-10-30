package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrArticleConflict = errors.New("the article changed or is no longer active; read it again before saving")
var ErrArticleMissing = errors.New("the article does not exist in this project")

// Change contains only the fields the Knowledge page can change. Evidence,
// relationships and history are always taken from the current saved article.
type Change struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Action    string `json:"action"`
	Title     string `json:"title,omitempty"`
	Lesson    string `json:"lesson,omitempty"`
	AppliesTo string `json:"applies_to,omitempty"`
	Body      string `json:"body,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Scene     string `json:"scene,omitempty"`
}

func (b Book) Manage(c Change) (Page, error) {
	if !idPattern.MatchString(c.ID) || !idPattern.MatchString(c.Revision) {
		return Page{}, errors.New("article id and revision must be 16 lowercase hexadecimal characters")
	}
	if c.Action != "edit" && c.Action != "delete" {
		return Page{}, errors.New("article action must be edit or delete")
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
	if old.Revision != c.Revision || old.Status == "deleted" {
		return Page{}, ErrArticleConflict
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
	}
	d.ChangeReason = fmt.Sprintf("%s from the Knowledge page", c.Action)
	result, err := b.ingest(d, true)
	if err != nil {
		return Page{}, err
	}
	return *result.Page, nil
}

// A submitted change is a source snapshot, not an authenticated identity or a
// calibration truth. Capture it only after validation, while holding the page lock.
func changeObservation(c Change) Observation {
	data, _ := json.Marshal(c)
	return Observation{Kind: "human", Title: "Knowledge page " + c.Action, Content: string(data), Reference: "lesson:" + c.ID + "@" + c.Revision, Version: c.Revision, ObservedAt: time.Now().UTC()}
}
