package main

import (
	"slices"
	"testing"
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
			for query, want := range map[string]string{"saffron lanterns": resultID(t, savedSaffron), "juniper meadow": resultID(t, savedJuniper)} {
				if _, result, found := f.journalMiss(query); !found || result != want {
					t.Fatalf("%q is tied to the result that speaks of it (%s): %q %v", query, want, result, found)
				}
				if earlier := earlierSearchOf(t, f.wiki(query)); earlier["result"] != want {
					t.Fatalf("a later search of %q is pointed at the result that speaks of it (%s): %v", query, want, earlier)
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
