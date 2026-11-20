package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// A missed search is a word search of the wiki that found no candidate at all. The journal keeps the words (never an answer), so a
// later task can see which topics the wiki could not answer. It is a note on a gap, not evidence that an article is missing: a
// miss closes by itself once the same words find a candidate (OpenMisses), and RecordLookup (a wiki query) or CloseMiss (the search
// of a prepare with a receipt) drops it when a search of the same words returns an article.
const (
	// missedSearchMax is how many misses the journal keeps. The oldest are dropped first.
	missedSearchMax = 64
	// missQueryMax is the longest query (in bytes) a miss is kept for.
	missQueryMax = 200
	// openMissExamineMax is how many misses one OpenMisses call examines. Each examined miss runs a whole search of the wiki, so the
	// cost of a call is bounded by it, not by how many misses the journal keeps.
	openMissExamineMax = 16
)

// Miss is one word search that found no candidate. Query is stored as the words were last typed, with white space trimmed, each run
// of it collapsed to one space and its characters composed (normalizeMissQuery); At is when the search last found nothing. Result is the id of the saved result of the work that had the miss open when it
// recorded its result (NoteMissResult), or empty: the report of that work, never an answer the wiki holds.
type Miss struct {
	Query  string    `json:"query"`
	At     time.Time `json:"at"`
	Result string    `json:"result,omitempty"`
}

// EarlierSearch is the reply field that points a search that found nothing at the result of an earlier work that searched the same
// words and found nothing too. Result is the id of that result (a source, read by a query of the id); it is that work's own report.
type EarlierSearch struct {
	Query      string    `json:"query"`
	At         time.Time `json:"at"`
	Result     string    `json:"result"`
	NextAction string    `json:"next_action"`
}

// missedList is the misses of the search journal, read as an allow-list: a miss that is not in the form the journal writes (not a
// JSON object of the right fields, an empty query, a query over missQueryMax bytes, text that is not UTF-8, no time, a result that
// is not the 16-digit id of a source) is dropped, and
// more than missedSearchMax of them are cut to the newest. The misses never make the journal unreadable: they are a note on gaps,
// and an older or newer version of the program may have written another form or another bound (the other collections of the
// journal are still refused when they are invalid).
type missedList []Miss

func (l *missedList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		*l = nil
		return nil
	}
	kept := missedList{}
	for _, r := range raw {
		var m Miss
		if utf8.Valid(r) && json.Unmarshal(r, &m) == nil && m.Query != "" && len(m.Query) <= missQueryMax && !m.At.IsZero() && (m.Result == "" || idPattern.MatchString(m.Result)) {
			kept = append(kept, m)
		}
	}
	if len(kept) > missedSearchMax {
		kept = kept[len(kept)-missedSearchMax:]
	}
	if len(kept) == 0 {
		kept = nil
	}
	*l = kept
	return nil
}

// sourceIDWord is a word that is the id of a source in any case: 16 hexadecimal digits.
var sourceIDWord = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)

// onlySourceIDs reports whether every word of the normalized query is the id of a source (one id, several ids, upper case too).
func onlySourceIDs(q string) bool {
	for _, word := range strings.Fields(q) {
		if !sourceIDWord.MatchString(word) {
			return false
		}
	}
	return q != ""
}

// normalizeMissQuery is the form a miss is stored in: the words as typed, with white space trimmed, each run of it collapsed to one
// space, and a letter and its combining mark composed to one character (NFC), so that the same text pasted in another composition
// is stored, listed and searched again in one form.
func normalizeMissQuery(query string) string {
	return strings.Join(strings.Fields(norm.NFC.String(query)), " ")
}

// missKey is the form in which two queries count as the same search: the stored form in lower case. The search compares its words
// in lower case, so words that differ in case only find the same articles and are one miss.
func missKey(query string) string {
	return strings.ToLower(normalizeMissQuery(query))
}

// missQuery is the allow-list of what a miss is kept for: the first page of a search (no cursor) for words, in at most
// missQueryMax bytes of UTF-8 as typed and as stored, that are not a reference. It returns the words in the stored form. Whatever is not listed here is never
// recorded, so a new kind of query is not recorded until it is added here. OpenMisses applies the same list to what it reads.
// A query whose words are all ids of sources (one or several, in any case) is a reading of sources, not a search of words.
func missQuery(query, cursor string) (string, bool) {
	if cursor != "" || len(query) > missQueryMax || !utf8.ValidString(query) {
		return "", false
	}
	q := normalizeMissQuery(query)
	// "lesson:<id>" reads an article and a word of 16 hexadecimal digits is the id of a source: neither is a search of words. The
	// composed form of a few characters is longer than the typed one, and the reader drops a query over the bound.
	if q == "" || len(q) > missQueryMax || strings.HasPrefix(q, "lesson:") || onlySourceIDs(q) {
		return "", false
	}
	return q, true
}

// searchFoundNothing is true for the result of a word search that has no candidate: the search result exists (a query with words
// always has one), counts no candidate and returned no article.
func (v View) searchFoundNothing() bool {
	return v.Search != nil && v.Search.Total == 0 && len(v.Lessons) == 0
}

// lastMiss is a copy of the newest miss of the same words (key is their missKey), or nil.
func lastMiss(misses []Miss, key string) *Miss {
	for i := len(misses) - 1; i >= 0; i-- {
		if missKey(misses[i].Query) == key {
			m := misses[i]
			return &m
		}
	}
	return nil
}

