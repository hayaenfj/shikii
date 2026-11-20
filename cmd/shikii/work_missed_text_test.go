package main

import (
	"slices"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// A record names the misses made while its work was open and ties to its result only those the result speaks of, through the whole
// path an agent takes: two searches of a prepare that carries a receipt, a record whose result speaks of one of them, and then the
// same words searched again. The words the result does not speak of are named, left tied to nothing, and a later search of them is
// pointed at no result.
func TestRecordTiesOnlyTheNamedMissesItsResultSpeaksOf(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	receipt := prepared["receipt"].(string)
	f.prepare("saffron lanterns", receipt)
	f.prepare("juniper meadow", receipt)
	saved := f.record(prepared, goodResultAbout("saffron lanterns"))
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"juniper meadow", "saffron lanterns"}) {
		t.Fatalf("the record names both open misses, newest first: %q", got)
	}
	id := resultID(t, saved)
	if _, result, found := f.journalMiss("saffron lanterns"); !found || result != id {
		t.Fatalf("the words the result speaks of are tied to it (%s): %q %v", id, result, found)
	}
	if _, result, found := f.journalMiss("juniper meadow"); !found || result != "" {
		t.Fatalf("the words the result does not speak of are tied to nothing: %q %v", result, found)
	}
	if earlier := earlierSearchOf(t, f.wiki("saffron lanterns")); earlier["result"] != id {
		t.Fatalf("the words tied to the result are pointed at it: %v", earlier)
	}
	if reply := f.wiki("juniper meadow"); reply["earlier_search"] != nil {
		t.Fatalf("the words no result speaks of are pointed at none: %v", reply["earlier_search"])
	}
}

// A wiki query carries no session, so the records of two works that were open then both name its misses. Each result is tied to the
// words it speaks of, whichever work records first, and the later record does not take the words of the earlier one.
func TestTwoRecordsOfAWikiQueryTieEachWordsToTheResultThatSpeaksOfThem(t *testing.T) {
	for _, firstRecordsFirst := range []bool{true, false} {
		name := "the work of the juniper meadow records last"
		if !firstRecordsFirst {
			name = "the work of the juniper meadow records first"
		}
		t.Run(name, func(t *testing.T) {
			f := newMissedFlow(t)
			saffron := f.prepare("export audit", "")
			juniper := f.prepare("export audit", "")
			f.wiki("saffron lanterns")
			f.wiki("juniper meadow")
			var savedSaffron, savedJuniper map[string]any
			if firstRecordsFirst {
				savedSaffron = f.record(saffron, goodResultAbout("saffron lanterns"))
				savedJuniper = f.record(juniper, goodResultAbout("juniper meadow"))
			} else {
				savedJuniper = f.record(juniper, goodResultAbout("juniper meadow"))
				savedSaffron = f.record(saffron, goodResultAbout("saffron lanterns"))
			}
			for _, saved := range []map[string]any{savedSaffron, savedJuniper} {
				if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"juniper meadow", "saffron lanterns"}) {
					t.Fatalf("both works name both misses: %q", got)
				}
			}
			// an array, not a map: each search below records its miss again, so the order it runs in is fixed
			for _, c := range []struct{ query, want string }{{"saffron lanterns", resultID(t, savedSaffron)}, {"juniper meadow", resultID(t, savedJuniper)}} {
				if _, result, found := f.journalMiss(c.query); !found || result != c.want {
					t.Fatalf("%q is tied to the result that speaks of it (%s): %q %v", c.query, c.want, result, found)
				}
				if earlier := earlierSearchOf(t, f.wiki(c.query)); earlier["result"] != c.want {
					t.Fatalf("a later search of %q is pointed at the result that speaks of it (%s): %v", c.query, c.want, earlier)
				}
			}
		})
	}
}

// The saved result of a real work is JSON with fixed field names; the words searched are never found by those names. Two one-word
// searches that are field names of every result miss, and a record whose values use neither ties neither: only the words of a value
// (here "export code", from the checked part) tie. The record names all three, as before.
func TestRecordIsNotTiedByTheFieldNamesOfItsResult(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	for _, query := range []string{"checked", "changed", "export code"} {
		f.wiki(query)
	}
	saved := f.record(prepared, goodResult())
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"export code", "changed", "checked"}) {
		t.Fatalf("the record names the three open misses, newest first: %q", got)
	}
	id := resultID(t, saved)
	if _, result, found := f.journalMiss("export code"); !found || result != id {
		t.Fatalf("the words of a value of the result are tied to it (%s): %q %v", id, result, found)
	}
	for _, query := range []string{"checked", "changed"} {
		if _, result, found := f.journalMiss(query); !found || result != "" {
			t.Fatalf("%q is the name of a field of every result and ties none: %q %v", query, result, found)
		}
	}
}

