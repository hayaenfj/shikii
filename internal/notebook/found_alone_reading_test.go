package notebook

import (
	"encoding/json"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// readingBook is five articles; of the words of readingQuery only "lanterns" is in one of them, and no article holds two of the
// words, so the search of the whole finds nothing and "lanterns" alone finds an article.
func readingBook(t *testing.T) Book {
	t.Helper()
	return foundAloneBook(t,
		[3]string{"Quartz export workflow", "Check Quartz exports with the raw parser.", "Quartz exports"},
		[3]string{"Saffron lantern notes", "Light the saffron lanterns after the audit.", "lanterns"},
		[3]string{"Juniper audit", "The audit of the juniper meadow records.", "audit"},
		[3]string{"Release checklist", "Update the CHANGELOG before the release.", "release"},
		[3]string{"Marigold canal", "The marigold canal was drained.", "canal"},
	)
}

const (
	readingQuery    = "alphaword lanterns betaword gammaword"
	readingArticles = 5
)

// readCounts is how many articles were parsed (a read of the wiki parses each once) and how many were split into terms since before.
type readCounts struct{ parses, splits uint64 }

func countsSince(parses, splits uint64) readCounts {
	return readCounts{PageParses() - parses, pageTokenizations.Load() - splits}
}

// A search of words that found nothing and the words of it that find an article alone read the wiki once between them: the second
// adds no parse of an article and no split of its texts. The counts are the measure, so this does not depend on how fast a machine is.
// The controls are the same question asked without the view of the search, which reads and splits every article again, and that the
// counts do count (a search alone parses and splits each article).
func TestWordsFoundAloneAfterTheSearchReadsNothingMore(t *testing.T) {
	b := readingBook(t)

	parses, splits := PageParses(), pageTokenizations.Load()
	view, err := b.QueryPage(readingQuery, "")
	if err != nil || view.Search == nil || view.Search.Total != 0 {
		t.Fatalf("control: the search of the words finds nothing: %+v %v", view.Search, err)
	}
	search := countsSince(parses, splits)
	if search.parses < readingArticles || search.splits != readingArticles {
		t.Fatalf("control: the search reads each of the %d articles and splits it once: %+v", readingArticles, search)
	}

	parses, splits = PageParses(), pageTokenizations.Load()
	words, err := b.WordsFoundAlone(readingQuery, "", view)
	if err != nil || !slices.Equal(words, []string{"lanterns"}) {
		t.Fatalf("the word that finds an article alone: %q %v", words, err)
	}
	if after := countsSince(parses, splits); after != (readCounts{}) {
		t.Fatalf("after the search, the words searched alone read nothing more of the wiki: %+v", after)
	}

	parses, splits = PageParses(), pageTokenizations.Load()
	again, err := b.WordsFoundAlone(readingQuery, "", emptyView())
	if err != nil || !slices.Equal(again, words) {
		t.Fatalf("a view without the reading names the same words: %q %v", again, err)
	}
	if fresh := countsSince(parses, splits); fresh != (readCounts{readingArticles, readingArticles}) {
		t.Fatalf("control: without the reading each of the %d articles is parsed and split again: %+v", readingArticles, fresh)
	}
}

// The reading belongs to the query and the project it was made for. A view of another query, or of another project, is not used: the
// wiki is read again and the answer is that of the words asked, over the wiki asked.
func TestWordsFoundAloneUsesOnlyTheReadingOfItsOwnSearch(t *testing.T) {
	b := readingBook(t)
	other, err := b.QueryPage("deltaword epsilonword", "")
	if err != nil || other.Search == nil || other.Search.Total != 0 {
		t.Fatalf("control: the other search finds nothing: %+v %v", other.Search, err)
	}
	parses, splits := PageParses(), pageTokenizations.Load()
	words, err := b.WordsFoundAlone(readingQuery, "", other)
	if err != nil || !slices.Equal(words, []string{"lanterns"}) {
		t.Fatalf("the view of another query does not answer for this one: %q %v", words, err)
	}
	if got := countsSince(parses, splits); got != (readCounts{readingArticles, readingArticles}) {
		t.Fatalf("it reads the wiki for the words asked: %+v", got)
	}

	view, err := b.QueryPage(readingQuery, "")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := foundAloneBook(t, [3]string{"Juniper audit", "The audit of the juniper meadow records.", "audit"})
	words, err = elsewhere.WordsFoundAlone(readingQuery, "", view)
	if err != nil || len(words) != 0 {
		t.Fatalf("the view of the same search in another project does not answer for this one, whose articles hold none of the words: %q %v", words, err)
	}
}

// The reading belongs to the scope it was made under, as it does to the query and the project: a view made under one scope is not used
// for the same project (the same root) asked under another, which is read again and answers for the words asked.
func TestWordsFoundAloneUsesNoReadingOfAnotherScope(t *testing.T) {
	b := readingBook(t)
	view, err := b.QueryPage(readingQuery, "")
	if err != nil || view.reading == nil || view.reading.scope != b.Scope {
		t.Fatalf("control: the search that found nothing keeps its reading, under its own scope: %v", err)
	}
	parses, splits := PageParses(), pageTokenizations.Load()
	words, err := b.WordsFoundAlone(readingQuery, "", view)
	if err != nil || !slices.Equal(words, []string{"lanterns"}) {
		t.Fatalf("control: under its own scope the reading answers: %q %v", words, err)
	}
	if got := countsSince(parses, splits); got != (readCounts{}) {
		t.Fatalf("control: under its own scope the reading is used and nothing is read: %+v", got)
	}

	other := b
	other.Scope = b.Scope + "/another"
	parses, splits = PageParses(), pageTokenizations.Load()
	words, err = other.WordsFoundAlone(readingQuery, "", view)
	if err != nil || !slices.Equal(words, []string{"lanterns"}) {
		t.Fatalf("under another scope the answer is that of the words asked: %q %v", words, err)
	}
	if got := countsSince(parses, splits); got != (readCounts{readingArticles, readingArticles}) {
		t.Fatalf("under another scope the reading of the first is not used: the wiki is read again: %+v", got)
	}
}

// When a word asked has a term that the texts were not split for, they are split again for the words (in memory: no article is read).
func TestWordsFoundAloneSplitsAgainWhatTheReadingDidNotSplitFor(t *testing.T) {
	b := readingBook(t)
	view, err := b.QueryPage(readingQuery, "")
	if err != nil || view.reading == nil {
		t.Fatalf("control: the search that found nothing keeps its reading: %v", err)
	}
	// a reading split for other words than the query (as a reading of another form of the query would be)
	view.reading.searcher.terms = splitPages(view.reading.searcher.lessons, []string{"unrelated"})
	parses, splits := PageParses(), pageTokenizations.Load()
	words, err := b.WordsFoundAlone(readingQuery, "", view)
	if err != nil || !slices.Equal(words, []string{"lanterns"}) {
		t.Fatalf("the word that finds an article alone: %q %v", words, err)
	}
	if got := countsSince(parses, splits); got != (readCounts{0, readingArticles}) {
		t.Fatalf("the texts are split once more for all the words, and no article is read: %+v", got)
	}
}

// A search that finds an article does not keep its reading (nothing asks about its words alone), and the ranking over the texts split
// once for the query is the ranking that splits them for itself: the same candidates, matches and order for a seeded mix of articles and
// queries, those that find nothing among them.
func TestSearchOverTheSplitTextsRanksAsTheSearchThatSplitsForItself(t *testing.T) {
	rng := rand.New(rand.NewSource(20261122))
	b, words := sharedSearchBook(t, rng)
	pool := words.all()
	kept, found := 0, 0
	for trial := 0; trial < 120; trial++ {
		typed := []string{}
		for i, n := 0, 1+rng.Intn(5); i < n; i++ {
			word := pool[rng.Intn(len(pool))]
			if rng.Intn(3) == 0 {
				word = words.absent[rng.Intn(len(words.absent))]
			}
			if rng.Intn(3) == 0 {
				word = strings.ToUpper(word)
			}
			typed = append(typed, word)
		}
		query := strings.Join(typed, " ")
		got, err := b.ReadView().scopedRead().query(query, false)
		if err != nil {
			t.Fatal(err)
		}
		pages, _, err := b.ReadView().scopedRead().searchPages(query, false)
		if err != nil {
			t.Fatal(err)
		}
		ranked, matches := rankPages(pages.Lessons, query, false) // splits the texts of each article for itself
		candidates := []SearchCandidate{}
		for i, p := range ranked {
			candidates = append(candidates, SearchCandidate{Match: matches[i], Title: p.Title, AppliesTo: p.AppliesTo})
		}
		shown := min(len(ranked), 2)
		want := rankingAsJSON(t, []any{ranked[:shown], matches[:shown], len(ranked), candidates})
		if reply := rankingAsJSON(t, []any{got.Lessons, got.Matches, got.Search.Total, got.Search.Candidates}); reply != want {
			t.Fatalf("%q: the search over the shared split ranks otherwise\n got %.400s\nwant %.400s", query, reply, want)
		}
		if got.reading != nil {
			kept++
			if len(got.Lessons) != 0 {
				t.Fatalf("%q: a search that found articles keeps no reading", query)
			}
		} else {
			found++
		}
	}
	if kept < 10 || found < 10 {
		t.Fatalf("the draw has searches that find nothing (%d) and searches that find articles (%d)", kept, found)
	}
}

func rankingAsJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
