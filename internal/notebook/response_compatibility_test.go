package notebook

import (
	"encoding/json"
	"fmt"
	"github.com/hayaenfj/shikii/internal/store"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestResponseSearchAndExcerptsPreserveOriginalValues(t *testing.T) {
	pieces := []string{"routing tenant", "owner", "\u65e5\u672c\u8a9e", "\u30d5\u30a9\u30fc\u30de\u30c3\u30c8", "1_a e\u0301", string([]byte{0xff}), ""}
	for _, query := range pieces {
		pages := []Page{}
		for i, text := range pieces {
			pages = append(pages, Page{ID: fmt.Sprintf("%016x", i+1), Title: text, AppliesTo: "routing tenant", Lesson: "owner condition", Body: text + " routing tenant", Rationale: "owner"})
		}
		for _, recall := range []bool{false, true} {
			got, gm := rankPages(pages, query, recall)
			want, wm := responseOriginalRankPages(pages, query, recall)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gm, wm) {
				t.Fatalf("retrieval changed: query=%q recall=%v got=%v want=%v", query, recall, gm, wm)
			}
		}
	}
	for _, body := range []string{"", "## Heading\n", "First owner condition.\nSecond tenant condition.\n", strings.Repeat("Routing owner. ", 70), "\u65e5\u672c\u8a9e\u3002\n\u30c6\u30ca\u30f3\u30c8\u3002"} {
		for _, context := range pieces {
			for _, budget := range []int{-1, 0, 1, 50, 300, 1200} {
				got, want := FocusExcerpts(body, context, budget), responseOriginalFocusExcerpts(body, context, budget)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("exact excerpts changed: budget=%d context=%q", budget, context)
				}
			}
		}
	}
}
func TestResponseSourceDecodeIdentityAndOwnership(t *testing.T) {
	book := Book{snapshot: &readSnapshot{}}
	f := store.Feedback{ID: "0000000000000001", Request: "Original request", Summary: "Original summary", Reply: "Original reply", Targets: []store.ReviewTarget{{Spec: "owner", TraceID: "original"}}}
	raw, _ := json.Marshal(f)
	first := book.decodedSource("/synthetic/source", f.ID, raw)
	first.feedback.Targets[0].TraceID = "caller mutation"
	again := book.decodedSource("/synthetic/source", f.ID, raw)
	if again.feedback.Targets[0].TraceID != "original" {
		t.Fatal("caller changed shared targets")
	}
	again.feedback.Targets[0].TraceID = "cached caller mutation"
	if third := book.decodedSource("/synthetic/source", f.ID, raw); third.feedback.Targets[0].TraceID != "original" {
		t.Fatal("cached caller changed shared targets")
	}
	f.Request = "Different request"
	changed, _ := json.Marshal(f)
	next := book.decodedSource("/synthetic/source", f.ID, changed)
	if next.feedback.Request != f.Request {
		t.Fatal("same path reused different bytes")
	}
	wrong := book.decodedSource("/synthetic/source", "0000000000000002", changed)
	if wrong.valid {
		t.Fatal("cached decode skipped source ID")
	}
	o := Observation{ID: f.ID, Kind: "test", Title: "Owner", Content: "Original condition", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)}
	observation, _ := json.Marshal(o)
	for _, data := range [][]byte{raw, observation, []byte(`{"id":"0000000000000001","request":17}`), []byte(`null`), []byte(`{"`)} {
		var oldf store.Feedback
		var oldo Observation
		oldvalid := json.Unmarshal(data, &oldf) == nil && oldf.ID == f.ID && oldf.Request != "" && oldf.Summary != "" && oldf.Reply != ""
		if !oldvalid {
			oldvalid = json.Unmarshal(data, &oldo) == nil && oldo.ID == f.ID && oldo.valid()
		}
		got := decodeSource(data, f.ID)
		if got.valid != oldvalid || !reflect.DeepEqual(got.feedback, oldf) {
			t.Fatal("source decoding validity or feedback changed")
		}
	}
	if book.snapshot.sourceDecodes.bytes > readViewLimit {
		t.Fatal("source decode retention exceeded its limit")
	}
}
func responseOriginalRankPages(pages []Page, query string, recall bool) ([]Page, []Match) {
	names, weights := []string{"title", "applies_to", "lesson", "body", "rationale", "discovery"}, []float64{4, 3, 2, 1, 2, 3}
	if recall {
		names, weights = []string{"title", "applies_to", "rationale", "discovery"}, []float64{4, 3, 2, 3}
	}
	terms := searchTerms(query)
	docs, frequency := []searchDocument{}, map[string]int{}
	for _, p := range pages {
		d := searchDocument{page: p}
		seen := map[string]bool{}
		texts := []string{p.Title, p.AppliesTo, p.Lesson, p.Body, p.Rationale, p.discovered}
		if recall {
			texts = []string{p.Title, p.AppliesTo, p.Rationale, p.discovered}
		}
		for _, text := range texts[:len(names)] {
			field := searchTerms(text)
			d.fields = append(d.fields, field)
			for term := range field {
				seen[term] = true
			}
		}
		for term := range seen {
			frequency[term]++
		}
		docs = append(docs, d)
	}
	type ranked struct {
		page  Page
		match Match
		score float64
	}
	results := []ranked{}
	// The request-side count for the automatic-delivery gate (runs of content words) does not depend on the page, so count it once before the loop (audit R4: performance)
	queryRunes := []rune(strings.ToLower(query))
	rare := func(term string) bool { return len(docs) == 1 || frequency[term]*2 <= len(docs) }
	requestContent, rareRequest := map[string]bool{}, map[string]bool{}
	for term := range terms {
		if isContentTerm(term) {
			requestContent[term] = true
			if rare(term) {
				rareRequest[term] = true
			}
		}
	}
	requestTerms, rareRequestTerms := contentClusters(queryRunes, requestContent), contentClusters(queryRunes, rareRequest)
	for _, d := range docs {
		m := Match{ID: d.page.ID, Revision: d.page.Revision, Fields: []string{}, Terms: []string{}}
		matched, score, fields := map[string]bool{}, 0.0, map[string]bool{}
		for term := range terms {
			best := 0.0
			for i, f := range d.fields {
				if f[term] {
					best = math.Max(best, weights[i])
					fields[names[i]] = true
				}
			}
			if best > 0 {
				matched[term] = true
				score += best * (1 + math.Log(1+float64(len(docs))/float64(1+frequency[term])))
			}
		}
		threshold := .18
		coverage := float64(len(matched)) / float64(max(len(terms), 1))
		minMatched := 2
		if recall {
			// Automatic-delivery gate (0.7.4): count only "terms that carry content" (identifiers, or 2-character pairs that are neither kana only nor contain a particle).
			// Pass on the request-side match rate (0.35 of content words) or the page-side match rate (0.15 of content words in title and applies-to), both with 2 or more content words.
			// A rare identifier (CHANGELOG, Makefile: an alphabetic word found in at most half the pages) passes even alone. An identifier found in every page,
			// such as the project name, does not pass alone (in a wiki with a single page there is nothing to compare, so it passes).
			// Up to 0.7.3 the gate used the share of all words in the request: long requests got nothing, and short ones got unrelated pages through ubiquitous words and grammatical pairs.
			// "Rare word" = a word found in at most half the pages (all of them if there is one page). A word found in every page, such as the project name,
			// is not evidence even when it matches (it alone delivered unrelated pages). But if almost all (0.8 or more) of the request's content words match the page,
			// deliver even with ubiquitous words (when every page in the wiki is on the same topic and a request on that topic comes in).
			rarePageTerms := 0
			for _, f := range d.fields {
				for term := range f {
					if isContentTerm(term) && rare(term) {
						rarePageTerms++
					}
				}
			}
			strong := false
			matchedContentSet, rareMatchedSet := map[string]bool{}, map[string]bool{}
			for term := range matched {
				if !isContentTerm(term) {
					continue
				}
				matchedContentSet[term] = true
				if rare(term) {
					rareMatchedSet[term] = true
					if isIdentifierTerm(term) {
						strong = true
					}
				}
			}
			matchedContent, rareMatched := contentClusters(queryRunes, matchedContentSet), contentClusters(queryRunes, rareMatchedSet)
			// A short one- or two-word request delivers for a kana-free title match; an applies-to-only match does not deliver.
			titleRare := map[string]bool{}
			for term := range rareMatchedSet {
				if d.fields[0][term] {
					titleRare[term] = true
				}
			}
			strongTitle := requestTerms <= 2 && hasKanaFreeCluster(queryRunes, titleRare)
			coverage, minMatched = 0, 2
			switch {
			case strongTitle, strong && (requestTerms <= 3 || (rareRequestTerms > 0 && float64(rareMatched)/float64(rareRequestTerms) >= .35)):
				// A single rare identifier delivers only when the request has few content words, or the request-side match rate is enough.
				// A single word like hook inside a long request delivered an unrelated page (measured 2026-10-23)
				coverage, minMatched = threshold, 1
			case rareMatched >= 2 && ((rareRequestTerms > 0 && float64(rareMatched)/float64(rareRequestTerms) >= .35) || (rarePageTerms > 0 && float64(rareMatched)/float64(rarePageTerms) >= .15)):
				coverage = threshold
			case matchedContent >= 2 && requestTerms > 0 && float64(matchedContent)/float64(requestTerms) >= .8:
				coverage = threshold
			}
			if len(matched) < minMatched || coverage < threshold {
				continue
			}
		} else if len(terms) == 0 || (len(terms) > 1 && len(matched) < minMatched) || coverage < threshold {
			continue
		}
		for term := range matched {
			m.Terms = append(m.Terms, term)
		}
		sort.Strings(m.Terms)
		for _, name := range names {
			if fields[name] {
				m.Fields = append(m.Fields, name)
			}
		}
		results = append(results, ranked{d.page, m, score / float64(len(terms))})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	out, matches := []Page{}, []Match{}
	for _, r := range results {
		out = append(out, r.page)
		matches = append(matches, r.match)
	}
	return out, matches
}

func responseOriginalFocusExcerpts(text, context string, budget int) []TextExcerpt {
	type passage struct {
		TextExcerpt
		score int
	}
	terms := searchTerms(context)
	passages := []passage{}
	// Sentence and line boundaries keep exact quotes understandable. A unit
	// too large for the budget is omitted, with full recovery left to the caller.
	for _, unit := range excerptUnits(text) {
		start, end := unit[0], unit[1]
		for start < end && (text[start] == ' ' || text[start] == '\n' || text[start] == '\t') {
			start++
		}
		for end > start && (text[end-1] == ' ' || text[end-1] == '\n' || text[end-1] == '\t') {
			end--
		}
		chunk := text[start:end]
		if chunk == "" || strings.HasPrefix(strings.TrimSpace(chunk), "#") {
			continue
		}
		score := 0
		for term := range searchTerms(chunk) {
			if terms[term] && isContentTerm(term) {
				score++
			}
		}
		passages = append(passages, passage{TextExcerpt{start, end, chunk}, score})
	}
	sort.SliceStable(passages, func(i, j int) bool { return passages[i].score > passages[j].score })
	out := []TextExcerpt{}
	for _, p := range passages {
		if budget == 0 || len(out) == 3 {
			break
		}
		if len(p.Text) > budget {
			continue
		}
		out = append(out, p.TextExcerpt)
		budget -= len(p.Text)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
