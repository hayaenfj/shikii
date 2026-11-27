package notebook

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const lossBody = "## Background\n\n- Agents sent only their new sentences to an article's id.\n- A save with an id replaces the whole body.\n\n## Decided\n\n- A save that drops half or more of the old body is refused.\n- Shortening goes through wiki patch.\n"

// lossBook saves an article with lossBody and returns the book, the saved page and a draft that updates it by id with the same summary.
func lossBook(t *testing.T) (Book, Page, Draft) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Saving into an existing article")
	body := lossBody
	d.Body = &body
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = saved.Page.ID
	return b, *saved.Page, d
}

func requireArticle(t *testing.T, b Book, id, revision, body string) {
	t.Helper()
	got, err := b.Query("lesson:" + id)
	if err != nil || len(got.Lessons) != 1 || got.Lessons[0].Revision != revision || got.Lessons[0].Body != strings.TrimSpace(body) {
		t.Fatalf("the article is not as it was (revision %s): %+v %v", revision, got.Lessons, err)
	}
}

// requireAskForAPatchFirst checks the three things the sentence of a body refusal is for, which the wording may change around: the patch comes
// before the whole body sent again; what a patch needs is written between them (the article's id, its current revision, sources, a
// change_reason and the shape of an edit with a unique ending of the body, and the article read first); and a save that named no id
// is told to give a different lesson another title (an id save is not told that, since it named the article itself).
func requireAskForAPatchFirst(t *testing.T, refusal error, id, revision string, idSent bool) {
	t.Helper()
	msg := refusal.Error()
	patch := strings.Index(msg, "shikii wiki patch")
	resend := strings.Index(msg, "Resend the whole body")
	if patch < 0 || resend < 0 || patch > resend {
		t.Fatalf("the refusal must name shikii wiki patch before it says to resend the whole body (patch %d, resend %d): %s", patch, resend, msg)
	}
	for _, want := range []string{
		`{"id":"` + id + `","revision":"` + revision + `","sources":`,
		`"change_reason":`,
		`"edits":[{"field":"body","before":"<a unique ending of the body>","after":`,
		"shikii wiki query lesson:" + id,
	} {
		at := strings.Index(msg, want)
		if at < patch || at > resend {
			t.Fatalf("what a patch needs must be written between the patch and the resend, and %q is not (at %d, patch %d, resend %d): %s", want, at, patch, resend, msg)
		}
	}
	if idSent && strings.Contains(msg, "another title") {
		t.Fatalf("a save with an id is told to take another title: %s", msg)
	}
	if !idSent {
		for _, want := range []string{"Without an id, the title found article " + id, "give it another title"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("the refusal of a save without an id lacks %q: %s", want, msg)
			}
		}
	}
}

// bodyRefusalCase is one refusal of a body save with what it has to say: the article's id and its current revision (the one the patch it
// asks for names), and whether the save sent the id.
type bodyRefusalCase struct {
	err          error
	id, revision string
	idSent       bool
}

// bodyRefusalUnread is the revision the patch names in a refusal that has not read the article (a save with an id that lacks a lesson or
// its sources is refused before the article is read): the revision is the one the query the patch asks for shows.
func bodyRefusalUnread(id string) string {
	return "<revision shown by shikii wiki query lesson:" + id + ">"
}

