package notebook

import (
	"encoding/json"
	"testing"
)

// A search reads the wiki in two steps: the ranking, and the list of the articles that still lack a kind and a scene that the first
// reply of a query carries. The steps share the parsing of the articles, so one search parses each article once. The count of parses
// is the measure, so this does not depend on how fast a machine is.
//
// The searches are those of every form a query takes (words that find an article, words that find nothing, no words, a direct
// reference), and the listing of Query alone, which also reads the wiki for the observations and the maintenance it carries. The
// first reply carries one article without a kind and a scene, so the step under test has something to list and the count is not low
// for lack of work; and the reply of the shared reading is the reply of reading each step for itself (a fresh Book that shares nothing).
func TestOneSearchParsesEachArticleOnce(t *testing.T) {
	b := readingBook(t)
	live, err := b.Live()
	if err != nil || len(live) != readingArticles {
		t.Fatalf("control: the wiki holds %d articles: %d %v", readingArticles, len(live), err)
	}
	legacy, direct := "", ""
	for _, p := range live {
		switch p.Title {
		case "Marigold canal":
			legacy = p.ID
		case "Saffron lantern notes":
			direct = p.ID
		}
	}
	if err := b.RewriteWithoutKindScene(legacy); err != nil {
		t.Fatal(err)
	}
	alone := Book{Root: b.Root, Scope: b.Scope} // shares no reading with the searches

	for _, c := range []struct {
		name, query string
		found       bool // the search ranks an article
	}{
		{"words that find an article", "lanterns", true},
		{"words that find nothing", readingQuery, false},
		{"no words", "", true},
		{"a direct reference", "lesson:" + direct, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := PageParses()
			view, err := b.QueryPage(c.query, "")
			if err != nil {
				t.Fatal(err)
			}
			if parses := PageParses() - before; parses != readingArticles {
				t.Fatalf("a search of %q parses each of the %d articles once, got %d passes", c.query, readingArticles, parses)
			}
			if found := len(view.Lessons) > 0; found != c.found {
				t.Fatalf("control: the search of %q returns articles: %v", c.query, found)
			}
			if view.UnclassifiedCount != 1 || len(view.Unclassified) != 1 || view.Unclassified[0].ID != legacy {
				t.Fatalf("control: the reply lists the article without a kind and a scene: %+v", view.Remaining)
			}
			want, err := alone.Query(c.query)
			if err != nil {
				t.Fatal(err)
			}
			want.Remaining = alone.RemainingUnclassified()
			if got, wantJSON := fullReply(t, view), fullReply(t, want); got != wantJSON {
				t.Fatalf("the reply of the shared reading differs from reading each step for itself\n got %.400s\nwant %.400s", got, wantJSON)
			}
		})
	}

	before := PageParses()
	if _, err := b.Query(""); err != nil {
		t.Fatal(err)
	}
	if parses := PageParses() - before; parses != readingArticles {
		t.Fatalf("the listing of Query alone (with its observations and maintenance) parses each of the %d articles once, got %d passes", readingArticles, parses)
	}
}

// The shared parsing ends with the search (it is not a cache of the process): two searches in a row parse the articles twice, and an
// article classified between them is seen as classified by the second.
func TestTheParsingASearchSharesEndsWithTheSearch(t *testing.T) {
	b := readingBook(t)
	live, err := b.Live()
	if err != nil || len(live) != readingArticles {
		t.Fatalf("control: the wiki holds %d articles: %d %v", readingArticles, len(live), err)
	}
	if err := b.RewriteWithoutKindScene(live[0].ID); err != nil {
		t.Fatal(err)
	}
	first, err := b.QueryPage("lanterns", "")
	if err != nil || first.UnclassifiedCount != 1 {
		t.Fatalf("control: the first search lists the unclassified article: %+v %v", first.Remaining, err)
	}
	if _, err := b.Classify(live[0].ID, "howto", "reading notes"); err != nil {
		t.Fatal(err)
	}
	before := PageParses()
	second, err := b.QueryPage("lanterns", "")
	if err != nil || second.UnclassifiedCount != 0 {
		t.Fatalf("the second search sees the article classified between the two: %+v %v", second.Remaining, err)
	}
	if parses := PageParses() - before; parses != readingArticles {
		t.Fatalf("the second search parses each of the %d articles once itself, got %d passes", readingArticles, parses)
	}
}

// fullReply is the reply as it is sent, without the cursor of a page, whose position can differ between two readings of the same wiki.
func fullReply(t *testing.T, v View) string {
	t.Helper()
	v.NextCursor = ""
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
