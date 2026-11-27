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
		err := bodyLossRefusal("0123456789abcdef", tc.previous, tc.next)
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

// The refusal sits in the one save the agents call. Every other call of ingest must be on a list: a caller that is added without a
// decision is a new way to wipe out an article that nothing refuses. Ingest sets the guard; Manage (the person's edit), patch and
// Organize (which previews new articles only) call ingest without it.
func TestOnlyTheListedFunctionsCallIngest(t *testing.T) {
	allowed := []string{"Ingest", "Manage", "Organize", "patch"}
	callers := ingestCallers(t, ".")
	for caller := range callers {
		if !slices.Contains(allowed, caller) {
			t.Errorf("%s calls ingest; the save an agent calls must set the guard in Ingest, so list it here with the reason it may skip the guard", caller)
		}
	}
	for _, want := range allowed {
		if callers[want] == 0 {
			t.Errorf("%s is on the list but no longer calls ingest: remove it from the list", want)
		}
	}
	// A negative example: a function outside the list that calls ingest is found.
	dir := t.TempDir()
	src := "package notebook\n\nfunc (b Book) Sneaky(d Draft) { b.ingest(d, true) }\n"
	if err := os.WriteFile(filepath.Join(dir, "sneaky.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if found := ingestCallers(t, dir); found["Sneaky"] != 1 {
		t.Fatalf("the scan does not find a call of ingest in a function that is not on the list: %v", found)
	}
}

// ingestCallers counts, per function, the uses of a method named ingest (a call, or the method taken as a value) in the non-test files of a directory.
func ingestCallers(t *testing.T, dir string) map[string]int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
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
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "ingest" {
					out[fn.Name.Name]++
				}
				return true
			})
		}
	}
	return out
}