// bodyRefusalCases makes the refusals that send an agent to a patch: the loss refusal with an id and by the title, the refusal that measures
// against the revision the saves started from, with an id and by the title, and the two refusals of a save with an id that lacks a lesson
// or its sources (an agent told only what is missing sends the whole article again, and a body copied wrong loses a part of it).
func bodyRefusalCases(t *testing.T) map[string]bodyRefusalCase {
	t.Helper()
	b, old, d := lossBook(t)
	short := "Only the new sentence."
	d.Body = &short
	_, withID := b.Ingest(d)
	d.ID = ""
	_, byTitle := b.Ingest(d)
	_, noLesson := b.Ingest(Draft{ID: old.ID, Sources: d.Sources, ChangeReason: "check"})
	noSources := d
	noSources.ID, noSources.Sources = old.ID, nil
	_, noSourcesErr := b.Ingest(noSources)

	eb, first, ed := erosionBook(t)
	second := erosionFirstTwoSaves(t, eb, ed) // the revision the article has now; the refusal measures against first.Revision
	_, worn := erosionSave(eb, ed, 1, 2, 3, 4, 11, 12, 13)
	ed.ID = ""
	_, wornByTitle := erosionSave(eb, ed, 1, 2, 3, 4, 11, 12, 13)

	cases := map[string]bodyRefusalCase{
		"a save with an id":                            {withID, old.ID, old.Revision, true},
		"a save by the title":                          {byTitle, old.ID, old.Revision, false},
		"a save that wears the body away":              {worn, first.ID, second.Revision, true},
		"a save by the title that wears the body away": {wornByTitle, first.ID, second.Revision, false},
		"a save with an id and no lesson":              {noLesson, old.ID, bodyRefusalUnread(old.ID), true},
		"a save with an id and no sources":             {noSourcesErr, old.ID, bodyRefusalUnread(old.ID), true},
	}
	for name, c := range cases {
		if c.err == nil {
			t.Fatalf("%s is not refused", name)
		}
	}
	return cases
}

// bodyRefusalMaxBytes is the most an agent reads in one refusal of a body save (a refusal was 917 to 1429 bytes: the cause and the way on
// were buried in sentences that explain the measure). The longest, the basis refusal of a save by the title, is 761 bytes with the numbers
// at their widest (refusalAtWidestNumbers); the rest is room for a few words.
const bodyRefusalMaxBytes = 770

// refusalAtWidestNumbers is the refusal with its numbers as wide as they can be: the share lost is at most 100% and a body is at most
// 16 KiB, so its bytes have at most 5 digits. The cases of the test have small articles, and the length a large article's refusal has is
// what the limit is about (0.44.2 confirming audit C-3).
var refusalShare, refusalBytes = regexp.MustCompile(`\b\d+%`), regexp.MustCompile(`\b\d+ of \d+ bytes`)

func refusalAtWidestNumbers(text string) string {
	return refusalBytes.ReplaceAllString(refusalShare.ReplaceAllString(text, "100%"), "16384 of 16384 bytes")
}

// All the refusals of a body save stay short, with their numbers at their widest.
func TestTheBodyRefusalsStayShort(t *testing.T) {
	for name, c := range bodyRefusalCases(t) {
		if n := len(refusalAtWidestNumbers(c.err.Error())); n > bodyRefusalMaxBytes {
			t.Errorf("%s: the refusal is %d bytes with its numbers at their widest, over %d: %s", name, n, bodyRefusalMaxBytes, c.err)
		}
	}
	// negative: the widening reaches the numbers of a refusal (a refusal whose numbers it missed would be measured short)
	if got := refusalAtWidestNumbers("drop 7% of it (9 of 12 bytes of its lines"); got != "drop 100% of it (16384 of 16384 bytes of its lines" {
		t.Fatalf("the numbers are not widened: %q", got)
	}
}

// A save with an id that lacks its lesson (an agent that sends only what it adds) or its sources is refused before the article is read, and
// the refusal used to say only what was missing: Claude Code and Codex, given a sentence to add, filled in the fields and sent the whole
// article again, which loses a part of the body when it is copied wrong. The refusal names the patch first, as the refusal of a body save does;
// what a save that is not a patch sends comes after it. A save without an id, or with an id that cannot be an article's, is refused as before.
func TestTheRefusalsOfAnIdSaveThatLacksALessonOrSourcesNameThePatchBeforeTheFieldsToSendAgain(t *testing.T) {
	cases := bodyRefusalCases(t)
	noLesson := cases["a save with an id and no lesson"]
	requireAskForAPatchFirst(t, noLesson.err, noLesson.id, noLesson.revision, true)
	text := noLesson.err.Error()
	patch, again := strings.Index(text, "shikii wiki patch"), strings.Index(text, "applies_to again")
	if patch < 0 || again < patch {
		t.Fatalf("the refusal of a save with an id and no lesson must name the patch before it says to send the lesson, title and applies_to again (patch %d, again %d): %s", patch, again, text)
	}
	if !strings.Contains(text, "lesson") {
		t.Fatalf("the refusal of a save with an id and no lesson does not say that the lesson is missing: %s", text)
	}
	if text := cases["a save with an id and no sources"].err.Error(); !strings.Contains(text, "needs sources") {
		t.Fatalf("the refusal of a save with an id and no sources does not say that the sources are missing: %s", text)
	}
}

