package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// storablePage refuses a page whose file would not read back as the same
// revision. The YAML encoder loses a leading line break of a multi-line text and
// writes a wrong indentation hint for one inside a list, so such a page would be
// saved, reported as a success and then counted as a changed page that blocks
// every later save.
func (b Book) storablePage(p Page) error {
	data, err := b.pageBytes(p)
	if err != nil || len(data) > 64<<10 {
		return errors.New("wiki page exceeds the size limit")
	}
	back, err := parsePage(data)
	if err != nil || back.ID != p.ID || revision(back) != p.Revision || !b.matchesPage(data, back) {
		return errors.New("wiki text cannot be stored exactly: a multi-line change_reason, correction, passage or learning text must not start with a space or a line break; remove the leading whitespace (for a quote, start it at the first visible character) and send it again")
	}
	return nil
}

func (b Book) buildIngestPage(d Draft, body, sourceDir string, pages []Page, refs []Source, raw map[string][]byte) (*Page, error) {
	var err error
	byID := map[string]Page{}
	for _, p := range pages {
		byID[p.ID] = p
		for _, ref := range p.Sources {
			if data, ok := raw[ref.ID]; ok && digest(data) != ref.Hash {
				return nil, errors.New("a wiki source differs from its existing citation")
			}
		}
	}
	if d.Lesson != "" {
		id := d.ID
		if id == "" {
			id = digest([]byte(strings.TrimSpace(d.Title)))[:16]
			// The id comes from the first title. After a rename it belongs to an
			// article with another title; a new lesson must not overwrite that one.
			if old, ok := byID[id]; ok && old.Status != "deleted" && old.Title != d.Title {
				return nil, fmt.Errorf("%w: the title %q was the first title of article %s, now titled %q; update that article by its id, or give the new lesson another title", ErrArticleConflict, d.Title, id, old.Title)
			}
		} else if _, ok := byID[id]; !ok {
			return nil, errors.New("the lesson to update does not exist in this project")
		}
		// A replaced article is never live again. Patch and Manage refuse it; a
		// lesson saved into it would be reported as saved and never be delivered.
		if _, ok := byID[id]; ok {
			for _, other := range pages {
				for _, replaced := range other.Replaces {
					if replaced == id {
						return nil, fmt.Errorf("%w: article %s was replaced by %s; update that article, or give the new lesson another title", ErrArticleConflict, id, other.ID)
					}
				}
			}
		}
		if d.CreateOnly && byID[id].ID != "" {
			return nil, ErrArticleConflict
		}
		if byID[id].Status == "deleted" {
			return nil, ErrArticleConflict
		}
		if _, exists := byID[id]; !exists {
			for _, old := range pages {
				if old.Status == "deleted" && old.Title == d.Title {
					return nil, ErrArticleConflict
				}
			}
		}
		for _, replaced := range d.Replaces {
			if _, ok := byID[replaced]; !ok || replaced == id {
				return nil, errors.New("a replaced lesson must be another page in this project")
			}
		}
		p := Page{ID: id, Title: d.Title, Lesson: d.Lesson, AppliesTo: d.AppliesTo, Status: "hypothesis", Sources: refs, Replaces: d.Replaces, Correction: d.Correction, Body: body, Related: d.Related, Kind: d.Kind, Scene: d.Scene}
		p.Rationale, p.Premises = byID[id].Rationale, byID[id].Premises
		p.Learning = byID[id].Learning
		if d.Learning != nil {
			p.Learning = d.Learning
			if learningEmpty(p.Learning) {
				p.Learning = nil
			}
		}
		if d.Rationale != nil {
			p.Rationale = strings.TrimSpace(*d.Rationale)
		}
		if d.preparedCreate != nil {
			p.Premises = append([]Premise(nil), d.preparedCreate.Premises...)
		} else if d.Watch != nil {
			p.Premises, err = b.snapshotPremises(*d.Watch)
			if err != nil {
				return nil, err
			}
		}
		if d.baseRevision != "" && byID[id].Revision != d.baseRevision {
			return nil, errors.New("the page changed while classifying; read it again and classify it again")
		}
		if old, ok := byID[id]; ok { // inherited if omitted when updating (pages from the previous version have none, so they cannot be saved without them)
			if p.Kind == "" {
				p.Kind = old.Kind
			}
			if p.Scene == "" {
				p.Scene = old.Scene
			}
		}
		if !validKindScene(p.Kind, p.Scene) && !(d.change != nil && d.change.Action == "delete") {
			return nil, kindSceneError(p.Kind, p.Scene)
		}
		p.ChangeReason = d.ChangeReason
		if old, ok := byID[id]; ok {
			if d.ChangeReason == "" {
				p.ChangeReason = old.ChangeReason
			}
			if bad := b.sourceIssues(old); len(bad) > 0 {
				return nil, errors.New("the existing lesson has missing or changed sources")
			}
			for _, ref := range old.Sources {
				found := false
				for _, r := range p.Sources {
					if ref.ID == r.ID {
						if ref.Hash != r.Hash || (ref.TextHash != "" && ref.TextHash != r.TextHash) {
							return nil, errors.New("an existing lesson's source hash changed")
						}
						found = true
					}
				}
				if !found && !b.recaptured(sourceDir, ref, raw) {
					p.Sources = append(p.Sources, ref)
				}
			}
			if d.Replaces == nil {
				p.Replaces = append([]string{}, old.Replaces...)
			}
			if d.Body == nil {
				p.Body = old.Body
			}
			p.Passages = append([]KnowledgePassage(nil), old.Passages...)
			if d.Related == nil {
				p.Related = old.Related
			}
			p.Previous = old.Previous
			if p.Correction == nil {
				p.Correction = old.Correction
			}
		}
		if d.Passages != nil {
			p.Passages = append([]KnowledgePassage(nil), (*d.Passages)...)
		}
		if err := b.validateLearningEvidence(&p, byID[id], byID); err != nil {
			return nil, err
		}
		if err := validatePassages(p.Body, p.Passages); err != nil {
			return nil, err
		}
		sort.Slice(p.Sources, func(i, j int) bool { return p.Sources[i].ID < p.Sources[j].ID })
		p.Replaces = unique(p.Replaces)
		p.Related = unique(p.Related)
		for _, related := range p.Related {
			linked, ok := byID[related]
			if !ok || related == id || len(b.sourceIssues(linked)) > 0 {
				return nil, errors.New("a related page must be another page with valid sources in this project")
			}
		}
		if old, ok := byID[id]; ok && revision(p) != old.Revision {
			// Even an update that restores old content becomes a new revision with a link to the previous one.
			p.Previous = old.Revision
		}
		p.Revision = revision(p)
		if d.preparedCreate != nil && (p.ID != d.preparedCreate.ID || p.Revision != d.preparedCreate.Revision) {
			return nil, errors.New("the prepared article changed before publication; prepare its exact revision again")
		}
		if err := b.storablePage(p); err != nil {
			return nil, err
		}
		byID[id] = p
		if cyclic(byID) {
			return nil, errors.New("replaced wiki lessons must not form a cycle")
		}
		if d.change != nil {
			if d.change.Action == "delete" {
				p.Status = "deleted"
			}
			observation := changeObservation(*d.change)
			data, _ := json.Marshal(observation)
			observation.ID = digest(data)[:16]
			data, _ = json.Marshal(observation)
			raw[observation.ID] = data
			p.Sources = append(p.Sources, Source{ID: observation.ID, Hash: digest(data)})
			sort.Slice(p.Sources, func(i, j int) bool { return p.Sources[i].ID < p.Sources[j].ID })
			// The previous article is the version checked by Manage under this lock.
			p.Previous = d.baseRevision
			p.Revision = revision(p)
			if err := b.storablePage(p); err != nil {
				return nil, err
			}
			observation.ID = ""
			if _, err := b.capture(observation); err != nil {
				return nil, err
			}
		}
		return &p, nil
	}
	return nil, nil
}