// The words the program chose are not the author's: the outcome of a coverage row and the ref the evidence names are in the saved
// result of a real work, and a search of them is tied to nothing; the area of a row is the author's, and ties.
func TestRecordIsNotTiedByTheOutcomeOrTheEvidenceRefOfItsResult(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	for _, query := range []string{"reported excluded", "new1", "second part"} {
		f.wiki(query)
	}
	result := &workCompletion{Changed: "Audited the export.", Checked: "Read the export code.", Evidence: []string{"new1"}, Coverage: []workCoverageRow{
		{Area: "First part", Outcome: "reported", Evidence: "new1"},
		{Area: "Second part", Outcome: "excluded", Reason: "Not needed."},
	}}
	saved := f.record(prepared, result)
	if saved["complete"] != true {
		t.Fatalf("the record is complete: %v", saved)
	}
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"second part", "new1", "reported excluded"}) {
		t.Fatalf("the record names the three open misses, newest first: %q", got)
	}
	id := resultID(t, saved)
	if _, tied, found := f.journalMiss("second part"); !found || tied != id {
		t.Fatalf("the area of a row is the author's and is tied to the result (%s): %q %v", id, tied, found)
	}
	for _, query := range []string{"reported excluded", "new1"} {
		if _, tied, found := f.journalMiss(query); !found || tied != "" {
			t.Fatalf("%q is the outcome or the evidence ref the program wrote, and ties none: %q %v", query, tied, found)
		}
	}
}

// A record names at most three open misses, but it ties to its result every open miss of its own session (and those no session is
// known for) whose words the result speaks of, as many as one search of the open misses examines: the words of an older search that
// the result speaks of are tied although the reply does not name them. The words the result does not speak of are not tied, named or
// not, and the misses of another session are not tied at all.
func TestRecordTiesEveryOpenMissItsResultSpeaksOfAndNamesThreeOfThem(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	receipt := prepared["receipt"].(string)
	for _, query := range []string{"quokka wombat", "numbat bilby", "kestrel harbor", "juniper meadow", "walnut grove"} {
		f.prepare(query, receipt) // a search of this session through its receipt, oldest first
	}
	f.addMissRowAt("orchid valley", "2099-01-01T00:00:00Z", "2222222222222222") // another session searched these words after the preparation
	saved := f.record(prepared, goodResultAbout("quokka wombat", "juniper meadow", "orchid valley"))
	if saved["complete"] != true {
		t.Fatalf("the record is complete: %v", saved)
	}
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"walnut grove", "juniper meadow", "kestrel harbor"}) {
		t.Fatalf("the reply names the three newest open misses of its session, as before: %q", got)
	}
	id := resultID(t, saved)
	for query, want := range map[string]string{
		"quokka wombat":  id, // not named, and the result speaks of it
		"juniper meadow": id, // named, and the result speaks of it
		"numbat bilby":   "", // not named, and the result does not speak of it
		"kestrel harbor": "", // named, and the result does not speak of it
		"walnut grove":   "",
		"orchid valley":  "", // the result speaks of it, and another session searched it
	} {
		if _, result, found := f.journalMiss(query); !found || result != want {
			t.Fatalf("%q is tied to %q, want %q: %v", query, result, want, found)
		}
	}
	if earlier := earlierSearchOf(t, f.wiki("quokka wombat")); earlier["result"] != id {
		t.Fatalf("the same words that miss again are pointed at the result: %v", earlier)
	}
}

// A record asks the journal of open misses once, however many misses it names and ties: each call reads the wiki and splits its articles once for the
// misses it examines, so the count of parses of the one article is the measure (it does not depend on how fast a machine is).
func TestRecordAsksTheJournalOfOpenMissesOnce(t *testing.T) {
	f := newMissedFlow(t)
	for _, query := range []string{"quokka wombat", "numbat bilby", "kestrel harbor", "juniper meadow", "walnut grove"} {
		f.wiki(query)
	}
	before := notebook.PageParses()
	named := recordedMissedSearches(f.b, time.Time{}, missSession("session-of-the-work"))
	if named == nil || len(named.Queries) != workMissedSearchesShown || len(named.tied) != 5 {
		t.Fatalf("the reply names %d of the 5 open misses and keeps all 5 to tie: %+v", workMissedSearchesShown, named)
	}
	if parses := notebook.PageParses() - before; parses != 1 {
		t.Fatalf("5 open misses over one article are examined in one call that parses it once, got %d passes", parses)
	}
}
