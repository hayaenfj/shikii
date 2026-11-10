package notebook

import (
	"fmt"
	"testing"
	"time"
)

func roundTripSource(t *testing.T, b Book, n int) string {
	t.Helper()
	o, err := b.Capture(Observation{Kind: "agent", Title: fmt.Sprintf("source %d", n), Content: fmt.Sprintf("checked result %d", n), Reference: "test", Version: "v1", ObservedAt: time.Date(2026, 1, 1, 0, 0, n, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// A save that reports success must leave a page that reads back as the same
// revision. A refused save must leave the wiki as it was. Either way the next,
// unrelated save must still work.
func requireWikiStaysWritable(t *testing.T, b Book, source string, saved *Page, err error) {
	t.Helper()
	issues, issueErr := b.PageIssues()
	if issueErr != nil || len(issues) > 0 {
		t.Errorf("save error=%v, but the wiki now has unreadable pages: %+v %v", err, issues, issueErr)
	}
	if err == nil {
		v, queryErr := b.Query("lesson:" + saved.ID)
		if queryErr != nil || len(v.Lessons) != 1 || v.Lessons[0].Revision != saved.Revision {
			t.Errorf("the saved revision %s@%s cannot be read back: %v %+v", saved.ID, saved.Revision, queryErr, v.Issues)
		}
	}
	if _, nextErr := b.Ingest(Draft{Title: "Unrelated next lesson", Lesson: "Keep the next lesson.", AppliesTo: "any later save", Kind: "rule", Scene: "when saving later", Sources: []string{source}}); nextErr != nil {
		t.Errorf("an unrelated later save is refused: %v", nextErr)
	}
}

func TestSavedPageWithIndentedMultilineTextReadsBack(t *testing.T) {
	indented := "  - run the focused test\n  - then run the whole suite"
	cases := map[string]func(d *Draft, source string){
		"passage quote that is an indented list": func(d *Draft, _ string) {
			body := "Steps:\n" + indented + "\nDone."
			d.Body = &body
			d.Passages = &[]KnowledgePassage{{Role: "current", Quote: indented, Conditions: "before a commit", Reason: "the order matters"}}
		},
		"passage reason that starts with a line break": func(d *Draft, _ string) {
			body := "Run the focused test first."
			d.Body = &body
			d.Passages = &[]KnowledgePassage{{Role: "current", Quote: "Run the focused test first.", Conditions: "before a commit", Reason: "\nthe whole suite is slow"}}
		},
		"change_reason that starts with a line break": func(d *Draft, _ string) {
			d.ChangeReason = "\nthe person corrected the order"
		},
		"practice step that is an indented block": func(d *Draft, source string) {
			d.Kind = "howto"
			d.Learning = &Learning{Practice: &KnowledgePractice{Steps: []PracticeStep{{Condition: "before a commit", Action: indented, Avoid: "skipping the focused test", Check: "both pass", Sources: []string{source}}}}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/project"}
			source := roundTripSource(t, b, 1)
			d := Draft{Title: "Test order", Lesson: "Run the focused test before the whole suite.", AppliesTo: "before a commit", Kind: "rule", Scene: "when committing", Sources: []string{source}}
			change(&d, source)
			result, err := b.Ingest(d)
			requireWikiStaysWritable(t, b, source, result.Page, err)
		})
	}
}

func TestPatchWithIndentedPassageKeepsTheArticleReadable(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	source := roundTripSource(t, b, 1)
	body := "Steps:\n  - run the focused test\n  - then run the whole suite\nDone."
	first, err := b.Ingest(Draft{Title: "Test order", Lesson: "Run the focused test before the whole suite.", AppliesTo: "before a commit", Body: &body, Kind: "rule", Scene: "when committing", Sources: []string{source}})
	if err != nil {
		t.Fatal(err)
	}
	passages := []KnowledgePassage{{Role: "current", Quote: "  - run the focused test\n  - then run the whole suite", Conditions: "before a commit", Reason: "the order matters"}}
	result, err := b.Patch(Patch{ID: first.Page.ID, Revision: first.Page.Revision, Sources: []string{source}, ChangeReason: "classify the steps", Passages: &passages})
	requireWikiStaysWritable(t, b, source, result.Page, err)
	v, queryErr := b.Query("lesson:" + first.Page.ID)
	if queryErr != nil || len(v.Lessons) != 1 {
		t.Fatalf("the article disappeared after the patch (patch error=%v): %v %+v", err, queryErr, v.Issues)
	}
	if err != nil && v.Lessons[0].Revision != first.Page.Revision {
		t.Fatalf("a refused patch changed the article: %s", v.Lessons[0].Revision)
	}
}
