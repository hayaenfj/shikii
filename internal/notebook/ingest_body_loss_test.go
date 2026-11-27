package notebook

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
	for _, want := range []string{"drop 98%", "changing even part of a line counts that whole line as dropped", "small edit inside a long line with patch"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal lacks %q: %v", want, err)
		}
	}
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
	for _, want := range []string{"Saving with an id replaces the whole body", "shikii wiki patch", "shikii wiki query lesson:" + old.ID} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal of a save with an id lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "another title") {
		t.Fatalf("a save with an id is told to take another title: %v", err)
	}

	d.ID = ""
	_, err = b.Ingest(d) // the same title, no id
	if err == nil {
		t.Fatal("a short body under the first title of an article was accepted")
	}
	for _, want := range []string{old.ID, "without an id", "first title", "give it another title", "shikii wiki patch", "shikii wiki query lesson:" + old.ID} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal of a save without an id lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Saving with an id") {
		t.Fatalf("the refusal of a save without an id says an id was sent: %v", err)
	}
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
		err := bodyLossRefusal("0123456789abcdef", false, tc.previous, tc.next)
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
