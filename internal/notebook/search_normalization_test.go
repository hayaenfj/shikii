package notebook

import (
	"slices"
	"testing"
	"time"

	"golang.org/x/text/unicode/norm"
)

// searchForms are words in the two forms a text can take: composed (one code point for the letter with its mark) and decomposed (the
// letter, then a combining mark, as macOS file names give kana with a voiced mark). Written as escapes so that the source keeps the
// form it means.
var searchForms = []struct{ name, composed, decomposed string }{
	{"latin with an accent", "caf\u00e9", "cafe\u0301"},
	{"katakana with a voiced mark", "\u30ac\u30a4\u30c9", "\u30ab\u3099\u30a4\u30c8\u3099"},
	{"hiragana with a voiced mark", "\u3052\u304d", "\u3051\u3099\u304d"},
}

// addSearchPage saves one article whose title, lesson and applies-to hold the word, as the caller wrote it (the save does not
// normalize it).
func addSearchPage(t *testing.T, b Book, word string) string {
	t.Helper()
	o, err := b.capture(Observation{Kind: "test", Title: "Synthetic source", Content: "A synthetic observation for the search of text in two forms.", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.Ingest(Draft{Title: "Notes on " + word, Lesson: "Check " + word + " before the export.", AppliesTo: word + " export", Kind: "howto", Scene: "checking a word", Sources: []string{o.ID}})
	if err != nil {
		t.Fatal(err)
	}
	return r.Page.ID
}

// searchedIDs is what a word search finds: how many candidates it counts and the ids of the ones it lists.
func searchedIDs(t *testing.T, b Book, query string) (int, []string) {
	t.Helper()
	view, err := b.QueryPage(query, "")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, c := range view.Search.Candidates {
		ids = append(ids, c.ID)
	}
	return view.Search.Total, ids
}

// The search compares text composed (NFC), so that a query pasted in decomposed form finds the article written in composed form and
// the other way round. Before, the combining mark was neither a letter nor kana: it ended the word (cafe) or the kana pair, and the
// same words in the other form found nothing.
func TestSearchFindsTheSameWordsInComposedAndDecomposedForm(t *testing.T) {
	for _, c := range searchForms {
		t.Run(c.name, func(t *testing.T) {
			if c.composed == c.decomposed || norm.NFC.String(c.decomposed) != c.composed || norm.NFD.String(c.composed) != c.decomposed {
				t.Fatalf("the fixture: %q and %q are not the two forms of one word", c.composed, c.decomposed)
			}
			for _, saved := range []struct{ form, word string }{{"composed", c.composed}, {"decomposed", c.decomposed}} {
				b := Book{Root: t.TempDir(), Scope: "/work/search-forms"}
				id := addSearchPage(t, b, saved.word)
				wantTotal, wantIDs := searchedIDs(t, b, saved.word) // the same form as the article: the baseline
				if wantTotal != 1 || !slices.Equal(wantIDs, []string{id}) {
					t.Fatalf("the article saved %s is found by its own words: %d %v", saved.form, wantTotal, wantIDs)
				}
				for _, query := range []string{c.composed, c.decomposed} {
					if total, ids := searchedIDs(t, b, query); total != wantTotal || !slices.Equal(ids, wantIDs) {
						t.Errorf("an article saved %s, searched by %q: total %d %v, want %d %v", saved.form, query, total, ids, wantTotal, wantIDs)
					}
				}
			}
		})
	}
}

// Words that differ are still different words: with and without the accent are two terms, and an article about one is not found by
// the other, in whichever form either is written. (Before, a decomposed accent was dropped, so it was the same word as the plain one.)
func TestSearchKeepsWordsThatDifferApart(t *testing.T) {
	with, without := "caf\u00e9", "cafe"
	if a, b := searchTerms(with), searchTerms(without); !a[with] || !b[without] || a[without] || b[with] {
		t.Fatalf("the accent is part of the word: %v %v", a, b)
	}
	for _, c := range []struct{ name, saved, query string }{
		{"composed accent, plain query", with, without},
		{"plain article, composed accent query", without, with},
		{"decomposed accent, plain query", "cafe\u0301", without},
		{"plain article, decomposed accent query", without, "cafe\u0301"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/search-forms"}
			addSearchPage(t, b, c.saved)
			if total, ids := searchedIDs(t, b, c.query); total != 0 {
				t.Errorf("an article about %q is found by %q: %d %v", c.saved, c.query, total, ids)
			}
		})
	}
}

// The automatic delivery counts the runs that terms cover in the text of the request, by position: the request text that the terms
// come from and the one the runs are measured in are the same normalized string, or the runs do not line up with the terms and a
// request that delivers in one form delivers nothing in the other. The same case as TestRecallKeepsKatakanaLongVowel, with a voiced
// kana (the ba of the Japanese word for "server", written in katakana) in the request and in the title, in each form.
func TestRecallDeliversTheSameForAComposedAndADecomposedRequest(t *testing.T) {
	// The title is the katakana word for "server" followed by "no kidou" (start-up); the request is the same word followed by "wo
	// naoshite" (please fix). The decomposed forms write the ba as ha and U+3099.
	const (
		titleComposed     = "\u30b5\u30fc\u30d0\u30fc\u306e\u8d77\u52d5"
		titleDecomposed   = "\u30b5\u30fc\u30cf\u3099\u30fc\u306e\u8d77\u52d5"
		requestComposed   = "\u30b5\u30fc\u30d0\u30fc\u3092\u76f4\u3057\u3066"
		requestDecomposed = "\u30b5\u30fc\u30cf\u3099\u30fc\u3092\u76f4\u3057\u3066"
	)
	if norm.NFC.String(titleDecomposed) != titleComposed || norm.NFC.String(requestDecomposed) != requestComposed || titleComposed == titleDecomposed {
		t.Fatal("the fixture: the decomposed texts are not the decomposed forms of the composed ones")
	}
	for _, c := range []struct{ name, title, request string }{
		{"composed title, composed request", titleComposed, requestComposed},
		{"composed title, decomposed request", titleComposed, requestDecomposed},
		{"decomposed title, composed request", titleDecomposed, requestComposed},
		{"decomposed title, decomposed request", titleDecomposed, requestDecomposed},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/project"}
			source := seed(t, b, 1)
			for _, title := range []string{c.title, "\u753b\u9762\u306e\u8272", "\u8a18\u9332\u306e\u5f62"} {
				d := draft(source, title)
				d.Lesson, d.AppliesTo = "\u6c7a\u307e\u308a", "\u305d\u306e\u6642"
				if _, err := b.Ingest(d); err != nil {
					t.Fatal(err)
				}
			}
			if p, _ := b.Recall(c.request); len(p) != 1 || p[0].Title != c.title {
				t.Fatalf("the request %q delivers the page titled %q: %+v", c.request, c.title, p)
			}
		})
	}
}