// 0.44.2 audit U-2: the refusal of a save that sends an id and no lesson comes before the article is read, so it cannot know that the id names
// an article, and an id of the right form may name none. It said "this save names article <id>" for any such id; it now says that the save names
// the id, for an article that exists and for one that does not alike.
func TestTheRefusalOfAnIdSaveWithNoLessonDoesNotSayThatTheIdIsAnArticle(t *testing.T) {
	b, old, d := lossBook(t)
	for _, c := range []struct{ name, id string }{{"an id that names an article", old.ID}, {"an id of the right form that names no article", "0123456789abcdef"}} {
		_, err := b.Ingest(Draft{ID: c.id, Sources: d.Sources, ChangeReason: "check"})
		if err == nil {
			t.Fatalf("%s: a save with an id and no lesson was accepted", c.name)
		}
		text := err.Error()
		if want := "refused: this save names the id " + c.id + " and sends no lesson"; !strings.Contains(text, want) {
			t.Errorf("%s: the refusal lacks %q: %s", c.name, want, text)
		}
		if strings.Contains(text, "names article") {
			t.Errorf("%s: the refusal calls an id that it has not looked up an article: %s", c.name, text)
		}
	}
}

func TestTheRefusalsOfASaveWithoutAValidIdThatLacksAThingStayAsTheyWere(t *testing.T) {
	const needSources = "wiki ingest needs sources: an array of source ids (from wiki capture or reflect query)"
	const sourceReview = "reviewing a source without a lesson accepts only source ids"
	const updateByID = "updating a page by id sends its lesson, title and applies_to again (read them with wiki query lesson:<id>), with sources and change_reason"
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	sources := []string{seed(t, b, 1)}
	for _, c := range []struct {
		name string
		d    Draft
		want string
	}{
		{"no id, no sources", Draft{Title: "a page", Lesson: "a lesson", AppliesTo: "when saving"}, needSources},
		{"no id, no lesson, a title", Draft{Sources: sources, Title: "a page"}, sourceReview},
		{"an id that is not an article's, no sources", Draft{ID: "not-an-id", Title: "a page", Lesson: "a lesson", AppliesTo: "when saving"}, needSources},
		{"an id that is not an article's, no lesson", Draft{ID: "not-an-id", Sources: sources, ChangeReason: "check"}, updateByID},
	} {
		_, err := b.Ingest(c.d)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: the refusal is not as it was:\n got %v\nwant %s", c.name, err, c.want)
		}
	}
}

// (a) An agent that sends only its new sentence under the id of an article would replace the body with it. The save is refused with
// the cause and the way on, and the article stays at its revision. The same holds when the id is the one made from the first title.
func TestIngestRefusesANewShortBodyAndKeepsTheArticle(t *testing.T) {
	for _, byTitle := range []bool{false, true} {
		b, old, d := lossBook(t)
		short := "Only the new sentence."
		d.Body = &short
		if byTitle {
			d.ID = "" // the title is the first one, so its id is the article's
		}
		_, err := b.Ingest(d)
		if err == nil {
			t.Fatalf("a save that replaces the whole body with one new sentence was accepted (by title %v)", byTitle)
		}
		for _, want := range []string{"refused", old.ID, "100%", "shikii wiki patch", "shikii wiki query lesson:" + old.ID} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal names the cause and what to send instead, and lacks %q: %v", want, err)
			}
		}
		requireAskForAPatchFirst(t, err, old.ID, old.Revision, !byTitle)
		requireArticle(t, b, old.ID, old.Revision, lossBody)
	}
}

