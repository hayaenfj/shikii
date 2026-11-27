package notebook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.44.0 leftover C-4: every save of the agents may keep a bit more than half of the body before it and the body still wears away (8 lines,
// then 6 kept and 1 added, then 5 kept and 1 added, then 4 kept and 1 added: each save keeps 5 of 7 or 6 of 7 lines of the one before it).
// The refusal measures a save against the body before it and also against the revision the agents' saves started from (Page.KeptFrom), which
// stays until a save keeps every line of it again or a person's edit or a patch settles the body.
//
// Every line of these bodies is 18 bytes, so the shares are plain line counts.

// erosionLine is the line with a number; erosionBody joins lines. Numbers are two digits so that no line is a part of another.
func erosionLine(n int) string { return fmt.Sprintf("row %02dxxxxxxxxxxxx", n) }

func erosionBody(numbers ...int) string {
	lines := make([]string, 0, len(numbers))
	for _, n := range numbers {
		lines = append(lines, erosionLine(n))
	}
	return strings.Join(lines, "\n")
}

// erosionBook saves an article of the lines 1 to 8 and returns the book, the saved page and the draft that updates it by id.
func erosionBook(t *testing.T) (Book, Page, Draft) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "A body that is saved in small steps")
	body := erosionBody(1, 2, 3, 4, 5, 6, 7, 8)
	d.Body = &body
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = saved.Page.ID
	return b, *saved.Page, d
}

// erosionSave sends the lines as the body of the article by its id and returns the saved page.
func erosionSave(b Book, d Draft, numbers ...int) (Result, error) {
	body := erosionBody(numbers...)
	d.Body = &body
	return b.Ingest(d)
}

// erosionFirstTwoSaves makes the two saves that each pass on their own and returns the page of the second.
func erosionFirstTwoSaves(t *testing.T, b Book, d Draft) Page {
	t.Helper()
	if _, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 11); err != nil { // 6 of the 8 lines kept
		t.Fatalf("the first save keeps 6 of 8 lines and is refused: %v", err)
	}
	second, err := erosionSave(b, d, 1, 2, 3, 4, 5, 11, 12) // 6 of the 7 lines of the page before it, 5 of the 8 lines of the first revision
	if err != nil {
		t.Fatalf("the second save keeps 6 of 7 lines and is refused: %v", err)
	}
	return *second.Page
}

// The third save keeps 6 of the 7 lines of the body before it, and only 4 of the 8 lines the article started from: half of its bytes are
// gone since, which is refused (exactly half kept is refused), with the revision it is measured against and the share lost. The save
// that sends the whole body of the page with an addition is still accepted.
func TestIngestRefusesABodyThatWearsAwayInSmallSavesAndSaysWhichRevisionItIsMeasuredAgainst(t *testing.T) {
	b, first, d := erosionBook(t)
	second := erosionFirstTwoSaves(t, b, d)
	_, err := erosionSave(b, d, 1, 2, 3, 4, 11, 12, 13)
	if err == nil {
		t.Fatal("a save that keeps 4 of the 8 lines of the revision the saves started from was accepted: the body wears away in small saves")
	}
	for _, want := range []string{"refused", first.ID, "revision " + first.Revision, "drop 50%", "72 of 144 bytes", "shikii wiki patch", "unique ending"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal of a body that wore away lacks %q: %v", want, err)
		}
	}
	requireArticle(t, b, second.ID, second.Revision, second.Body)

	// What it asks for works: the whole body of the page with the addition keeps everything the page has.
	whole, err := erosionSave(b, d, 1, 2, 3, 4, 5, 11, 12, 13)
	if err != nil || whole.Page.Previous != second.Revision {
		t.Fatalf("the whole body of the page with an addition is refused: %v", err)
	}
}