// The same for the paths of the gate that count the runs the matched terms cover (the rate of rare words), where the word is in the
// applies-to condition and not in the title, so that no title path delivers it. The word has two voiced marks: written decomposed,
// none of its pairs is found in the request text unless the text the runs are measured in is composed too.
func TestRecallCountsTheRunsOfAComposedAndADecomposedRequestAlike(t *testing.T) {
	const (
		wordComposed   = "\u30ac\u30a4\u30c9"
		wordDecomposed = "\u30ab\u3099\u30a4\u30c8\u3099"
		askComposed    = "\u3092\u76f4\u3057\u3066"
	)
	if norm.NFC.String(wordDecomposed) != wordComposed || wordComposed == wordDecomposed {
		t.Fatal("the fixture: the decomposed word is not the decomposed form of the composed one")
	}
	for _, c := range []struct{ name, applies, request string }{
		{"composed condition, composed request", wordComposed, wordComposed + askComposed},
		{"composed condition, decomposed request", wordComposed, wordDecomposed + askComposed},
		{"decomposed condition, composed request", wordDecomposed, wordComposed + askComposed},
		{"decomposed condition, decomposed request", wordDecomposed, wordDecomposed + askComposed},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/project"}
			source := seed(t, b, 1)
			for i, applies := range []string{c.applies, "\u753b\u9762\u306e\u8272", "\u8a18\u9332\u306e\u5f62", "\u6587\u66f8\u306e\u5e45"} {
				d := draft(source, "lesson "+string(rune('A'+i)))
				d.Lesson, d.AppliesTo = "\u6c7a\u307e\u308a "+string(rune('A'+i)), applies
				if _, err := b.Ingest(d); err != nil {
					t.Fatal(err)
				}
			}
			if p, _ := b.Recall(c.request); len(p) != 1 || p[0].Title != "lesson A" {
				t.Fatalf("the request %q delivers the page whose condition is %q: %+v", c.request, c.applies, p)
			}
		})
	}
}