// withoutMiss is the misses without those of the same words (key is their missKey).
func withoutMiss(misses []Miss, key string) []Miss {
	kept := make([]Miss, 0, len(misses))
	for _, m := range misses {
		if missKey(m.Query) != key {
			kept = append(kept, m)
		}
	}
	return kept
}

// RecordMiss is called where the agent asks the wiki a question in words: an explicit wiki query, and the search of a prepare that
// carries a receipt. The query of a standalone preparation is the topic of a task, not a question, and does not call it. It keeps
// the words of a search that found no candidate (a repeated search of the same words, in any case and composition, keeps one entry
// with a newer time, and keeps the result the earlier one was tied to). It records nothing for any other query or page, and the internal searches of the wiki (delivery,
// placement, consultation) never call it. A search that found a candidate is not recorded here; its lookup (or CloseMiss) closes the miss.
//
// It returns the earlier miss of the same words (as it was before this search) when that one was tied to a result, so the caller can
// say where that result is; otherwise it returns nil.
func (b Book) RecordMiss(query, cursor string, view View) (*Miss, error) {
	return b.recordMissAt(query, cursor, view, time.Now().UTC())
}

func (b Book) recordMissAt(query, cursor string, view View, at time.Time) (*Miss, error) {
	q, ok := missQuery(query, cursor)
	if !ok || !view.searchFoundNothing() {
		return nil, nil
	}
	var earlier *Miss
	err := b.updateJournal(true, func(j *searchJournal) bool {
		miss, key := Miss{Query: q, At: at}, missKey(q)
		if before := lastMiss(j.Misses, key); before != nil && before.Result != "" {
			miss.Result, earlier = before.Result, before
		}
		j.Misses = append(withoutMiss(j.Misses, key), miss)
		if len(j.Misses) > missedSearchMax {
			j.Misses = j.Misses[len(j.Misses)-missedSearchMax:]
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return earlier, nil
}

// updateJournal locks the search journal of the book, reads it, lets update change it and writes it when update says it did. A book
// with no folder for the journal has nothing to update, and none is made unless create is set.
func (b Book) updateJournal(create bool, update func(*searchJournal) bool) error {
	dir, err := b.directory(create, "searches")
	if os.IsNotExist(err) && !create {
		return nil
	}
	if err != nil {
		return err
	}
	unlock, err := lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := readSearchJournal(dir)
	if err != nil {
		return err
	}
	if !update(&j) {
		return nil
	}
	return saveSearchJournal(dir, j)
}

// CloseMiss drops the miss of the given words: a search of them returned a candidate, so the gap it noted is closed (the journal
// would otherwise keep it, and a later search that found nothing again would be pointed at the result it was tied to). It writes
// nothing when the journal does not exist, when it holds no miss of these words, and for a query that is not a search of words
// (missQuery). The caller treats it as best effort.
func (b Book) CloseMiss(query string) error {
	q, ok := missQuery(query, "")
	if !ok {
		return nil
	}
	key := missKey(q)
	return b.updateJournal(false, func(j *searchJournal) bool {
		if lastMiss(j.Misses, key) == nil {
			return false
		}
		j.Misses = withoutMiss(j.Misses, key)
		return true
	})
}

// NoteMissResult ties the misses of the given words to a saved result: the id of the result of the work that had them open. A
// result tied before is replaced (the last work that searched those words and found nothing is the one reported). It writes nothing
// when the journal does not exist or holds none of the words, and it refuses a result that is not a 16-digit id, which the reader
// would drop.
func (b Book) NoteMissResult(queries []string, result string) error {
	if !idPattern.MatchString(result) {
		return errors.New("a missed search is tied to the 16-digit id of a saved result")
	}
	words := make([]string, len(queries))
	for i, q := range queries {
		words[i] = missKey(q)
	}
	return b.updateJournal(false, func(j *searchJournal) bool {
		changed := false
		for i := range j.Misses {
			if m := &j.Misses[i]; m.Result != result && slices.Contains(words, missKey(m.Query)) {
				m.Result, changed = result, true
			}
		}
		return changed
	})
}

// readJournalIfAny reads the search journal of the book; a book that has none has an empty one.
func (b Book) readJournalIfAny() (searchJournal, error) {
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return searchJournal{}, nil
	}
	if err != nil {
		return searchJournal{}, err
	}
	return readSearchJournal(dir)
}

// OpenMisses returns the misses made at since or later that are still open, newest first, at most limit of them. A miss is open
// while the same search, run now, still finds no candidate; an article added since closes it with no write. It only reads.
//
// A call examines at most openMissExamineMax misses: the newest ones that are at or after since and of an allowed form (the others
// are skipped and not counted). A miss older than those is not examined in that call, whether it is open or not, so a journal full
// of answered misses costs a bounded number of searches. The searches of one call share the parsing of the articles (ReadView:
// every read still opens the files and compares all bytes, so an article changed in between is read as it is then).
func (b Book) OpenMisses(since time.Time, limit int) ([]Miss, error) {
	j, err := b.readJournalIfAny()
	if err != nil {
		return nil, err
	}
	b = b.ReadView()
	open := []Miss{}
	examined := 0
	// the journal keeps its misses oldest first
	for i := len(j.Misses) - 1; i >= 0 && len(open) < limit && examined < openMissExamineMax; i-- {
		m := j.Misses[i]
		if _, ok := missQuery(m.Query, ""); !ok || m.At.Before(since) {
			continue
		}
		examined++
		v, err := b.Query(m.Query)
		if err != nil {
			return nil, err
		}
		if v.searchFoundNothing() {
			open = append(open, m)
		}
	}
	return open, nil
}