// A line is kept only when it is in the new body whole, so a rewording inside a long line, as small as one word, counts the whole line as
// dropped (a body whose paragraphs are single lines loses nearly all its bytes to it). The refusal says so and points the small edit to
// patch (0.44.0 audit A-1); without that sentence the "98%" it names reads as a body that really goes.
func TestTheBodyLossRefusalSaysThatAChangeInsideALineDropsTheWholeLine(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "A body of one long line")
	long := "## Background\n\n" + strings.Repeat("The agent reads the article and keeps its whole body when it adds a sentence. ", 12)
	d.Body = &long
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.ID = saved.Page.ID
	reworded := strings.Replace(long, "keeps", "retains", 1)
	d.Body = &reworded
	_, err = b.Ingest(d)
	if err == nil {
		t.Fatal("a one-word rewording of a body made of one long line is kept whole by the measure, as the audit found; the measure changed")
	}
	for _, want := range []string{"drop 98%", "a line changed anywhere counts as dropped"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal lacks %q: %v", want, err)
		}
	}
	requireAskForAPatchFirst(t, err, saved.Page.ID, saved.Page.Revision, true) // a small edit inside a long line goes through the patch
}

// A save without an id finds its article by the title (the id is made from the first title). The refusal then does not say an id was
// sent, and tells a new lesson that happens to have that title to take another one (0.44.0 audit C-2). With an id the refusal is the
// one about the id and does not tell it to take another title.
func TestTheBodyLossRefusalOfASaveWithoutAnIdDoesNotClaimAnIdWasSent(t *testing.T) {
	short := "A different topic that happens to share the title."
	b, old, d := lossBook(t)
	d.Body = &short
	_, err := b.Ingest(d) // with the id of the article
	if err == nil {
		t.Fatal("a short body under the id of an article was accepted")
	}
	requireAskForAPatchFirst(t, err, old.ID, old.Revision, true)

	d.ID = ""
	_, err = b.Ingest(d) // the same title, no id
	if err == nil {
		t.Fatal("a short body under the first title of an article was accepted")
	}
	requireAskForAPatchFirst(t, err, old.ID, old.Revision, false)
	requireArticle(t, b, old.ID, old.Revision, lossBody)
}

// (b) Sending the whole previous body with an addition is how an id save adds to an article.
func TestIngestAcceptsThePreviousBodyWithAnAddition(t *testing.T) {
	b, old, d := lossBook(t)
	added := lossBody + "\n- A new line the agent adds.\n"
	d.Body = &added
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatalf("the previous body with an addition is refused: %v", err)
	}
	if saved.Page.Previous != old.Revision || !strings.Contains(saved.Page.Body, "A new line the agent adds.") || !strings.Contains(saved.Page.Body, "Shortening goes through wiki patch.") {
		t.Fatalf("the addition did not keep the old body: %+v", saved.Page)
	}
}

// (c) A small rewording keeps the other lines of the body, so more than half of it stays.
func TestIngestAcceptsASmallRewording(t *testing.T) {
	b, old, d := lossBook(t)
	reworded := strings.Replace(lossBody, "- Shortening goes through wiki patch.", "- Shortening is done with wiki patch.", 1)
	d.Body = &reworded
	saved, err := b.Ingest(d)
	if err != nil || saved.Page.Previous != old.Revision || !strings.Contains(saved.Page.Body, "is done with wiki patch") {
		t.Fatalf("a rewording of one line is refused: %v", err)
	}
}

// Where the line falls: losing half of the bytes or more is refused (exactly half kept is refused), one byte over half kept is accepted.
func TestIngestBodyLossLineIsHalfOfTheBytes(t *testing.T) {
	for _, tc := range []struct {
		name, previous, next string
		refused              bool
	}{
		{"exactly half kept", "aaaa\nbbbb", "aaaa", true},
		{"one byte over half kept", "aaaaa\nbbbb", "aaaaa", false},
		{"one byte under half", "aaaa\nbbbbb", "aaaa", true},
		{"all kept", "aaaa\nbbbb", "xx aaaa yy bbbb zz", false},
		{"nothing kept", "aaaa\nbbbb", "cccc", true},
		{"a long kept line weighs more than a short lost one", "aaaaaaaa\nb", "aaaaaaaa", false},
		{"blank lines and indentation do not count", "aaaa\n\n   \n  bbbb  \n", "bbbb\naaaa", false},
		{"an empty previous body has nothing to lose", "", "anything", false},
		{"only blank lines before has nothing to lose", "\n \n", "anything", false},
	} {
		err := bodyLossRefusal("0123456789abcdef", false, "fedcba9876543210", tc.previous, tc.next)
		if (err != nil) != tc.refused {
			t.Errorf("%s: refused %v, want %v (%v)", tc.name, err != nil, tc.refused, err)
		}
	}
}

