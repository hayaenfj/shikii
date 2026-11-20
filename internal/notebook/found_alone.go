package notebook

import "strings"

const (
	// foundAloneExamineMax is how many distinct words of a query WordsFoundAlone searches. Each is ranked against every article of the wiki (read once for the call), so
	// the cost of a call is bounded by it, not by how long the query is.
	foundAloneExamineMax = 8
	// foundAloneMax is how many words WordsFoundAlone returns.
	foundAloneMax = 3
)

// FoundAlone is the reply field that says which words of a search of several words that found nothing would find an article if they
// were searched alone. A search of several words usually needs at least two of them in one article (two of the units it splits
// its words into), so words an agent put in as alternatives
// for one thing find nothing together although one of them finds the article. Words are those words, in the order of the query;
// the caller fills NextAction. It is a note on the search, not an answer: the articles themselves are not in it.
type FoundAlone struct {
	Words      []string `json:"words"`
	NextAction string   `json:"next_action"`
}

// WordsFoundAlone returns the words of query that find an article when searched alone, when the search of the query found nothing
// (view is its result) and it is a search of two or more words: the same allow-list as a recorded miss (missQuery: the first page of a
// search of words, not a reference), so it is asked only where a miss is recorded. Words are distinct as missKey counts them, taken
// in the order of the query as they were typed (composed), at most foundAloneExamineMax of them are searched, and at most
// foundAloneMax are returned. A word that is itself a reference (an id, lesson:<id>) is not searched.
//
// Each word is searched as a wiki query of that word is: the same pages, thresholds and discovery contexts, over one shared reading
// of the wiki (wordSearcher: the pages are read, their sources checked and their texts split once for all the words, not once for
// each). It records nothing: no lookup, no miss, no change to the journal.
func (b Book) WordsFoundAlone(query, cursor string, view View) ([]string, error) {
	q, ok := missQuery(query, cursor)
	if !ok || !view.searchFoundNothing() {
		return nil, nil
	}
	words := strings.Fields(q)
	if len(words) < 2 {
		return nil, nil
	}
	seen := map[string]bool{}
	examined := []string{}
	for _, word := range words {
		key := missKey(word)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(seen) > foundAloneExamineMax {
			break
		}
		if _, ok := missQuery(word, ""); ok {
			examined = append(examined, word)
		}
	}
	if len(examined) == 0 {
		return nil, nil
	}
	searcher, err := b.searcherFor(examined)
	if err != nil {
		return nil, err
	}
	found := []string{}
	for _, word := range examined {
		if searcher.finds(word) {
			if found = append(found, word); len(found) == foundAloneMax {
				break
			}
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found, nil
}
