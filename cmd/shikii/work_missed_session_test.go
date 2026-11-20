package main

import (
	"slices"
	"testing"
)

// Two works of two sessions are open side by side, and only one of them searches words that find nothing, through its receipt. The
// record of the other work does not name those words and does not tie them to its result, in either order of the two records: a
// later search of the same words is pointed at the result of the session that searched.
func TestARecordNamesAndTiesOnlyTheMissesOfItsOwnSession(t *testing.T) {
	for _, searcherFirst := range []bool{true, false} {
		name := "the work that searched records last"
		if searcherFirst {
			name = "the work that searched records first"
		}
		t.Run(name, func(t *testing.T) {
			f := newMissedFlow(t)
			bystander := f.prepare("export audit", "")
			searcher := f.prepare("export audit", "")
			f.prepare("import retries", searcher["receipt"].(string))
			var savedSearcher, savedBystander map[string]any
			if searcherFirst {
				savedSearcher = f.record(searcher, goodResult())
				savedBystander = f.record(bystander, goodResult())
			} else {
				savedBystander = f.record(bystander, goodResult())
				savedSearcher = f.record(searcher, goodResult())
			}
			if named, ok := savedBystander["missed_searches"]; ok {
				t.Fatalf("the work that did not search is not told the words of another session: %v", named)
			}
			if got := missedQueriesOf(t, savedSearcher); !slices.Equal(got, []string{"import retries"}) {
				t.Fatalf("the work that searched is told its words: %q", got)
			}
			want := resultID(t, savedSearcher)
			if _, result, found := f.journalMiss("import retries"); !found || result != want {
				t.Fatalf("the words are tied to the result of the session that searched (%s), not to %s: %q %v", want, resultID(t, savedBystander), result, found)
			}
			if earlier := earlierSearchOf(t, f.wiki("import retries")); earlier["result"] != want {
				t.Fatalf("a later search is pointed at the result of the session that searched (%s): %v", want, earlier)
			}
		})
	}
}

// A wiki query carries no session, so the words it missed are named and tied by a work that was open then, as before; and when
// another session misses the same words through its receipt, the words become that session's.
func TestAMissWithoutASessionIsNamedByAnOpenWorkUntilASessionMissesTheWords(t *testing.T) {
	f := newMissedFlow(t)
	first := f.prepare("export audit", "")
	second := f.prepare("export audit", "")
	f.wiki("import retries")
	savedFirst := f.record(first, goodResult())
	if got := missedQueriesOf(t, savedFirst); !slices.Equal(got, []string{"import retries"}) {
		t.Fatalf("a miss of a wiki query is named by a work that was open then: %q", got)
	}
	// the second work misses the same words through its receipt, while a third is open: the second is told where the first result
	// is, and the words are now its own
	third := f.prepare("export audit", "")
	if earlier := earlierSearchOf(t, f.prepare("import retries", second["receipt"].(string))); earlier["result"] != resultID(t, savedFirst) {
		t.Fatalf("the search of the second work is told the first result: %v", earlier)
	}
	if named, ok := f.record(third, goodResult())["missed_searches"]; ok {
		t.Fatalf("a third session is not told the words the second session missed: %v", named)
	}
	if _, result, _ := f.journalMiss("import retries"); result != resultID(t, savedFirst) {
		t.Fatalf("the record of a session that did not search leaves the tie as it was: %q", result)
	}
	savedSecond := f.record(second, goodResult())
	if got := missedQueriesOf(t, savedSecond); !slices.Equal(got, []string{"import retries"}) {
		t.Fatalf("the session that missed the words last is told them: %q", got)
	}
	if _, result, _ := f.journalMiss("import retries"); result != resultID(t, savedSecond) {
		t.Fatalf("the words are tied to the result of the session that missed them last: %q", result)
	}
}
