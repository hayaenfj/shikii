package notebook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The revision of an article is made from the whole article, the hash and the text_hash of each source included. That is why a
// search or a list can name a source by its id alone: the article's id with its revision already fixes the content of every
// source. If the revision stopped covering them, the hashes would be the only place that says a source changed.
func TestAnArticleRevisionCoversTheHashesOfItsSources(t *testing.T) {
	hex := func(c string) string { return strings.Repeat(c, 64) }
	article := func(hash, text string) Page {
		return Page{ID: "0000000000000001", Title: "Title", Lesson: "Lesson", AppliesTo: "Condition", Status: "hypothesis", Sources: []Source{{ID: "0000000000000002", Hash: hash, TextHash: text}}}
	}
	base := revision(article(hex("a"), hex("b")))
	if revision(article(hex("a"), hex("b"))) != base {
		t.Fatal("the revision of the same article is the same")
	}
	for name, changed := range map[string]Page{"the hash of a source": article(hex("c"), hex("b")), "the text_hash of a source": article(hex("a"), hex("c")), "a source without a text_hash": article(hex("a"), "")} {
		if revision(changed) == base {
			t.Errorf("the revision changes with %s", name)
		}
	}
}

// savedView is a project with an article that has everything a lesson of a list can carry, and a second that cites the first.
func savedView(t *testing.T) (Book, View) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: t.TempDir()}
	if err := os.WriteFile(filepath.Join(b.Scope, "plain.md"), []byte("plain\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := b.CaptureObserved(Observation{Kind: "test", Title: "First check", Content: "The first check passed.", Reference: "check one", Version: "1", ObservedAt: time.Unix(1700000000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.CaptureObserved(Observation{Kind: "test", Title: "Second check", Content: "The second check passed.", Reference: "check two", Version: "1", ObservedAt: time.Unix(1700000100, 0)})
	if err != nil {
		t.Fatal(err)
	}
	body, rationale, watch := "## Steps\nRun both checks.", "Both checks were run and passed.", []string{"plain.md"}
	one, err := b.Ingest(Draft{Sources: []string{first.ID, second.ID}, Title: "Both checks", Lesson: "Run both checks before saying done.", AppliesTo: "finishing work", Kind: "rule", Scene: "when finishing", Body: &body, Rationale: &rationale, Watch: &watch})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(Draft{Sources: []string{first.ID}, Title: "First check only", Lesson: "Run the first check when finishing.", AppliesTo: "finishing work", Kind: "howto", Scene: "when finishing", Related: []string{one.Page.ID}}); err != nil {
		t.Fatal(err)
	}
	view, err := b.QueryPage("", "")
	if err != nil || len(view.Lessons) != 2 {
		t.Fatal(view, err)
	}
	return b, view
}

func jsonTree(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	return tree
}

// A listed view is the view with each source of each lesson reduced to its id: nothing else in it changes (a field of an article
// added later is carried along without a change here), and the hashes the articles hold for their watched files stay.
func TestAListedViewIsTheViewWithoutTheHashesOfItsSources(t *testing.T) {
	b, view := savedView(t)
	words, err := b.QueryPage("both checks", "")
	if err != nil || len(words.Lessons) == 0 || words.Search == nil {
		t.Fatal(words, err)
	}
	for name, v := range map[string]View{"list": view, "words": words} {
		plain := jsonTree(t, v)
		listed := jsonTree(t, v.WithSourceIDsOnly())
		sawHash := false
		for _, lesson := range plain["lessons"].([]any) {
			for _, source := range lesson.(map[string]any)["sources"].([]any) {
				ref := source.(map[string]any)
				sawHash = sawHash || ref["hash"] != nil
				delete(ref, "hash")
				delete(ref, "text_hash")
			}
		}
		if !sawHash {
			t.Fatalf("%s: the fixture changed: no source with a hash was listed", name)
		}
		if !reflect.DeepEqual(plain, listed) {
			t.Errorf("%s: the listed view is the plain view without the hash and text_hash of its sources:\nplain:  %v\nlisted: %v", name, plain, listed)
		}
		for _, lesson := range listed["lessons"].([]any) {
			for _, source := range lesson.(map[string]any)["sources"].([]any) {
				if ref := source.(map[string]any); len(ref) != 1 || ref["id"] == nil {
					t.Errorf("%s: a listed source is its id alone: %v", name, ref)
				}
			}
		}
	}
	// the hashes of a watched file are not the hashes of a source
	listed := jsonTree(t, view.WithSourceIDsOnly())
	watched := 0
	for _, lesson := range listed["lessons"].([]any) {
		premises, _ := lesson.(map[string]any)["premises"].([]any)
		for _, premise := range premises {
			if premise.(map[string]any)["hash"] == nil {
				t.Errorf("a listed article keeps the hash of its watched file: %v", premise)
			}
			watched++
		}
	}
	if watched != 1 {
		t.Errorf("the fixture changed: %d watched files listed", watched)
	}
	// the view itself is not changed by listing it: a caller that reads the hashes of its lessons still finds them
	if view.Lessons[0].Sources[0].Hash == "" || view.WithSourceIDsOnly().Lessons[0].Sources[0].Hash != view.Lessons[0].Sources[0].Hash {
		t.Error("the sources of the lessons of a listed view keep their hashes in memory")
	}
}

// What is not a source stays as it was: a view with nothing in it, a lesson with no list of sources, and one with an empty list.
func TestAListedViewKeepsAbsentAndEmptyLists(t *testing.T) {
	type plain View
	for name, v := range map[string]View{
		"nothing":              {},
		"no lessons":           {Lessons: []Page{}},
		"no sources":           {Lessons: []Page{{ID: "0000000000000001"}}},
		"an empty source list": {Lessons: []Page{{ID: "0000000000000001", Sources: []Source{}}}},
	} {
		want, err := json.Marshal(plain(v))
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := json.Marshal(v); string(got) != string(want) {
			t.Errorf("%s: a view that is not listed is written as it was:\n%s\n%s", name, got, want)
		}
		// the members come in another order (the reply is written in reading order by size, whatever order they are marshaled in)
		if got, unlisted := jsonTree(t, v.WithSourceIDsOnly()), jsonTree(t, v); !reflect.DeepEqual(got, unlisted) {
			t.Errorf("%s: nothing to reduce, nothing changes (an absent list stays absent, an empty one stays empty):\n%v\n%v", name, got, unlisted)
		}
	}
}

// A lesson named by its id is read whole; anything else is a search or a list.
func TestOnlyAQueryNamingAnArticleIsANamedRead(t *testing.T) {
	b, view := savedView(t)
	id := view.Lessons[0].ID
	for query, want := range map[string]bool{"lesson:" + id: true, "lesson:" + id + "@" + view.Lessons[0].Revision: true, "": false, "both checks": false, "lessons": false, " lesson:" + id: false, id: false} {
		if got := IsNamedRead(query); got != want {
			t.Errorf("%q: named read %v, want %v", query, got, want)
		}
		if want {
			if v, err := b.Query(query); err != nil || len(v.Lessons) != 1 || v.Lessons[0].ID != id {
				t.Errorf("%q reads the article it names: %v %v", query, v.Lessons, err)
			}
		}
	}
}