// An update that does not send the body, a save of a new article and a save into an article that has no body yet lose nothing.
func TestIngestAcceptsWhatLosesNothing(t *testing.T) {
	b, _, d := lossBook(t)
	d.Body = nil
	d.Lesson = "Check model startup instructions and keep the body."
	saved, err := b.Ingest(d)
	if err != nil || saved.Page.Body != strings.TrimSpace(lossBody) {
		t.Fatalf("an update without a body is refused or lost the body: %v", err)
	}
	other := draft(seed(t, b, 2), "A different article")
	short := "A new article may have any body."
	other.Body = &short
	if _, err := b.Ingest(other); err != nil {
		t.Fatalf("a new article is refused: %v", err)
	}
	bare := draft(seed(t, b, 3), "An article without a body")
	created, err := b.Ingest(bare)
	if err != nil {
		t.Fatal(err)
	}
	bare.ID = created.Page.ID
	empty := ""
	bare.Body = &empty
	if _, err := b.Ingest(bare); err != nil {
		t.Fatalf("clearing a body that is already empty is refused: %v", err)
	}
	bare.Body = &short
	if _, err := b.Ingest(bare); err != nil {
		t.Fatalf("giving a body to an article that has none is refused: %v", err)
	}
}

// (d) A patch shows what goes and may shorten a body as far as it wants; (e) so may the person's edit on the Knowledge page. The same
// short body is what Ingest refused above, so the difference is the entry and not the text.
func TestPatchAndTheKnowledgePageMayShortenABody(t *testing.T) {
	short := "Only the new sentence."
	b, old, d := lossBook(t)
	d.Body = &short
	if _, err := b.Ingest(d); err == nil {
		t.Fatal("Ingest accepted the short body this test then saves through the other entries")
	}
	patched, err := b.Patch(Patch{ID: old.ID, Revision: old.Revision, Sources: d.Sources, ChangeReason: "shorten the body", Edits: []TextEdit{{Field: "body", Before: strings.TrimSpace(lossBody), After: short}}})
	if err != nil || patched.Page.Body != short {
		t.Fatalf("a patch that shortens a body to a sentence is refused: %v", err)
	}

	b, old, _ = lossBook(t)
	managed, err := b.Manage(Change{ID: old.ID, Revision: old.Revision, Action: "edit", Title: old.Title, Lesson: old.Lesson, AppliesTo: old.AppliesTo, Body: short, Kind: old.Kind, Scene: old.Scene})
	if err != nil || managed.Body != short {
		t.Fatalf("the person's edit that shortens a body is refused: %v", err)
	}
}

// The refusal sits in the one save the agents call, and the page of an article is put together and written by a few functions only. Every
// other use of them must be on a list: a caller that is added without a decision is a new way to wipe out an article that nothing refuses.
// pageWriterCallers lists, for each method, the functions that may use it (a call, or the method taken as a value) and why.
//
// What is watched is where an article's body is built into a page and where the page is written out, not only ingest: a function that
// calls buildIngestPage and publishIngest itself, or renders the page with pageBytes and writes it, replaces a body without passing the
// guard that Ingest sets. The page file comes only from renderPage (pageBytes is its form for writing); writeFile is not watched, since
// it also writes the index, the log and every other file of the store, none of which holds a body.
//
//   - ingest: Ingest sets the guard; Manage (the person's edit), patch (the revision-checked edit that shows what changes) and Organize
//     (which previews new articles only) call it without the guard.
//   - buildIngestPage, publishIngest: only ingest, which runs them in that order and, for a refusal, stops between them (nothing is written).
//   - pageBytes: storablePage (checks that a page reads back as it is, writes nothing), publishIngest (writes the page) and
//     RewriteWithoutKindScene (the test-only rewrite that drops kind and scene and keeps the body).
//   - renderPage: pageBytes, and matchesPage (compares a file with what would be written, writes nothing).
var pageWriterCallers = map[string][]string{
	"ingest":          {"Ingest", "Manage", "Organize", "patch"},
	"buildIngestPage": {"ingest"},
	"publishIngest":   {"ingest"},
	"pageBytes":       {"storablePage", "publishIngest", "RewriteWithoutKindScene"},
	"renderPage":      {"pageBytes", "matchesPage"},
}

