package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// addMissRowAt puts a miss of another session into the existing journal file, as the oldest, as an earlier version left it: the same
// words typed in another case are a miss of their own there.
func (f missedFlow) addMissRowAt(query, at, session string) {
	f.t.Helper()
	path := f.journalPath()
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		f.t.Fatal(err)
	}
	old, _ := file["misses"].([]any)
	file["misses"] = append([]any{map[string]any{"query": query, "at": at, "session": session}}, old...)
	data, _ = json.Marshal(file)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// A journal that an earlier version wrote holds the same words twice when they were typed in another case, and the result may be on
// the older of the two. The wordless query lists the words once, with that result, and a search of them that finds nothing again is
// pointed at the result and leaves one miss.
func TestTheSameWordsKeptTwiceByAnEarlierVersionAreListedOnceAndKeepTheirResult(t *testing.T) {
	const id = "fedcba9876543210"
	f := newMissedFlow(t)
	f.wiki("saffron lanterns")
	f.addMissAt("Saffron Lanterns", "2026-11-01T09:00:00Z", id)
	if got := f.journalMisses(); !slices.Equal(got, []string{"Saffron Lanterns", "saffron lanterns"}) {
		t.Fatalf("the fixture: the words are in the journal twice: %q", got)
	}
	listing, _ := f.wiki("")["missed_searches"].([]any)
	if len(listing) != 1 || listing[0].(map[string]any)["query"] != "saffron lanterns" || listing[0].(map[string]any)["result"] != id {
		t.Fatalf("the listing names the words once, with the result of the one that has it: %v", listing)
	}
	if earlier := earlierSearchOf(t, f.wiki("saffron lanterns")); earlier["result"] != id {
		t.Fatalf("a search that finds nothing again is pointed at the result the older miss had: %v", earlier)
	}
	if got := f.journalMisses(); !slices.Equal(got, []string{"saffron lanterns"}) {
		t.Fatalf("the search leaves one miss of the words: %q", got)
	}
	if _, result, _ := f.journalMiss("saffron lanterns"); result != id {
		t.Fatalf("the one miss keeps the result: %q", result)
	}
}

// The record of a work ties the words it names to the misses of its own session: a miss of the same words that another session left
// (here the same words in another case, as an earlier version kept them) is not tied to the result of this work, as it would be if the
// record asked for the misses of every session.
func TestARecordDoesNotTieTheSameWordsThatAnotherSessionMissed(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	f.prepare("import retries", prepared["receipt"].(string)) // a search of this session, through its receipt
	f.addMissRowAt("Import Retries", "2026-11-01T09:00:00Z", "2222222222222222")
	saved := f.record(prepared, goodResultAbout("import retries"))
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"import retries"}) {
		t.Fatalf("the record names the words of its own session once: %q", got)
	}
	id := resultID(t, saved)
	if _, result, found := f.journalMiss("import retries"); !found || result != id {
		t.Fatalf("the miss of this session is tied to the result of its work (%s): %q %v", id, result, found)
	}
	if _, result, found := f.journalMiss("Import Retries"); !found || result != "" {
		t.Fatalf("the miss of the other session is left as it is: %q %v", result, found)
	}
}