// The reference revision is kept while a save has not brought back every line of it: after the first save it is the revision before, and
// the second save keeps it (it is not moved to the page of the first save, or the body would wear away one step at a time).
func TestTheRevisionThatSavesAreMeasuredAgainstStaysUntilEveryLineIsBack(t *testing.T) {
	b, first, d := erosionBook(t)
	if first.KeptFrom != "" {
		t.Fatalf("a new article has no reference revision: %q", first.KeptFrom)
	}
	one, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 11)
	if err != nil || one.Page.KeptFrom != first.Revision {
		t.Fatalf("a save that drops lines is measured against the revision before it from now on: %q %v", one.Page.KeptFrom, err)
	}
	two, err := erosionSave(b, d, 1, 2, 3, 4, 5, 11, 12)
	if err != nil || two.Page.KeptFrom != first.Revision {
		t.Fatalf("the second save is still measured against the first revision: %q %v", two.Page.KeptFrom, err)
	}
	// An addition to the body keeps the reference, as long as lines of it are still missing.
	three, err := erosionSave(b, d, 1, 2, 3, 4, 5, 11, 12, 13)
	if err != nil || three.Page.KeptFrom != first.Revision {
		t.Fatalf("an addition does not bring the missing lines back: %q %v", three.Page.KeptFrom, err)
	}
	// A save that has every line of the reference revision again starts afresh: nothing is missing, so nothing is measured against it.
	back, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 7, 8, 11, 12, 13)
	if err != nil || back.Page.KeptFrom != "" {
		t.Fatalf("a save with every line of the reference revision again still has one: %q %v", back.Page.KeptFrom, err)
	}
	// And the body that follows may lose lines in small steps again, measured from this page.
	if _, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 7, 11, 12, 13, 14); err != nil {
		t.Fatalf("a save after the reference was cleared is refused: %v", err)
	}
}

// A patch and the person's edit on the Knowledge page show exactly what changes, so what they save is settled: the reference is gone, and
// the agents' saves that follow are measured against that page. The same third save is refused without them (the first test) and passes
// after either of them.
func TestPatchAndTheKnowledgePageSettleTheBodyForTheSavesThatFollow(t *testing.T) {
	for _, entry := range []string{"patch", "the Knowledge page"} {
		b, _, d := erosionBook(t)
		second := erosionFirstTwoSaves(t, b, d)
		var settled Page
		switch entry {
		case "patch":
			last := erosionLine(12)
			patched, err := b.Patch(Patch{ID: second.ID, Revision: second.Revision, Sources: d.Sources, ChangeReason: "add a line at the end", Edits: []TextEdit{{Field: "body", Before: last, After: last + "\n" + erosionLine(13)}}})
			if err != nil {
				t.Fatal(err)
			}
			settled = *patched.Page
		default:
			managed, err := b.Manage(Change{ID: second.ID, Revision: second.Revision, Action: "edit", Title: second.Title, Lesson: second.Lesson, AppliesTo: second.AppliesTo, Body: second.Body + "\n" + erosionLine(13), Kind: second.Kind, Scene: second.Scene})
			if err != nil {
				t.Fatal(err)
			}
			settled = managed
		}
		if settled.KeptFrom != "" {
			t.Fatalf("%s left the reference revision of the agents' saves in place: %q", entry, settled.KeptFrom)
		}
		// 4 of the 8 lines of the first revision, 6 of the 8 lines of the page the person or the patch saved.
		if _, err := erosionSave(b, d, 1, 2, 3, 4, 11, 12, 13, 14); err != nil {
			t.Fatalf("after %s the next save, which keeps 7 of 8 lines of the page it was made on, is refused: %v", entry, err)
		}
	}
}

// A new article and an article that no save has dropped lines from have no reference revision, so what has always been saved is as it was:
// a page file without kept_from reads, saves and keeps its revision, and the field is neither in the file nor in the JSON when it is empty
// (the revision is made from the JSON, so a field that showed when empty would change the revision of every article that exists).
func TestAnArticleWithoutAReferenceRevisionIsReadAndSavedAsBefore(t *testing.T) {
	b, first, d := erosionBook(t)
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(dir, first.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(file), "kept_from") || strings.Contains(string(raw), "kept_from") {
		t.Fatalf("the empty reference revision is written out: file %v, json %v", strings.Contains(string(file), "kept_from"), strings.Contains(string(raw), "kept_from"))
	}
	back, err := parsePage(file)
	if err != nil || back.KeptFrom != "" || revision(back) != first.Revision {
		t.Fatalf("a page file without kept_from does not read back as the page: %q %v", back.KeptFrom, err)
	}
	added, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 7, 8, 11)
	if err != nil || added.Page.KeptFrom != "" || added.Page.Previous != first.Revision {
		t.Fatalf("an addition to an article without a reference revision is refused or gets one: %q %v", added.Page.KeptFrom, err)
	}
	// A reference that is written in the file reads back too, and a malformed one is not a page.
	one, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 11)
	if err != nil {
		t.Fatal(err)
	}
	file, err = os.ReadFile(filepath.Join(dir, first.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	back, err = parsePage(file)
	if err != nil || back.KeptFrom != one.Page.KeptFrom || back.KeptFrom == "" || revision(back) != one.Page.Revision {
		t.Fatalf("a page file with kept_from does not read back as the page: %q %v", back.KeptFrom, err)
	}
	if _, err := parsePage([]byte(strings.Replace(string(file), "kept_from: "+one.Page.KeptFrom, "kept_from: ../x", 1))); err == nil {
		t.Fatal("a page whose kept_from is not a revision is read")
	}
}

// A reference revision that cannot be read is no reason to refuse a save: the save is measured against the body before it only, and the
// page it makes is measured from its own revision on, so the unreadable one is not carried along.
func TestAReferenceRevisionThatCannotBeReadDoesNotStopASave(t *testing.T) {
	b, first, d := erosionBook(t)
	if _, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 11); err != nil {
		t.Fatal(err)
	}
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "revisions", first.ID+"-"+first.Revision+".md")); err != nil {
		t.Fatal(err)
	}
	two, err := erosionSave(b, d, 1, 2, 3, 4, 11, 12) // 5 of the 7 lines of the page before it; the 4 of 8 of the unreadable revision is not measured
	if err != nil {
		t.Fatalf("a save is refused because the reference revision cannot be read: %v", err)
	}
	if two.Page.KeptFrom == first.Revision {
		t.Fatalf("the unreadable reference revision is carried along: %q", two.Page.KeptFrom)
	}
}

