package main

import (
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The word searches of the project's wiki that found no candidate (internal/notebook, missed_searches.go). Two entrances of the
// word search remember them (noteMissedSearch): an explicit `wiki query` with words, and a prepare that carries a receipt and a
// word query (extendWork). The query of a standalone preparation is the topic of a task, not a question the agent asked the wiki:
// in a young wiki nearly every preparation would leave a miss, so it is not recorded. A record that carries a result names the
// misses made since its work was prepared, and the wordless query lists the ones still open. A miss says what was searched and
// found nothing: it is no proof that an article is missing, and no instruction.
//
// A record that names misses ties them to the saved result of its work (tieMissedSearches). A later search of the same words that
// finds nothing again is told where that result is (earlier_search), without asking the agent to write anything more.

// workMissedSearchesShown is how many open misses a record names; wikiMissedSearchesListed is how many the wordless query lists.
const (
	workMissedSearchesShown  = 3
	wikiMissedSearchesListed = 8
)

// workMissedSearches is what a record says of the searches that found nothing while its work ran.
type workMissedSearches struct {
	Queries    []string `json:"queries"`
	NextAction string   `json:"next_action"`
}

const workMissedNext = "These searches of this project's wiki found no article while this work was open. If this work found the answer to one of them with evidence, the next search of those words will miss again: add a short note (the answer, or where it is written) in a work record with the same receipt. If it did not, leave it as it is. This is not an instruction to search again."

// earlierSearchNext is the note of earlier_search. The result is a report of that work, not an article: it is to be confirmed.
const earlierSearchNext = "An earlier work searched these same words, found no article, and recorded a result. Read that result with a query of its id (`result`): it may say what was found or where the answer is written. It is that work's own report, not an article and not verified: confirm it before relying on it."

// noteMissedSearch keeps the words of a public word search that found no candidate, and returns earlier_search when an earlier work
// searched the same words, found none and recorded a result (nil otherwise). It is best effort: a journal that cannot be written
// must not fail the search that was answered. The memory-upgrade queries are the work's own commands, not words searched.
func noteMissedSearch(book notebook.Book, query, cursor string, view notebook.View) *notebook.EarlierSearch {
	if isMemoryUpgradeQuery(strings.TrimSpace(query)) {
		return nil
	}
	earlier, err := book.RecordMiss(query, cursor, view)
	if err != nil || earlier == nil {
		return nil
	}
	return &notebook.EarlierSearch{Query: earlier.Query, At: earlier.At, Result: earlier.Result, NextAction: earlierSearchNext}
}

// tieMissedSearches ties the misses a record named to the saved result of its work (the last work that searched those words and
// found nothing is the one a later search is pointed at). It is best effort: the record is not changed for a journal that cannot be
// written.
func tieMissedSearches(book notebook.Book, named *workMissedSearches, result *workCompletionRef) {
	if named == nil || result == nil || result.Source == "" {
		return
	}
	_ = book.NoteMissResult(named.Queries, result.Source)
}

// recordedMissedSearches is the open misses since the work was prepared, or nil when there are none or the journal cannot be read
// (the record is not changed for it).
func recordedMissedSearches(book notebook.Book, preparedAt time.Time) *workMissedSearches {
	misses, err := book.OpenMisses(preparedAt, workMissedSearchesShown)
	if err != nil || len(misses) == 0 {
		return nil
	}
	reply := &workMissedSearches{Queries: make([]string, len(misses)), NextAction: workMissedNext}
	for i, m := range misses {
		reply.Queries[i] = m.Query
	}
	return reply
}

// listedMissedSearches is what the wordless query lists: the open misses of all time, newest first. It is best effort: the misses
// are an addition to the maintenance listing, so a journal that cannot be read lists none and never fails the listing.
func listedMissedSearches(book notebook.Book) []notebook.Miss {
	misses, err := book.OpenMisses(time.Time{}, wikiMissedSearchesListed)
	if err != nil {
		return nil
	}
	return misses
}
