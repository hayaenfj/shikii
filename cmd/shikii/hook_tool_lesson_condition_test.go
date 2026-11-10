package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// A lesson silent at a door stays silent there when a new revision leaves its title, lesson and applies_to alone (a section added
// to the body). Repairing the condition gives it a new start.
func TestSilenceAtADoorSurvivesAnEditThatLeavesTheConditionAlone(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	commit := map[string]any{"command": "git commit -m x"}
	for _, session := range []string{"a", "b"} {
		if ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Bash", commit)); !strings.Contains(ctx, ids["commit"]) {
			t.Fatalf("precondition: delivered in %s:\n%s", session, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		memoryEffectComplete(t, st, scope, session)
		assess(t, st, scope, d, ids["commit"], "unused")
	}
	if out := toolHook(t, scope, "c", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("precondition: silent at git commit:\n%s", out)
	}
	book := notebook.Book{Root: st.Dir, Scope: scope}
	id := strings.Split(ids["commit"], ":")[1]
	current, err := book.Query("lesson:" + id)
	if err != nil || len(current.Lessons) != 1 {
		t.Fatal(err)
	}
	p := current.Lessons[0]
	sources := []string{}
	for _, s := range p.Sources {
		sources = append(sources, s.ID)
	}
	body := "A note added later; the condition is the same."
	revised, err := book.Ingest(notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: p.Scene, Body: &body, Sources: sources})
	if err != nil || revised.Page.Revision == p.Revision {
		t.Fatalf("the body edit makes a new revision: %v", err)
	}
	if out := toolHook(t, scope, "d", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("a body edit does not start the count again: still silent at git commit:\n%s", out)
	}
	repaired, err := book.Ingest(notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: "right before git commit in shikii", Kind: p.Kind, Scene: p.Scene, Body: &body, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "e", toolPrompt, "Bash", commit)); !strings.Contains(ctx, "lesson:"+id+":"+repaired.Page.Revision) {
		t.Fatalf("a repaired condition gets a new start:\n%s", ctx)
	}
}
