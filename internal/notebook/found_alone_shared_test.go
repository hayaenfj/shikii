package notebook

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"
)

// sharedSearchWords are the words the articles of sharedSearchBook are made of, by the kind of place the search finds them in.
type sharedSearchWords struct {
	plain      []string // words of letters and digits, in the lessons, the bodies and the conditions, in a case of the draw
	japanese   []string // kanji, katakana and hiragana words
	titleOnly  []string // each is in the title of one article and nowhere else
	discovered []string // each is only in the discovery context of one article
	rare       []string // each is in the body of one article
	absent     []string // in no article
}

func (w sharedSearchWords) all() []string {
	var out []string
	for _, group := range [][]string{w.plain, w.japanese, w.titleOnly, w.discovered, w.rare, w.absent} {
		out = append(out, group...)
	}
	return out
}

// sharedSearchBook is a project of articles drawn from a seeded source: every word of the kinds above is placed where its kind says.
func sharedSearchBook(t *testing.T, rng *rand.Rand) (Book, sharedSearchWords) {
	t.Helper()
	words := sharedSearchWords{
		plain:      []string{"quartz", "saffron", "lantern", "juniper", "marigold", "harbor", "kestrel", "walnut", "export2", "v2api", "parser"},
		japanese:   []string{"\u8acb\u6c42", "\u671f\u9650", "\u9001\u4ed8", "\u66f8\u985e", "\u76e3\u67fb", "\u30c6\u30b9\u30c8", "\u30c7\u30fc\u30bf", "\u3072\u3089\u304c\u306a"},
		titleOnly:  []string{"beacon", "anchor", "rudder", "compass", "sextant", "capstan"},
		discovered: []string{"periwinkle", "tamarind"},
		rare:       []string{"zephyrine", "orrery"},
		absent:     []string{"zzabsent", "qqmissing", "\u672a\u53ce\u9332\u8a9e", "xyloph"},
	}
	b := Book{Root: t.TempDir(), Scope: "/work/shared-search"}
	o, err := b.capture(Observation{Kind: "test", Title: "Synthetic source", Content: "A source for the articles.", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	cased := func(word string) string { // the case a draw gives a word: as it is, capitalized or upper
		switch rng.Intn(3) {
		case 0:
			return strings.ToUpper(word)
		case 1:
			return strings.ToUpper(word[:1]) + word[1:]
		}
		return word
	}
	pick := func(group []string, n int) []string {
		out := []string{}
		for i := 0; i < n; i++ {
			out = append(out, group[rng.Intn(len(group))])
		}
		return out
	}
	var pages []Page
	for i := 0; i < 12; i++ {
		title := "Notes " + cased(pick(words.plain, 1)[0])
		if i < len(words.titleOnly) {
			title = "Notes " + words.titleOnly[i] // a title word is in the title of one article only
		}
		var lesson, body strings.Builder
		for _, w := range pick(words.plain, 4+rng.Intn(4)) {
			lesson.WriteString(cased(w) + " ")
		}
		for _, w := range pick(words.japanese, 2+rng.Intn(2)) {
			lesson.WriteString(w + "\u306e")
		}
		lesson.WriteString("\u78ba\u8a8d\u3002")
		for _, w := range pick(words.plain, 6+rng.Intn(5)) {
			body.WriteString(w + " ")
		}
		if i < len(words.rare) {
			body.WriteString(words.rare[i])
		}
		text := body.String()
		page, err := b.Ingest(Draft{Title: title, Lesson: lesson.String(), AppliesTo: strings.Join(pick(words.plain, 1+rng.Intn(2)), " "), Body: &text, Kind: "howto", Scene: "reading notes", Sources: []string{o.ID}})
		if err != nil {
			t.Fatal(i, err)
		}
		pages = append(pages, *page.Page)
	}
	for i, word := range words.discovered {
		lookup, err := b.RecordLookup("a lookup of the article", []Page{pages[i]})
		if err != nil || lookup == nil {
			t.Fatal(err)
		}
		if err := b.Discover(Discovery{LookupID: lookup.ID, PageID: pages[i].ID, Revision: pages[i].Revision, Context: "where " + word + " was used", Decision: "Used the article", Evidence: "The check passed"}); err != nil {
			t.Fatal(err)
		}
	}
	return b, words
}

// foundByTheQuery is whether b.query of the word (the search of a wiki query) finds an article: what each word must agree with.
func foundByTheQuery(t *testing.T, b Book, word string) bool {
	t.Helper()
	v, err := b.ReadView().scopedRead().query(word, false)
	if err != nil {
		t.Fatal(err)
	}
	return len(v.Lessons) > 0 || v.Search != nil && v.Search.Total > 0
}

// A word searched through the shared reading finds an article exactly when a wiki query of the word does, for a seeded mix of articles
// and words: words of letters and digits in any case, kanji and kana, a word only in a title, a word only in a discovery context, a
// word in one article, and words in no article. The counts of what the draw produced are checked, so that an agreement of two empty
// answers cannot pass.
func TestWordsOfASharedReadingFindWhatTheWikiQueryOfEachFinds(t *testing.T) {
	rng := rand.New(rand.NewSource(20261120))
	b, words := sharedSearchBook(t, rng)
	groups := map[string][]string{"plain": words.plain, "japanese": words.japanese, "title-only": words.titleOnly, "discovered-only": words.discovered, "rare": words.rare, "absent": words.absent}
	asked := []string{}
	for _, group := range groups {
		for _, word := range group {
			asked = append(asked, word, strings.ToUpper(word), strings.ToUpper(word[:1])+word[1:])
		}
	}
	slices.Sort(asked)
	searcher, err := b.searcherFor(asked)
	if err != nil {
		t.Fatal(err)
	}
	found, notFound := map[string]int{}, map[string]int{}
	for name, group := range groups {
		for _, word := range group {
			for _, typed := range []string{word, strings.ToUpper(word), strings.ToUpper(word[:1]) + word[1:]} {
				want := foundByTheQuery(t, b, typed)
				if got := searcher.finds(typed); got != want {
					t.Fatalf("%s %q: the shared reading says %v, the wiki query says %v", name, typed, got, want)
				}
				if want {
					found[name]++
				} else {
					notFound[name]++
				}
			}
		}
	}
	for _, name := range []string{"plain", "japanese", "title-only", "discovered-only", "rare"} {
		if found[name] == 0 {
			t.Fatalf("the draw has words of kind %s that the wiki query finds: found %v, not found %v", name, found, notFound)
		}
	}
	if notFound["absent"] == 0 || found["absent"] != 0 {
		t.Fatalf("the words in no article are not found: found %v, not found %v", found, notFound)
	}
	t.Logf("found %v, not found %v", found, notFound)
}

// WordsFoundAlone returns, for a search of random words, the words among the first eight different ones that a wiki query finds, at
// most three, in the order searched. The draw has to produce answers that name words and answers that name none.
func TestWordsFoundAloneAgreesWithTheWikiQueryOfRandomWords(t *testing.T) {
	rng := rand.New(rand.NewSource(20261121))
	b, words := sharedSearchBook(t, rng)
	pool := words.all()
	named, empty, capped, some := 0, 0, 0, 0
	_ = named
	for trial := 0; trial < 160; trial++ {
		typed := []string{}
		silent := rng.Float64() // how many of the words of this search are in no article
		for i, n := 0, 2+rng.Intn(10); i < n; i++ {
			word := pool[rng.Intn(len(pool))]
			if rng.Float64() < silent {
				word = words.absent[rng.Intn(len(words.absent))]
			}
			if rng.Intn(3) == 0 {
				word = strings.ToUpper(word)
			}
			typed = append(typed, word)
		}
		query := strings.Join(typed, " ")
		want, seen := []string{}, map[string]bool{}
		for _, word := range strings.Fields(normalizeMissQuery(query)) {
			if seen[missKey(word)] {
				continue
			}
			seen[missKey(word)] = true
			if len(seen) > 8 {
				break
			}
			if foundByTheQuery(t, b, word) && len(want) < 3 {
				want = append(want, word)
			}
		}
		got, err := b.WordsFoundAlone(query, "", emptyView())
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Fatalf("%q: the words that find an article alone are %q, the wiki query of each word says %q", query, got, want)
		}
		switch {
		case len(want) == 0:
			empty++
		case len(want) == 3:
			capped++
		default:
			some++
		}
		named = capped + some
	}
	if some < 10 || empty < 10 || capped < 10 {
		t.Fatalf("the draw produced %d answers that name one or two words, %d that name three (cut) and %d that name none", some, capped, empty)
	}
	t.Logf("named one or two %d, three %d, none %d", some, capped, empty)
}

// A search of several words splits the texts of each article once, however many words it searches: the count of the articles split
// is the measure, so this does not depend on how fast a machine is. The control is a wiki query of each word, which splits them for
// every word.
func TestWordsFoundAloneSplitsEachArticleOnce(t *testing.T) {
	b := foundAloneBook(t,
		[3]string{"Quartz export workflow", "Check Quartz exports with the raw parser.", "Quartz exports"},
		[3]string{"Saffron lantern notes", "Light the saffron lanterns after the audit.", "lanterns"},
		[3]string{"Juniper audit", "The audit of the juniper meadow records.", "audit"},
		[3]string{"Release checklist", "Update the CHANGELOG before the release.", "release"},
		[3]string{"Marigold canal", "The marigold canal was drained.", "canal"},
	)
	const articles = 5
	words := "alphaword betaword gammaword deltaword epsilonword zetaword etaword thetaword iotaword kappaword"
	before := pageTokenizations.Load()
	for _, word := range strings.Fields(words)[:5] {
		if foundByTheQuery(t, b, word) {
			t.Fatalf("control: %q is in no article", word)
		}
	}
	if split := pageTokenizations.Load() - before; split != 5*articles {
		t.Fatalf("control: a wiki query of each of five words splits each of the %d articles five times, got %d", articles, split)
	}
	for _, c := range []struct {
		name, query string
	}{
		{"five words that find nothing", strings.Join(strings.Fields(words)[:5], " ")},
		{"ten words (eight are searched)", words},
		{"words of which some find an article", "quartz lanterns alphaword juniper canal"},
	} {
		before = pageTokenizations.Load()
		if _, err := b.WordsFoundAlone(c.query, "", emptyView()); err != nil {
			t.Fatal(err)
		}
		if split := pageTokenizations.Load() - before; split != articles {
			t.Fatalf("%s: each of the %d articles is split once for all the words, got %d", c.name, articles, split)
		}
	}
}

// OpenMisses lists, for a seeded mix of misses over the articles, the newest misses (at most 16 examined, distinct words) for which
// a wiki query of their words finds nothing: it answers as a wiki query of each does. The draw has to produce misses that are
// answered by an article and misses that are still open.
func TestOpenMissesAgreeWithTheWikiQueryOfEachMiss(t *testing.T) {
	rng := rand.New(rand.NewSource(20261122))
	b, words := sharedSearchBook(t, rng)
	pool := words.all()
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	answered, open := 0, 0
	for trial := 0; trial < 40; trial++ {
		queries := []string{}
		for i, n := 0, 1+rng.Intn(3); i < n; i++ {
			word := pool[rng.Intn(len(pool))]
			if rng.Intn(5) < 2 {
				word = words.absent[rng.Intn(len(words.absent))]
			}
			queries = append(queries, word)
		}
		query := strings.Join(queries, " ")
		if _, err := b.recordMissAt(query, "", "", emptyView(), base.Add(time.Duration(trial)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := b.readJournalIfAny()
	if err != nil {
		t.Fatal(err)
	}
	want, seen, examined := []string{}, map[string]bool{}, 0
	for i := len(journal.Misses) - 1; i >= 0 && examined < 16; i-- {
		key := missKey(journal.Misses[i].Query)
		if seen[key] {
			continue
		}
		seen[key] = true
		examined++
		if foundByTheQuery(t, b, journal.Misses[i].Query) {
			answered++
		} else {
			open++
			want = append(want, journal.Misses[i].Query)
		}
	}
	got, err := b.OpenMisses(time.Time{}, 64, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(openQueries(got), want) {
		t.Fatalf("the open misses are those a wiki query of their words does not answer: %q, want %q", openQueries(got), want)
	}
	if answered < 3 || open < 6 || len(got) != open {
		t.Fatalf("the draw has misses answered by an article (%d) and misses that are still open (%d), and lists %d", answered, open, len(got))
	}
	t.Logf("answered %d, open %d (journal of %d misses)", answered, open, len(journal.Misses))
}

// The misses a call examines are searched over one reading of the wiki: each article is split once for all of them, however many
// misses are examined (the count of the articles split is the measure).
func TestOpenMissesSplitEachArticleOnce(t *testing.T) {
	b := foundAloneBook(t,
		[3]string{"Quartz export workflow", "Check Quartz exports with the raw parser.", "Quartz exports"},
		[3]string{"Saffron lantern notes", "Light the saffron lanterns after the audit.", "lanterns"},
		[3]string{"Juniper audit", "The audit of the juniper meadow records.", "audit"},
	)
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i, query := range []string{"alphaword betaword", "gammaword deltaword", "epsilonword zetaword", "quartz exports", "etaword thetaword", "iotaword kappaword"} {
		if _, err := b.recordMissAt(query, "", "", emptyView(), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	before := pageTokenizations.Load()
	got, err := b.OpenMisses(time.Time{}, 16, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("five of the six misses are open (the one of quartz exports is answered): %q", openQueries(got))
	}
	if split := pageTokenizations.Load() - before; split != 3 {
		t.Fatalf("each of the 3 articles is split once for the six misses, got %d", split)
	}
}