// The refusals say to add with a patch first: before a unique ending of the body and after that ending with the new sentences. The whole
// body sent again is the way for what a patch does not fit, and comes after (0.44.0 leftover U-11: copying a body of 100 lines by hand
// lost 49% of it without a refusal, so an agent that adds should not be sent to do that first).
func TestTheBodyLossRefusalsPointToAPatchThatAddsAtTheEndBeforeResendingTheBody(t *testing.T) {
	b, _, d := lossBook(t)
	short := "Only the new sentence."
	d.Body = &short
	_, withID := b.Ingest(d)
	d.ID = ""
	_, byTitle := b.Ingest(d)
	eb, _, ed := erosionBook(t)
	erosionFirstTwoSaves(t, eb, ed)
	_, worn := erosionSave(eb, ed, 1, 2, 3, 4, 11, 12, 13)
	ed.ID = ""
	_, wornByTitle := erosionSave(eb, ed, 1, 2, 3, 4, 11, 12, 13)
	for name, refusal := range map[string]error{"a save with an id": withID, "a save by the title": byTitle, "a save that wears the body away": worn, "a save by the title that wears the body away": wornByTitle} {
		if refusal == nil {
			t.Fatalf("%s is not refused", name)
		}
		text := refusal.Error()
		patch, ending, resend := strings.Index(text, "shikii wiki patch"), strings.Index(text, "unique ending"), strings.Index(text, "whole body again")
		if patch < 0 || ending < patch || resend < ending {
			t.Fatalf("%s: the refusal must name the patch, then the unique ending of the body to add after, and only then the whole body sent again (patch %d, unique ending %d, whole body again %d): %s", name, patch, ending, resend, text)
		}
		for _, want := range []string{"before", "after", "ending plus the new text"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: the refusal lacks %q: %s", name, want, text)
			}
		}
		// A patch needs the article's id and its current revision, and the article is read first (0.44.1 audit D-3): the way on says so
		// before the unique ending, not only in the branch for a patch that does not fit.
		read := strings.Index(text, "current revision")
		query := strings.Index(text, "shikii wiki query lesson:")
		if read < patch || read > ending || query < patch || query > ending {
			t.Fatalf("%s: the refusal must say, between naming the patch and the unique ending, that a patch needs the article's id and current revision and that the article is read first with shikii wiki query lesson:<id> (patch %d, current revision %d, query %d, unique ending %d): %s", name, patch, read, query, ending, text)
		}
	}
}

