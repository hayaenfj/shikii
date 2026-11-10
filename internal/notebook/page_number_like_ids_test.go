package notebook

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

var (
	digitsOnlyID = regexp.MustCompile(`^[0-9]{16}$`)
	floatLikeID  = regexp.MustCompile(`^[0-9]+e[0-9]+$`)
)

// titleWithID finds a title whose article id (a hash of the title) has the wanted shape.
func titleWithID(t *testing.T, shape *regexp.Regexp) (string, string) {
	t.Helper()
	for n := range 200000 {
		title := fmt.Sprintf("Number-like article %d", n)
		if id := digest([]byte(title))[:16]; shape.MatchString(id) {
			return title, id
		}
	}
	t.Fatal("no title with the wanted id shape")
	return "", ""
}

// Ids are hexadecimal, so some look like a YAML integer or float. They are
// written as page ids, source ids, related and replaced ids and revisions.
func TestSavedPageWithNumberLikeIDsReadsBack(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: t.TempDir()}
	st := store.Store{Dir: b.Root}
	sources := []string{"0000000000000012", "1234567890123456", "0000000000001e10", "00000000000000e5", "0000000000000000"}
	for _, id := range sources {
		if err := st.AppendFeedback(b.Scope, store.Feedback{ID: id, Request: "reorder the steps", Summary: "reordered", Reply: "run the focused test first", At: time.Unix(1, 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	save := func(what string, d Draft) Page {
		t.Helper()
		r, err := b.Ingest(d)
		if !requireReadable(t, b, what, err) || err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		back, ok := requireReadsBack(t, b, what, *r.Page)
		if !ok {
			t.FailNow()
		}
		return back
	}
	draftFor := func(title string) Draft {
		return Draft{Title: title, Lesson: "Keep ids as text.", AppliesTo: "saving wiki ids", Kind: "rule", Scene: "when saving ids", Sources: sources}
	}
	digitsTitle, digitsID := titleWithID(t, digitsOnlyID)
	floatTitle, floatID := titleWithID(t, floatLikeID)
	digits := save("article with an all-digit id", draftFor(digitsTitle))
	float := save("article with a float-like id", draftFor(floatTitle))
	if digits.ID != digitsID || float.ID != floatID || len(digits.Sources) != len(sources) {
		t.Fatalf("the fixtures changed: %s %s %d", digits.ID, float.ID, len(digits.Sources))
	}
	both := strings.Join(unique([]string{digitsID, floatID}), ",")
	linked := draftFor("Linked to number-like articles")
	linked.Related = []string{digitsID, floatID}
	if got := save("related number-like ids", linked); strings.Join(got.Related, ",") != both {
		t.Errorf("related ids read back as %v", got.Related)
	}
	replacing := draftFor("Replaces number-like articles")
	replacing.Replaces = []string{digitsID, floatID}
	if got := save("replaced number-like ids", replacing); strings.Join(got.Replaces, ",") != both {
		t.Errorf("replaced ids read back as %v", got.Replaces)
	}
	// A revision is a hash of the content: change the lesson until it is number-like, then update once more so that it is also written as "previous".
	var numbered Page
	for n := 0; numbered.ID == ""; n++ {
		if n == 50000 {
			t.Fatal("no content with a number-like revision")
		}
		d := draftFor("Article with a number-like revision")
		d.Lesson = fmt.Sprintf("Keep revisions as text (%d).", n)
		r, err := b.PreviewCreate(d)
		if err != nil {
			t.Fatal(err)
		}
		if digitsOnlyID.MatchString(r.Page.Revision) || floatLikeID.MatchString(r.Page.Revision) {
			numbered = save("number-like revision", d)
		}
	}
	update := draftFor(numbered.Title)
	update.ID, update.Lesson, update.ChangeReason = numbered.ID, "Keep revisions as text after an update.", "check the previous revision"
	if got := save("number-like previous revision", update); got.Previous != numbered.Revision {
		t.Errorf("previous revision read back as %q, want %q", got.Previous, numbered.Revision)
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Errorf("lint after number-like ids: %+v %v", report.Issues, err)
	}
}
