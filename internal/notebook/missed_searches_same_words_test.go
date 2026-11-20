package notebook

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Two searches are the same words when they differ only in case, in white space or in how a character is composed (a letter and
// a combining mark, or the one composed character): the search compares words in lower case, and the same text typed or pasted in
// another composition is the same question. They are one miss, kept as the words were last typed, in the composed form.
func TestTheSameWordsInAnotherCaseOrCompositionAreOneMiss(t *testing.T) {
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct{ name, first, again, kept string }{
		{"case", "Saffron Lanterns", "SAFFRON  lanterns", "SAFFRON lanterns"},
		{"a letter and its combining mark", "cafe\u0301 menus", "caf\u00e9 menus", "caf\u00e9 menus"},
		{"the composed letter first", "caf\u00e9 menus", "Cafe\u0301 menus", "Caf\u00e9 menus"},
		{"a kana and its voiced mark", "\u30ab\u3099\u30a4\u30c8\u3099 \u691c\u7d22", "\u30ac\u30a4\u30c9 \u691c\u7d22", "\u30ac\u30a4\u30c9 \u691c\u7d22"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
			if earlier, err := b.recordMissAt(c.first, "", "", emptyView(), base); earlier != nil || err != nil {
				t.Fatalf("a first miss has no earlier one: %+v %v", earlier, err)
			}
			// the record of a work names the words as the journal holds them; a caller that names them in another case ties them too
			if err := b.NoteMissResult([]string{strings.ToUpper(c.again)}, resultA, ""); err != nil {
				t.Fatal(err)
			}
			earlier, err := b.recordMissAt(c.again, "", "", emptyView(), base.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if earlier == nil || earlier.Result != resultA || !earlier.At.Equal(base) {
				t.Fatalf("the same words in another form are told where the earlier result is: %+v", earlier)
			}
			got := missesOnDisk(t, b)
			if len(got) != 1 || got[0].Query != c.kept || got[0].Result != resultA || !got[0].At.Equal(base.Add(time.Minute)) {
				t.Fatalf("one miss, as the words were last typed and composed, still tied to the result: %+q", got)
			}
			// a search of the same words in yet another case that returns an article closes it
			if err := b.CloseMiss(strings.ToLower(c.first)); err != nil {
				t.Fatal(err)
			}
			if got := missesOnDisk(t, b); len(got) != 0 {
				t.Fatalf("the miss is closed by the same words in another form: %+q", got)
			}
		})
	}
}

// The lookup of a wiki query closes the miss of the same words in another case too.
func TestALookupClosesTheMissOfTheSameWordsInAnotherCase(t *testing.T) {
	b := missFixture(t)
	if _, err := b.recordMissAt("Quartz EXPORTS", "", "", emptyView(), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	view, err := b.QueryPage("quartz exports", "")
	if err != nil || len(view.Lessons) == 0 {
		t.Fatalf("the fixture changed: %+v %v", view.Search, err)
	}
	if _, err := b.RecordLookup("quartz exports", view.Lessons); err != nil {
		t.Fatal(err)
	}
	if got := missesOnDisk(t, b); len(got) != 0 {
		t.Fatalf("the lookup of the same words closes the miss: %+q", got)
	}
}

// Words that differ by more than case, white space and composition stay two misses, and a query whose composed form is longer than
// the journal reads back is not kept at all (the reader would drop it).
func TestOtherWordsStayOtherMissesAndTheComposedFormIsBounded(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	// U+0958 is 3 bytes and its composed form (NFC) is two characters of 3 bytes: 34 of them are 102 bytes as typed and 204 composed
	long := strings.Repeat("\u0958", 34)
	for i, query := range []string{"saffron lantern", "saffron lanterns", "caf\u00e9 menus", "cafe menus", long} {
		if _, err := b.recordMissAt(query, "", "", emptyView(), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if got := diskQueries(missesOnDisk(t, b)); !slices.Equal(got, []string{"saffron lantern", "saffron lanterns", "caf\u00e9 menus", "cafe menus"}) {
		t.Fatalf("other words are other misses, and a form over the bound is not kept: %+q", got)
	}
	open, err := b.OpenMisses(time.Time{}, 8, "")
	if err != nil || len(open) != 4 {
		t.Fatalf("every miss the journal wrote is read back: %+q %v", openQueries(open), err)
	}
}