// callerOutsideAFunction names the uses of a watched method that sit outside any function (a package-level variable holding a method value).
const callerOutsideAFunction = "(package level)"

func TestOnlyTheListedFunctionsCallIngest(t *testing.T) {
	calls := pageWriterCalls(t, ".")
	if problems := unlistedPageWriterCalls(calls); len(problems) > 0 {
		t.Errorf("these use the functions that build or write an article's page without being on the list; the save an agent calls must set the guard in Ingest, so list each one in pageWriterCallers with the reason it may skip the guard: %v", problems)
	}
	for method, allowed := range pageWriterCallers {
		for _, want := range allowed {
			if calls[method][want] == 0 {
				t.Errorf("%s is on the list for %s but no longer uses it: remove it from the list", want, method)
			}
		}
	}
	// Negative examples: a function that is not on the list is found for each way of reaching the page (the call of ingest, the direct
	// build and publish the audit found without the guard, the page rendered and written by hand, a method value held in a variable).
	dir := t.TempDir()
	src := "package notebook\n\n" +
		"func (b Book) Sneaky(d Draft) { b.ingest(d, true) }\n\n" +
		"func (b Book) SneakyDirectPublish(d Draft) {\n\tpage, _ := b.buildIngestPage(d, \"\", \"\", nil, nil, nil)\n\tb.publishIngest(\"\", \"\", d, Result{Page: page}, nil, nil)\n}\n\n" +
		"func (b Book) SneakyRender(p Page) { data, _ := b.pageBytes(p); writeFile(\"page.md\", data) }\n\n" +
		"var sneakyHeld = Book.renderPage\n"
	if err := os.WriteFile(filepath.Join(dir, "sneaky.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"SneakyDirectPublish uses buildIngestPage",
		"SneakyDirectPublish uses publishIngest",
		"Sneaky uses ingest",
		"SneakyRender uses pageBytes",
		callerOutsideAFunction + " uses renderPage",
	}
	slices.Sort(want)
	if got := unlistedPageWriterCalls(pageWriterCalls(t, dir)); !slices.Equal(got, want) {
		t.Fatalf("the scan does not find every use of the page writers in functions that are not on the list:\n got  %v\n want %v", got, want)
	}
}

// unlistedPageWriterCalls returns "<caller> uses <method>" (sorted) for each use of a watched method by a function that is not on its list.
func unlistedPageWriterCalls(calls map[string]map[string]int) []string {
	var out []string
	for method, callers := range calls {
		for caller := range callers {
			if !slices.Contains(pageWriterCallers[method], caller) {
				out = append(out, caller+" uses "+method)
			}
		}
	}
	slices.Sort(out)
	return out
}

// pageWriterCalls counts, per watched method and per function, the uses of that method in the non-test files of a directory, including
// the uses that sit outside any function.
func pageWriterCalls(t *testing.T, dir string) map[string]map[string]int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]int{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			caller := callerOutsideAFunction
			if fn, ok := decl.(*ast.FuncDecl); ok {
				caller = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if _, watched := pageWriterCallers[sel.Sel.Name]; watched {
						if out[sel.Sel.Name] == nil {
							out[sel.Sel.Name] = map[string]int{}
						}
						out[sel.Sel.Name][caller]++
					}
				}
				return true
			})
		}
	}
	return out
}
