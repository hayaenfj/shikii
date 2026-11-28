package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 2026-11-28: the prepared work that a request hook hands to an agent carries each article as one text (planningArticleSource: title,
// summary and applies-when lines, then the body), and the orientation of the same entry wrote the summary and the conditions a second
// time. The orientation leaves a text out when the entry it belongs to holds the very same string (work_orientation.go); these tests
// hold it to the same rule for the lines of the entry's text, and to the same care: a text that is not there, or is there only
// somewhere else than in the header lines, is written by the orientation as before.

// hookEntry is an article as the request hook delivers it: its text, with the orientation bound to the entry.
func hookEntry(p notebook.Page, text string) map[string]any {
	e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "text": text, "body_included": true,
		"query": map[string]any{"operation": "query", "query": "lesson:" + p.ID + "@" + p.Revision}}
	bindOrientation(e, notebook.Orient(p, "owner approval", 2400))
	return e
}

func TestOrientationOmitsTheTextsTheLinesOfAHookEntryHold(t *testing.T) {
	p := orientationPage(10)
	e := hookEntry(p, planningArticleSource(p))
	o := orientationOfJSON(t, e)
	for _, key := range []string{"id", "revision", "summary", "conditions"} {
		if _, there := o[key]; there {
			t.Errorf("the orientation repeats %s, which the entry's own text or fields hold", key)
		}
	}
	for _, key := range orientationAlways {
		if _, there := o[key]; !there {
			t.Errorf("the orientation keeps %s, empty or not", key)
		}
	}
	text, _ := e["text"].(string)
	for _, want := range []string{"\nsummary: " + p.Lesson + "\n", "\napplies when: " + p.AppliesTo + "\n"} {
		if strings.Count(text, want) != 1 {
			t.Errorf("the entry's text holds %q once: %q", want, text)
		}
	}
	if cur, _ := o["current"].([]any); len(cur) != 1 {
		t.Errorf("the interpreted passage stays in the orientation: %v", cur)
	}
}

func TestOrientationWritesATextTheLinesOfAHookEntryDoNotHold(t *testing.T) {
	p := orientationPage(10)
	other := p
	other.Lesson, other.AppliesTo = "Another summary.", "Another condition."
	inBody := p
	inBody.Body = "summary: " + p.Lesson + "\napplies when: " + p.AppliesTo + "\n\n" + p.Body
	header := func(page notebook.Page) string { return "wiki article\ntitle: " + page.Title + "\n" }
	summaryLine, conditionsLine := "summary: "+p.Lesson+"\n", "applies when: "+p.AppliesTo+"\n"
	for _, c := range []struct {
		name                            string
		text                            string
		writesSummary, writesConditions bool
	}{
		{"another article's lines", planningArticleSource(other), true, true},
		{"the values only in the body", header(p) + "\n" + inBody.Body, true, true},
		{"a line that goes on", header(p) + "summary: " + p.Lesson + " and more\n" + conditionsLine + "\nbody", true, false},
		{"no text", "", true, true},
		{"the lines after a blank line", header(p) + "\n" + summaryLine + conditionsLine, true, true},
		{"the value inside a longer line", header(p) + "see " + summaryLine + "see " + conditionsLine + "\nbody", true, true},
		{"the summary alone", header(p) + summaryLine + "\nbody", false, true},
		{"the conditions alone", header(p) + conditionsLine + "\nbody", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := orientationOfJSON(t, hookEntry(p, c.text))
			if got := o["summary"] == p.Lesson; got != c.writesSummary {
				t.Errorf("the orientation writes the summary: %v, want %v (%v)", got, c.writesSummary, o["summary"])
			}
			if got := o["conditions"] == p.AppliesTo; got != c.writesConditions {
				t.Errorf("the orientation writes the conditions: %v, want %v (%v)", got, c.writesConditions, o["conditions"])
			}
		})
	}
}

// A text that leaves the entry after the orientation was made (limitWorkPreparation takes fields off an entry) is written again.
func TestOrientationWritesTheTextsOnceAHookEntryLosesItsText(t *testing.T) {
	p := orientationPage(10)
	e := hookEntry(p, planningArticleSource(p))
	delete(e, "text")
	o := orientationOfJSON(t, e)
	if o["summary"] != p.Lesson || o["conditions"] != p.AppliesTo {
		t.Errorf("the orientation writes what the entry no longer holds: %v", o)
	}
}

// The record of a delivery names where each text stood; a reply that refers back to it names the place, and the place of a text in
// the entry's text is that field.
func TestAReusedDeliveryNamesTheTextWhereASummaryStood(t *testing.T) {
	fields := reusedMetadataFields(workMetadataRef{Held: map[string]string{"id": "id", "revision": "revision", "summary": "text", "conditions": "text"}})
	for _, want := range []string{"id", "revision", "text", "orientation.current", "orientation.history"} {
		if !slices.Contains(fields, want) {
			t.Errorf("the reply names %s: %v", want, fields)
		}
	}
	for _, not := range []string{"orientation.summary", "orientation.conditions"} {
		if slices.Contains(fields, not) {
			t.Errorf("the reply does not point to %s, which the delivery did not write: %v", not, fields)
		}
	}
	if len(slices.Compact(slices.Clone(fields))) != len(fields) {
		t.Errorf("a name is written once: %v", fields)
	}
}

// Through the hook: the entries of the prepared work hold the summary and the applies-when line once, in their text.
func TestTheHookPreparedWorkWritesEachArticlesSummaryOnce(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	for i := 0; i < 3; i++ {
		interimAddArticle(t, book, fmt.Sprintf("Archive approval %d", i), strings.Repeat("Restoring production needs the owner's approval. Get approval before you restore the archive. ", 3))
	}
	actAs(t, "codex")
	ctx := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "orientation-once", "cwd": repo, "prompt": "Tell me the conditions for restoring production from an archive."}))
	v := preparedOf(t, ctx)
	entries, _ := v["knowledge"].([]any)
	if len(entries) == 0 {
		t.Fatalf("the prepared work carries articles: %.300s", ctx)
	}
	for _, raw := range entries {
		e, _ := raw.(map[string]any)
		text, _ := e["text"].(string)
		o, _ := e["orientation"].(map[string]any)
		if !strings.Contains(text, "\nsummary: ") || o == nil {
			t.Fatalf("precondition: an entry carries its text and an orientation: %v", e)
		}
		for _, key := range []string{"summary", "conditions"} {
			if _, there := o[key]; there {
				t.Errorf("the orientation of %v repeats %s that its text holds", e["ref"], key)
			}
		}
	}
}