// A save that sends no body (it changes the lesson or another field and leaves the body out) keeps the body the page has, and is guarded
// like the saves that send one: the reference revision is carried on, so the saves that follow cannot use it to start the count again
// (a save that leaves the body out between two that cut would otherwise give the wearing away a fresh start every time).
func TestASaveWithoutABodyCarriesTheReferenceRevisionOn(t *testing.T) {
	b, first, d := erosionBook(t)
	second := erosionFirstTwoSaves(t, b, d)
	if second.KeptFrom != first.Revision {
		t.Fatalf("the two saves that cut did not leave the first revision as the reference: %q", second.KeptFrom)
	}
	noBody := d
	noBody.Body = nil
	noBody.Lesson = "Check model startup instructions and the license files when updating README installation instructions."
	kept, err := b.Ingest(noBody)
	if err != nil {
		t.Fatalf("a save that leaves the body out and changes the lesson is refused: %v", err)
	}
	if kept.Page.Body != second.Body || kept.Page.Lesson != noBody.Lesson || kept.Page.Previous != second.Revision {
		t.Fatalf("the save without a body did not change the lesson and keep the body: %+v", kept.Page)
	}
	if kept.Page.KeptFrom != first.Revision {
		t.Fatalf("a save that sends no body dropped the reference revision (%q, want %q): the next saves would start the count again", kept.Page.KeptFrom, first.Revision)
	}
	// The save that follows is measured against the first revision still: 6 of the 7 lines of the page, 4 of the 8 lines of the first.
	_, err = erosionSave(b, d, 1, 2, 3, 4, 11, 12, 13)
	if err == nil {
		t.Fatal("a save after one without a body is not measured against the first revision any more: the body wears away in small steps again")
	}
	if !strings.Contains(err.Error(), "revision "+first.Revision) {
		t.Fatalf("the refusal does not name the revision the saves started from: %v", err)
	}
	requireArticle(t, b, kept.Page.ID, kept.Page.Revision, kept.Page.Body)
}

// A save that names no id finds its article by the title (the id is made from the first title), and the comparison with the revision the
// saves started from works there as with an id: the third save is refused and names that revision, and the article stays as it was.
func TestASaveByTheTitleIsMeasuredAgainstTheRevisionTheSavesStartedFrom(t *testing.T) {
	b, first, d := erosionBook(t)
	d.ID = "" // the title of the draft is the first title of the article
	if _, err := erosionSave(b, d, 1, 2, 3, 4, 5, 6, 11); err != nil {
		t.Fatalf("the first save by the title keeps 6 of 8 lines and is refused: %v", err)
	}
	second, err := erosionSave(b, d, 1, 2, 3, 4, 5, 11, 12)
	if err != nil {
		t.Fatalf("the second save by the title keeps 6 of 7 lines and is refused: %v", err)
	}
	if second.Page.ID != first.ID || second.Page.KeptFrom != first.Revision {
		t.Fatalf("the saves by the title did not reach the article or did not keep its first revision as the reference: %q %q", second.Page.ID, second.Page.KeptFrom)
	}
	_, err = erosionSave(b, d, 1, 2, 3, 4, 11, 12, 13) // 6 of 7 lines of the page, 4 of 8 of the first revision
	if err == nil {
		t.Fatal("a save by the title that keeps 4 of the 8 lines of the revision the saves started from was accepted: the comparison is skipped when no id is sent")
	}
	for _, want := range []string{"refused", first.ID, "revision " + first.Revision, "drop 50%"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal of a save by the title lacks %q: %v", want, err)
		}
	}
	requireArticle(t, b, second.Page.ID, second.Page.Revision, second.Page.Body)
}

// The refusal that measures against the revision the saves started from tells a save without an id what the refusal of the first kind
// tells it: the article was found by the title, so a different lesson that has the same title takes another one. A save with an id is
// not told that (it names the article itself).
func TestTheBasisRefusalOfASaveWithoutAnIdTellsADifferentLessonToTakeAnotherTitle(t *testing.T) {
	b, first, d := erosionBook(t)
	erosionFirstTwoSaves(t, b, d)
	_, withID := erosionSave(b, d, 1, 2, 3, 4, 11, 12, 13)
	d.ID = ""
	_, byTitle := erosionSave(b, d, 1, 2, 3, 4, 11, 12, 13)
	if withID == nil || byTitle == nil {
		t.Fatalf("a save that wears the body away is accepted: with an id %v, by the title %v", withID, byTitle)
	}
	for _, want := range []string{"revision " + first.Revision, "Saving without an id finds the article by its title", "first title of article " + first.ID, "give it another title", "shikii wiki patch"} {
		if !strings.Contains(byTitle.Error(), want) {
			t.Fatalf("the refusal of a save by the title lacks %q: %v", want, byTitle)
		}
	}
	if strings.Contains(byTitle.Error(), "Saving with an id") {
		t.Fatalf("the refusal of a save without an id says an id was sent: %v", byTitle)
	}
	if !strings.Contains(withID.Error(), "Saving with an id replaces the whole body") || strings.Contains(withID.Error(), "another title") {
		t.Fatalf("the refusal of a save with an id must say that an id replaces the body, and must not tell it to take another title: %v", withID)
	}
}
