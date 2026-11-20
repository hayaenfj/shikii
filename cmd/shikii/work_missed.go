package main

import (
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// The word searches of the project's wiki that found no candidate (internal/notebook, missed_searches.go). Two entrances of the
// word search remember them (noteMissedSearch): an explicit `wiki query` with words, and a prepare that carries a receipt and a
// word query (extendWork). The query of a standalone preparation is the topic of a task, not a question the agent asked the wiki:
// in a young wiki nearly every preparation would leave a miss, so it is not recorded. A record that carries a result names the
// newest three of the open misses made since its work was prepared, and the wordless query lists the ones still open. A search of the same words that
// returns an article closes the miss at either entrance (RecordLookup, closeMissedSearch). A miss says what was searched and
// found nothing: it is no proof that an article is missing, and no instruction.
//
// A search of several words that found nothing at either entrance also says which of its words find an article when searched alone
// (found_alone, noteFoundAlone): a search of several words needs two of them in one article, so alternatives put in one query find
// nothing together. It records nothing and carries no article.
//
// A record ties to the saved result of its work the open misses made since its work was prepared (all that one call of OpenMisses
// examines, up to 16, not only the three the reply names) whose words that result speaks of (tieMissedSearches: a word search of
// the wiki for the words would find the text of the result); the others keep the result they had, or none. A later search of the
// same words that finds nothing again is told where that result is (earlier_search), without asking the agent to write anything
// more.
//
// A search of a prepare with a receipt is the search of a session, and the miss remembers it (missSession): only the records of
// that session name and tie it, so a work of another session that was open at the time does not report words it never searched. A
// wiki query carries no session; the words it missed are named by a work that was open then, whichever it is, and tied by the one
// whose result speaks of them, so several works that were open then each take the words their own result speaks of.

// workMissedSearchesShown is how many open misses a record names; wikiMissedSearchesListed is how many the wordless query lists.
const (
	workMissedSearchesShown  = 3
	wikiMissedSearchesListed = 8
)

// workMissedSearches is what a record says of the searches that found nothing while its work ran.
type workMissedSearches struct {
	Queries    []string `json:"queries"`
	NextAction string   `json:"next_action"`
	// tied is every open miss the record may tie to its result, the newest first: the ones Queries names and the older ones the reply
	// does not name. It is not part of the reply.
	tied []string
}

const workMissedNext = "These searches of this project's wiki found no article while this work was open. If this work found the answer to one of them with evidence, the next search of those words will miss again: add a short note (the answer, or where it is written) in a work record with the same receipt. If it did not, leave it as it is. This is not an instruction to search again."

// earlierSearchNext is the note of earlier_search. The result is the report of a work that was open when the words missed and then
// recorded a result that speaks of them (a word search of the wiki for them finds its text): it is not necessarily the work that
// searched (several sessions run side by side in a project), it is not an article, and it is to be confirmed.
const earlierSearchNext = "These same words found no article before, and a work that was open then recorded a result that speaks of them. Read that result with a query of its id: it may say what was found or where the answer is written. It is that work's own report (it may not be the work that searched), not an article and not verified: confirm it before relying on it."

// foundAloneNext is the note of found_alone: a search of several words usually needs at least two of them in one article (two of the
// units the search splits its words into: a word of letters and digits is one, kanji and kana go two characters at a time), so words put in as
// alternatives for one thing find nothing together. It is a note on the search, not an instruction to keep searching.
const foundAloneNext = "No article has these words together (a search of several words usually needs at least two of them in one article). Searched alone, each word in `words` finds an article: search one of them. Search alternatives for one thing one word at a time. It is a note, not an instruction to keep searching."

// noteFoundAlone says which words of a public word search that found no candidate find an article when searched alone
// (found_alone), or returns nil. It is asked where noteMissedSearch is, for the same searches, and records nothing. It is best
// effort: a wiki that cannot be read leaves the answered search as it was.
func noteFoundAlone(book notebook.Book, query, cursor string, view notebook.View) *notebook.FoundAlone {
	words, err := book.WordsFoundAlone(query, cursor, view)
	if err != nil || len(words) == 0 {
		return nil
	}
	return &notebook.FoundAlone{Words: words, NextAction: foundAloneNext}
}

// missSession is what a miss keeps of the session of the work that searched: the same 16 hexadecimal digits the coverage ledger
// keeps of a session, never the session id itself.
func missSession(sessionID string) string {
	return store.CoverageSession(sessionID)
}

// noteMissedSearch keeps the words of a public word search that found no candidate, and returns earlier_search when an earlier work
// searched the same words, found none and recorded a result (nil otherwise). session is missSession of the work that searched, or
// empty for a wiki query. It is best effort: a journal that cannot be written must not fail the search that was answered. The
// memory-upgrade queries are the work's own commands, not words searched.
func noteMissedSearch(book notebook.Book, query, cursor, session string, view notebook.View) *notebook.EarlierSearch {
	if isMemoryUpgradeQuery(strings.TrimSpace(query)) {
		return nil
	}
	earlier, err := book.RecordMiss(query, cursor, session, view)
	if err != nil || earlier == nil {
		return nil
	}
	return &notebook.EarlierSearch{Query: earlier.Query, At: earlier.At, Result: earlier.Result, NextAction: earlierSearchNext}
}

// closeMissedSearch drops the miss of the words of a search of a prepare that carries a receipt when the search returned a
// candidate: the wiki query closes it where it records the lookup (RecordLookup), and this entrance records none. It is best effort.
func closeMissedSearch(book notebook.Book, query string, view notebook.View) {
	if len(view.Lessons) > 0 || view.Search != nil && view.Search.Total > 0 {
		_ = book.CloseMiss(query)
	}
}

// tieMissedSearches ties the open misses of a record to the saved result of its work, those whose words the text of the result speaks
// of (the last work that searched those words, found nothing and reported them is the one a later search is pointed at). They are all
// the open misses the record found (workMissedSearches.tied), not only the ones the reply names: the result may speak of words
// searched long before the last three. A miss the result does not speak of keeps the result it had, so a work that named it only
// because it was open then does not take it. session is missSession of the work. It is best effort: the record is not changed for a
// journal that cannot be written or a result that cannot be read.
func tieMissedSearches(book notebook.Book, named *workMissedSearches, result *workCompletionRef, session string) {
	if named == nil || result == nil || result.Source == "" {
		return
	}
	_ = book.NoteMissResult(named.tied, result.Source, session)
}

// recordedMissedSearches is the open misses since the work was prepared that its session made or that no session is known for
// (session is missSession of the work), or nil when there are none or the journal cannot be read (the record is not changed for it).
// It asks the journal once, for as many misses as one call examines: the reply names the newest workMissedSearchesShown of them, and
// all of them are what the record may tie to its result.
func recordedMissedSearches(book notebook.Book, preparedAt time.Time, session string) *workMissedSearches {
	misses, err := book.OpenMisses(preparedAt, notebook.OpenMissesMax, session)
	if err != nil || len(misses) == 0 {
		return nil
	}
	reply := &workMissedSearches{Queries: make([]string, 0, min(len(misses), workMissedSearchesShown)), NextAction: workMissedNext, tied: make([]string, len(misses))}
	for i, m := range misses {
		reply.tied[i] = m.Query
		if i < workMissedSearchesShown {
			reply.Queries = append(reply.Queries, m.Query)
		}
	}
	return reply
}

// listedMissedSearches is what the wordless query lists: the open misses of all time and of every session, newest first. It is best effort: the misses
// are an addition to the maintenance listing, so a journal that cannot be read lists none and never fails the listing.
func listedMissedSearches(book notebook.Book) []notebook.Miss {
	misses, err := book.OpenMisses(time.Time{}, wikiMissedSearchesListed, "")
	if err != nil {
		return nil
	}
	return misses
}
