package notebook

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// foundAloneBook is a project whose articles are the given (title, lesson, applies-to) triples, all citing one source.
func foundAloneBook(t *testing.T, articles ...[3]string) Book {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/work/found-alone"}
	o, err := b.capture(Observation{Kind: "test", Title: "Synthetic source", Content: "A source for the articles.", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range articles {
		if _, err := b.Ingest(Draft{Title: a[0], Lesson: a[1], AppliesTo: a[2], Kind: "howto", Scene: "reading notes", Sources: []string{o.ID}}); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

// aloneOf asks as the callers do: the search of the query as a wiki query runs it, then the words of it that find an article alone.
func aloneOf(t *testing.T, b Book, query string) []string {
	t.Helper()
	view, err := b.QueryPage(query, "")
	if err != nil {
		t.Fatal(err)
	}
	words, err := b.WordsFoundAlone(query, "", view)
	if err != nil {
		t.Fatal(err)
	}
	return words
}

// foundByTheWikiQuery is whether a wiki query of the words returns a candidate (the real entrance, with no journal written).
func foundByTheWikiQuery(t *testing.T, b Book, words string) bool {
	t.Helper()
	view, err := b.QueryPage(words, "")
	if err != nil {
		t.Fatal(err)
	}
	return view.Search != nil && view.Search.Total > 0
}

// The case that was seen with a real agent: five words put in as alternatives, of which only one is in the article. A search of
// several words needs two of them in one article, so the whole finds nothing; the search says that the one word finds the article.
func TestWordsFoundAloneNamesTheOneWordOfAFiveWordSearchThatFindsTheArticle(t *testing.T) {
	b := foundAloneBook(t, [3]string{"\u8acb\u6c42\u306e\u671f\u9650\u3068\u9001\u4ed8", "\u8acb\u6c42\u66f8\u306f\u671f\u9650\u307e\u3067\u306b\u9001\u4ed8\u3059\u308b\u3002", "\u8acb\u6c42\u66f8\u306e\u51e6\u7406"})
	query := "\u7de0\u3081\u65e5 \u7de0\u65e5 \u8acb\u6c42 invoice \u6848\u4ef6"
	if view, err := b.QueryPage(query, ""); err != nil || view.Search == nil || view.Search.Total != 0 {
		t.Fatalf("control: the search of the five words finds nothing: %+v %v", view.Search, err)
	}
	if got, want := aloneOf(t, b, query), []string{"\u8acb\u6c42"}; !slices.Equal(got, want) {
		t.Fatalf("the one word that finds the article alone: %q, want %q", got, want)
	}
	if !foundByTheWikiQuery(t, b, "\u8acb\u6c42") {
		t.Fatal("control: that word finds the article through the wiki query")
	}
}

// Each word it returns finds an article when a wiki query searches it alone, and each word it does not return does not (the same
// search, so the same answer): a word in two articles, a word in none, the same word twice in another case, an identifier, a word
// that is only a part of a word in an article, and two words that each find an article alone but not together.
func TestWordsFoundAloneAgreesWithTheWikiQueryOfEachWord(t *testing.T) {
	b := foundAloneBook(t,
		[3]string{"Quartz export workflow", "Check Quartz exports with the raw parser.", "Quartz exports"},
		[3]string{"Saffron lantern notes", "Light the saffron lanterns after the audit.", "lanterns"},
		[3]string{"Juniper audit", "The audit of the juniper meadow records.", "audit"},
		[3]string{"Release checklist", "Update the CHANGELOG before the release.", "release"},
		[3]string{"\u8acb\u6c42\u306e\u671f\u9650\u3068\u9001\u4ed8", "\u8acb\u6c42\u66f8\u306f\u671f\u9650\u307e\u3067\u306b\u9001\u4ed8\u3059\u308b\u3002", "\u8acb\u6c42\u66f8\u306e\u51e6\u7406"},
	)
	for _, c := range []struct {
		name, query string
		want        []string
	}{
		{"one word of three", "kestrel quartz harbor", []string{"quartz"}},
		{"no word finds an article", "kestrel harbor", nil},
		{"a word in two articles", "audit kestrel", []string{"audit"}},
		{"the same word twice in another case", "QUARTZ kestrel quartz", []string{"QUARTZ"}},
		{"an identifier", "kestrel CHANGELOG", []string{"CHANGELOG"}},
		{"a part of a word", "kestrel lanter", nil},
		{"two words that find an article alone and not together", "saffron kestrel juniper", []string{"saffron", "juniper"}},
		{"a Japanese word of two terms of which the article has only one", "kestrel \u8acb\u6c42\u984d", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if foundByTheWikiQuery(t, b, c.query) {
				t.Fatalf("control: the search of %q finds an article, so the words alone are not asked", c.query)
			}
			got := aloneOf(t, b, c.query)
			if !slices.Equal(got, c.want) {
				t.Fatalf("%q: %q, want %q", c.query, got, c.want)
			}
			seen := map[string]bool{}
			for _, word := range strings.Fields(c.query) {
				if seen[strings.ToLower(word)] {
					continue
				}
				seen[strings.ToLower(word)] = true
				listed := slices.ContainsFunc(got, func(g string) bool { return strings.EqualFold(g, word) })
				if found := foundByTheWikiQuery(t, b, word); found != listed {
					t.Fatalf("the wiki query of %q finds an article: %v, and it is returned: %v", word, found, listed)
				}
			}
		})
	}
}

// Only the search of several words that found nothing is asked: one word, what is not a search of words (references, a continuation
// of a search), and a search that found an article return nothing. A word of the query that is a reference is not searched.
func TestWordsFoundAloneAnswersOnlyASearchOfSeveralWordsThatFoundNothing(t *testing.T) {
	b := foundAloneBook(t, [3]string{"Quartz export workflow", "Check Quartz exports with the raw parser.", "Quartz exports"})
	for _, query := range []string{"quartz", "kestrel", "lesson:0123456789abcdef quartz", "0123456789abcdef 0123456789abcdef", "quartz exports kestrel"} {
		if got := aloneOf(t, b, query); got != nil {
			t.Fatalf("%q: %q, want nothing", query, got)
		}
	}
	view, err := b.QueryPage("kestrel quartz", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.WordsFoundAlone("kestrel quartz", "bmV4dA", view); got != nil || err != nil {
		t.Fatalf("a continuation of a search is not asked: %q %v", got, err)
	}
	if got, err := b.WordsFoundAlone("kestrel quartz", "", view); !slices.Equal(got, []string{"quartz"}) || err != nil {
		t.Fatalf("control: the same query without a cursor is asked: %q %v", got, err)
	}
	if got := aloneOf(t, b, "kestrel 0123456789abcdef quartz"); !slices.Equal(got, []string{"quartz"}) {
		t.Fatalf("a word that is an id is not a search of words and is left out: %q", got)
	}
	if got, err := b.WordsFoundAlone("", "", View{}); got != nil || err != nil {
		t.Fatalf("no query: %q %v", got, err)
	}
}

// At most three words are returned, in the order of the query, and at most the first eight distinct words are searched.
func TestWordsFoundAloneReturnsAtMostThreeWordsAndSearchesAtMostEight(t *testing.T) {
	b := foundAloneBook(t,
		[3]string{"Alpha note", "Alpha is the first.", "alpha"},
		[3]string{"Bravo note", "Bravo is the second.", "bravo"},
		[3]string{"Charlie note", "Charlie is the third.", "charlie"},
		[3]string{"Delta note", "Delta is the fourth.", "delta"},
		[3]string{"Echo note", "Echo is the fifth.", "echo"},
	)
	if got := aloneOf(t, b, "alpha bravo charlie delta echo"); !slices.Equal(got, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("the first three in the order of the query: %q", got)
	}
	if got := aloneOf(t, b, "echo delta charlie bravo alpha"); !slices.Equal(got, []string{"echo", "delta", "charlie"}) {
		t.Fatalf("the order of the query, not the order of the articles: %q", got)
	}
	filler := "kestrel harbor meadow orchard valley ridge canyon"
	if got := aloneOf(t, b, filler+" alpha"); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("control: the eighth distinct word is searched: %q", got)
	}
	if got := aloneOf(t, b, filler+" Kestrel alpha"); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("a word twice, in another case, counts once: %q", got)
	}
	if got := aloneOf(t, b, filler+" quartz alpha"); got != nil {
		t.Fatalf("the ninth distinct word is not searched: %q", got)
	}
}

// Asking records nothing: not a lookup, not a miss, nothing in the journal, and nothing else in the project changes. A project that has
// no journal has none after it.
func TestWordsFoundAloneRecordsNothing(t *testing.T) {
	b := foundAloneBook(t,
		[3]string{"Quartz export workflow", "Check Quartz exports with the raw parser.", "Quartz exports"},
		[3]string{"Saffron lantern notes", "Light the saffron lanterns after the audit.", "lanterns"},
	)
	tree := func() map[string]string {
		t.Helper()
		files := map[string]string{}
		err := filepath.WalkDir(b.Root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			files[path] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	same := func(when string, before, after map[string]string) {
		t.Helper()
		if len(before) != len(after) {
			t.Fatalf("%s: the files of the project changed: %d before, %d after", when, len(before), len(after))
		}
		for path, data := range before {
			if got, ok := after[path]; !ok || got != data {
				t.Fatalf("%s: %s changed", when, path)
			}
		}
	}
	// the first read may write what it derives (an index); only what a call of ours changes after that is the question
	if _, err := b.Query("warm up"); err != nil {
		t.Fatal(err)
	}
	before := tree()
	for _, query := range []string{"kestrel quartz saffron", "kestrel harbor", "quartz kestrel"} {
		aloneOf(t, b, query)
	}
	same("no journal", before, tree())
	if _, err := os.Stat(filepath.Join(b.Root, "records", "wiki-searches")); !os.IsNotExist(err) {
		t.Fatalf("the call created the folder of the search journal: %v", err)
	}
	// a journal that holds a lookup and a miss
	if _, err := b.recordMissAt("kestrel harbor", "", "", emptyView(), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	view, err := b.QueryPage("quartz", "")
	if err != nil || len(view.Lessons) == 0 {
		t.Fatalf("the fixture changed: %v", err)
	}
	if _, err := b.RecordLookup("quartz", view.Lessons); err != nil {
		t.Fatal(err)
	}
	before = tree()
	for _, query := range []string{"kestrel quartz saffron", "kestrel harbor", "quartz kestrel"} {
		aloneOf(t, b, query)
	}
	same("a journal with a lookup and a miss", before, tree())
}
