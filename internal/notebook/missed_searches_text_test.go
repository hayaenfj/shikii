package notebook

import (
	"os"
	"testing"
	"time"
)

// savedResult saves text as a source of the book, the form of the saved result of a work (an observation its author wrote), and
// returns its id.
func savedResult(t *testing.T, b Book, text string) string {
	t.Helper()
	o, err := b.capture(Observation{Kind: "test", Title: "Saved result", Content: text, Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// missedBook is a project whose journal holds one miss for each of the given words, oldest first.
func missedBook(t *testing.T, queries ...string) Book {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i, query := range queries {
		if _, err := b.recordMissAt(query, "", "", emptyView(), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

// tiedResults is the result each miss of the journal is tied to, by its words ("" for none).
func tiedResults(t *testing.T, b Book) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, m := range missesOnDisk(t, b) {
		got[m.Query] = m.Result
	}
	return got
}

func sameResults(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for query, result := range want {
		if have, ok := got[query]; !ok || have != result {
			return false
		}
	}
	return true
}

// A result is tied only to the named misses whose words would find the text of that result: a work that names two misses but
// speaks of one of them in its report is not pointed at for the other. The words that were not named are never tied.
func TestNoteMissResultTiesOnlyTheNamedMissesTheResultSpeaksOf(t *testing.T) {
	b := missedBook(t, "saffron lanterns", "juniper meadow", "kestrel harbor")
	result := savedResult(t, b, "Checked the saffron lanterns by hand: the export kept its filters.")
	if err := b.NoteMissResult([]string{"saffron lanterns", "juniper meadow"}, result, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := tiedResults(t, b), (map[string]string{"saffron lanterns": result, "juniper meadow": "", "kestrel harbor": ""}); !sameResults(got, want) {
		t.Fatalf("only the named words the text speaks of are tied: %v, want %v", got, want)
	}
	// the words are decided one by one, whatever their case and spacing (the same search, as missKey counts it)
	b = missedBook(t, "saffron lanterns", "juniper meadow")
	result = savedResult(t, b, "The Juniper and the saffron were checked, then the lanterns, then the meadow.")
	if err := b.NoteMissResult([]string{"  SAFFRON \t Lanterns", "Juniper   meadow "}, result, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := tiedResults(t, b), (map[string]string{"saffron lanterns": result, "juniper meadow": result}); !sameResults(got, want) {
		t.Fatalf("words that are the same search, in another case and spacing, are found by the text: %v, want %v", got, want)
	}
}

// A miss keeps the result it was tied to when the later result does not speak of its words, and moves to the later result when it
// does: the last work whose report speaks of the words is the one a later search is pointed at.
func TestNoteMissResultKeepsTheEarlierResultUnlessTheNewOneSpeaksOfTheWords(t *testing.T) {
	b := missedBook(t, "saffron lanterns", "juniper meadow")
	both := []string{"saffron lanterns", "juniper meadow"}
	first := savedResult(t, b, "The saffron lanterns were lit by hand.")
	if err := b.NoteMissResult(both, first, ""); err != nil {
		t.Fatal(err)
	}
	second := savedResult(t, b, "The juniper meadow was mown in the morning.")
	if err := b.NoteMissResult(both, second, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := tiedResults(t, b), (map[string]string{"saffron lanterns": first, "juniper meadow": second}); !sameResults(got, want) {
		t.Fatalf("the saffron words keep the first result, the juniper words take the second: %v, want %v", got, want)
	}
	third := savedResult(t, b, "Lit the saffron lanterns again and mown the juniper meadow.")
	if err := b.NoteMissResult(both, third, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := tiedResults(t, b), (map[string]string{"saffron lanterns": third, "juniper meadow": third}); !sameResults(got, want) {
		t.Fatalf("a result that speaks of both takes both: %v, want %v", got, want)
	}
	for _, m := range missesOnDisk(t, b) {
		if want := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC); m.At.Before(want) || m.At.After(want.Add(time.Minute)) {
			t.Fatalf("tying a result does not touch the time of a miss: %+v", m)
		}
	}
}

// A result whose source cannot be read ties nothing, and a result that speaks of none of the words writes nothing: the file of the
// journal (written by hand with spaces, so that a rewrite would show) keeps its bytes.
func TestNoteMissResultTiesNothingWhenItCannotReadTheResultOrItSpeaksOfNoneOfTheWords(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	other := savedResult(t, b, "Mown the juniper meadow before noon.")
	path := journalPath(t, b)
	const file = `{ "lookups": [], "misses": [ {"query":"saffron lanterns","at":"2026-11-01T09:00:00Z"} ] }`
	for name, result := range map[string]string{
		"a source that does not exist": resultA,
		"a result of other words":      other,
	} {
		if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		err := b.NoteMissResult([]string{"saffron lanterns"}, result, "")
		if (err != nil) != (result == resultA) {
			t.Fatalf("%s: an unreadable result is an error and a result of other words is not: %v", name, err)
		}
		if after, readErr := os.ReadFile(path); readErr != nil || string(after) != file {
			t.Fatalf("%s: the journal changed: %s (%v)", name, after, readErr)
		}
	}
}

// wordsFind answers as the word search of the wiki does, for the text of one page: every case below is run through the real search
// of a project that holds one article with that text, and the table pins what both say.
func TestWordsFindAnswersAsTheWordSearchOfTheWikiDoes(t *testing.T) {
	for _, c := range []struct {
		name, query, text string
		want              bool
	}{
		{"both words", "saffron lanterns", "The saffron lanterns were lit by hand.", true},
		{"the words in another order", "lanterns saffron", "The saffron lanterns were lit by hand.", true},
		{"another case", "SAFFRON Lanterns", "the saffron lanterns were lit by hand.", true},
		{"one of two words", "saffron lanterns", "The saffron was bought at the market.", false},
		{"two of three words", "saffron lanterns market", "The saffron lanterns were lit by hand.", true},
		{"one of three words", "saffron harbor market", "The saffron lanterns were lit by hand.", false},
		{"a single word", "saffron", "The saffron lanterns were lit by hand.", true},
		{"a single word that is not there", "juniper", "The saffron lanterns were lit by hand.", false},
		{"a part of a word", "lantern", "The saffron lanterns were lit by hand.", false},
		{"the composed form of a letter against the decomposed one", "caf\u00e9 menu", "The cafe\u0301 menu was printed.", true},
		{"the decomposed form of a letter against the composed one", "cafe\u0301 menu", "The caf\u00e9 menu was printed.", true},
		{"Japanese words that are there", "\u76e3\u67fb\u624b\u9806", "\u76e3\u67fb\u624b\u9806\u3092\u78ba\u8a8d\u3057\u305f\u3002", true},
		{"Japanese words that are not there", "\u7d4c\u8cbb\u7cbe\u7b97", "\u76e3\u67fb\u624b\u9806\u3092\u78ba\u8a8d\u3057\u305f\u3002", false},
		{"no words", "", "The saffron lanterns were lit by hand.", false},
		{"no text", "saffron lanterns", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := wordsFind(c.query, c.text); got != c.want {
				t.Fatalf("wordsFind(%q, %q) = %v, want %v", c.query, c.text, got, c.want)
			}
			if c.text == "" {
				return // an article has no empty text: the search has nothing to compare
			}
			b := Book{Root: t.TempDir(), Scope: "/work/words-find"}
			o, err := b.capture(Observation{Kind: "test", Title: "Synthetic source", Content: "A source for the article.", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Ingest(Draft{Title: "Note", Lesson: c.text, AppliesTo: "notes", Kind: "howto", Scene: "reading notes", Sources: []string{o.ID}}); err != nil {
				t.Fatal(err)
			}
			view, err := b.QueryPage(c.query, "")
			if err != nil {
				t.Fatal(err)
			}
			if found := view.Search != nil && view.Search.Total > 0; found != c.want {
				t.Fatalf("the word search of the wiki says %v for %q against %q, the table says %v", found, c.query, c.text, c.want)
			}
		})
	}
}

// fieldWords are the names of the fields of the saved result of a work (and of a row of its coverage). They are structure, not words
// the author wrote: a miss of one of them is tied only by a result whose values use the word.
var fieldWords = []string{"checked", "changed", "remaining", "coverage", "area", "outcome", "evidence", "reason"}

// A saved result is JSON, and a miss is tied by what its author wrote (the string values), never by the names of its fields: the
// one-word searches "checked" and "changed" would otherwise be found by every result there is. A result whose values do not use the
// words has all those fields, a coverage row too, and is tied to none of them; a result whose value uses one of them is tied to it.
func TestNoteMissResultIsNotTiedByTheFieldNamesOfAResult(t *testing.T) {
	b := missedBook(t, fieldWords...)
	plain := savedResult(t, b, `{"changed":"Audited the export.","checked":"Read the export code.","evidence":["new1"],"remaining":"The import path was not read.","coverage":[{"area":"Import path","outcome":"excluded","evidence":"new1","reason":"No import code in this change."}]}`)
	path := journalPath(t, b)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.NoteMissResult(fieldWords, plain, ""); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatalf("a result whose values use none of the field names ties none of them, and the journal keeps its bytes: %s (%v)", after, err)
	}
	spoken := savedResult(t, b, `{"changed":"Audited the export.","checked":"Checked the ledger twice.","evidence":["new1"]}`)
	if err := b.NoteMissResult(fieldWords, spoken, ""); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, word := range fieldWords {
		want[word] = ""
	}
	want["checked"] = spoken
	if got := tiedResults(t, b); !sameResults(got, want) {
		t.Fatalf("only the word a value uses is tied: %v, want %v", got, want)
	}
}

// The words of a coverage row (an object in an array in the result) are the author's words: a miss whose words appear only in the area
// or the reason of a row is tied, and so are words deeper in any object or array, an array of strings included. Words that are in no
// value are not.
func TestNoteMissResultIsTiedByTheValuesNestedInAResult(t *testing.T) {
	b := missedBook(t, "saffron wiring", "juniper meadow", "kestrel harbor", "quartz ledger", "walnut grove")
	result := savedResult(t, b, `{"changed":"Reviewed.","coverage":[{"area":"Saffron wiring","outcome":"excluded","reason":"The juniper meadow has none."}],"notes":{"inner":[{"text":"Kestrel harbor"}],"list":["a quartz ledger"]}}`)
	if err := b.NoteMissResult([]string{"saffron wiring", "juniper meadow", "kestrel harbor", "quartz ledger", "walnut grove"}, result, ""); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"saffron wiring": result, "juniper meadow": result, "kestrel harbor": result, "quartz ledger": result, "walnut grove": ""}
	if got := tiedResults(t, b); !sameResults(got, want) {
		t.Fatalf("the words of the nested values tie, the words in none do not: %v, want %v", got, want)
	}
}

// Numbers, booleans and null are not text: a miss of one of them is not tied, and the words of a string next to them are.
func TestNoteMissResultIsNotTiedByANumberABooleanOrNull(t *testing.T) {
	b := missedBook(t, "4242", "false", "null", "saffron lanterns")
	result := savedResult(t, b, `{"count":4242,"flag":false,"none":null,"note":"Saffron lanterns."}`)
	if err := b.NoteMissResult([]string{"4242", "false", "null", "saffron lanterns"}, result, ""); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"4242": "", "false": "", "null": "", "saffron lanterns": result}
	if got := tiedResults(t, b); !sameResults(got, want) {
		t.Fatalf("a number, a boolean and null are not text: %v, want %v", got, want)
	}
}

// Content that is not JSON is the text as it is: words of a plain report tie, and so does a text that only looks like JSON (cut off,
// so it is none), field names included, because nothing says they are names.
func TestNoteMissResultUsesContentThatIsNotJSONAsItIs(t *testing.T) {
	b := missedBook(t, "saffron lanterns", "changed", "kestrel harbor")
	plain := savedResult(t, b, "Checked the saffron lanterns by hand.")
	if err := b.NoteMissResult([]string{"saffron lanterns", "changed"}, plain, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := tiedResults(t, b), (map[string]string{"saffron lanterns": plain, "changed": "", "kestrel harbor": ""}); !sameResults(got, want) {
		t.Fatalf("a plain report ties by its words: %v, want %v", got, want)
	}
	cut := savedResult(t, b, `{"changed":"Walked the kestrel harbor.`)
	if err := b.NoteMissResult([]string{"changed", "kestrel harbor"}, cut, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := tiedResults(t, b), (map[string]string{"saffron lanterns": plain, "changed": cut, "kestrel harbor": cut}); !sameResults(got, want) {
		t.Fatalf("text that is not JSON is used as it is, quotes and names too: %v, want %v", got, want)
	}
}
