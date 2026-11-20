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

const workMissedNext = "These searches of this project's wiki found no article while this work was open. This is a note, not an instruction. If this work found the answer to one of them with evidence, add it as a note in a work record with the same receipt. If it did not, leave it as it is."

// noteMissedSearch keeps the words of a public word search that found no candidate. It is best effort: a journal that cannot be
// written must not fail the search that was answered. The memory-upgrade queries are the work's own commands, not words searched.
func noteMissedSearch(book notebook.Book, query, cursor string, view notebook.View) {
	if isMemoryUpgradeQuery(strings.TrimSpace(query)) {
		return
	}
	_ = book.RecordMiss(query, cursor, view)
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
