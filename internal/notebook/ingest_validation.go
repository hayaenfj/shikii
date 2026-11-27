package notebook

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// idSaveRefusal is the refusal of a save that lacks what an update needs. A save with the id of an article is told the patch first, as a
// refusal of a body save is, and then what is left to send (otherwise): an agent told only what was missing filled the fields in and sent the
// whole article again, which loses a part of the body when it is copied wrong (Claude Code and Codex both did, given a sentence to add). The
// refusal has not read the article, so the patch names the revision the query it asks for shows, and it does not say that the id is an
// article's (an id of the right form may name no article: 0.44.2 audit U-2). A save without an id, or with an id that cannot be an
// article's, is told the reason only.
func idSaveRefusal(id, reason, otherwise string) error {
	if !idPattern.MatchString(id) {
		return errors.New(reason + otherwise)
	}
	return errors.New(reason + ". " + bodyWayOnUnread(id) + otherwise)
}

func validateIngestDraft(d *Draft) (string, error) {
	if d.Correction != nil && (!d.Correction.Valid() || d.Lesson == "") {
		return "", errors.New("a correction needs the source and quote of what the person said, and a lesson stating the next behavior to change")
	}
	d.Title, d.Lesson, d.AppliesTo = strings.TrimSpace(d.Title), strings.TrimSpace(d.Lesson), strings.TrimSpace(d.AppliesTo)
	d.Kind, d.Scene = strings.TrimSpace(d.Kind), strings.TrimSpace(d.Scene)
	body := ""
	if d.Body != nil {
		body = strings.TrimSpace(*d.Body)
	}
	if d.Rationale != nil && (len(*d.Rationale) > 1600 || !utf8.ValidString(*d.Rationale)) {
		return "", errors.New("rationale must be UTF-8, at most 1600 bytes")
	}
	if len(d.Sources) < 1 {
		return "", idSaveRefusal(d.ID, "wiki ingest needs sources: an array of source ids (from wiki capture or reflect query)", "")
	}
	if d.Lesson != "" && (strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.AppliesTo) == "") {
		return "", errors.New("a lesson needs a title and the conditions where it applies")
	}
	for _, s := range []string{d.Title, d.Lesson, d.AppliesTo, body, d.ChangeReason} {
		if !utf8.ValidString(s) {
			return "", errors.New("wiki text must be UTF-8")
		}
	}
	for _, f := range []struct {
		name  string
		size  int
		limit int
	}{{"title", len(d.Title), 200}, {"lesson", len(d.Lesson), 2000}, {"applies_to", len(d.AppliesTo), 1000}, {"body", len(body), 16 << 10}, {"change_reason", len(d.ChangeReason), 1000}} {
		if f.size > f.limit {
			return "", fmt.Errorf("wiki %s is %d bytes, over the limit of %d bytes: shorten it, or move older parts to a new page and link it with related", f.name, f.size, f.limit)
		}
	}
	if d.Lesson == "" && d.ID != "" {
		if idPattern.MatchString(d.ID) {
			return "", idSaveRefusal(d.ID, "refused: this save names the id "+d.ID+" and sends no lesson", " Otherwise a save with an id sends the lesson, title and applies_to again, with sources and change_reason.")
		}
		return "", errors.New("updating a page by id sends its lesson, title and applies_to again (read them with wiki query lesson:<id>), with sources and change_reason")
	}
	if d.Lesson == "" && (d.Title != "" || d.AppliesTo != "" || d.ID != "" || len(d.Replaces) > 0 || d.Body != nil || d.Rationale != nil || d.Watch != nil || d.Passages != nil || d.Learning != nil || d.Related != nil || d.ChangeReason != "" || d.Kind != "" || d.Scene != "") {
		return "", errors.New("reviewing a source without a lesson accepts only source ids")
	}
	for _, id := range append(append(append([]string{}, d.Sources...), d.Replaces...), d.Related...) {
		if !idPattern.MatchString(id) {
			return "", errors.New("wiki ids must be 16 lowercase hexadecimal characters")
		}
	}
	if d.ID != "" && !idPattern.MatchString(d.ID) {
		return "", errors.New("invalid wiki lesson id")
	}
	d.Sources = unique(d.Sources)
	return body, nil
}
