package main

import (
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The four texts an orientation repeats from its candidate, with the field of a shortened candidate that holds each.
var orientationRepeated = []struct{ orientation, candidate string }{
	{"id", "id"}, {"revision", "revision"}, {"summary", "summary"}, {"conditions", "applies_to"},
}

// The parts of an orientation that are never left out, empty lists included.
var orientationAlways = []string{"current", "history", "recheck", "counterexamples", "unclassified", "needs_interpretation", "premise_changed", "omitted"}

// orientationPage is an article with one interpreted passage; repeat sizes its body.
func orientationPage(repeat int) notebook.Page {
	return notebook.Page{ID: "00000000000000a1", Revision: "00000000000000b2", Title: "Routing owner", Lesson: "Ask the owner before publishing a routed article.", AppliesTo: "When a routed article is about to be published.", Rationale: "The owner answers for what is published.",
		Body:     "Owner approval is required before publishing.\n\nKeep the unresolved exception open.\n\n" + strings.Repeat("Background of the routing rule, read the source first. ", repeat),
		Passages: []notebook.KnowledgePassage{{Role: "current", Quote: "Owner approval is required before publishing.", Conditions: "Routed articles only.", Reason: "Checked against the routing source."}}}
}

func orientationTexts(p notebook.Page) map[string]string {
	return map[string]string{"id": p.ID, "revision": p.Revision, "summary": p.Lesson, "conditions": p.AppliesTo}
}

// shortenedByLimit is a candidate as limitWorkPreparation shortens it when the reply does not fit: its own summary, applies_to, id
// and revision, and the orientation it binds to itself.
func shortenedByLimit(t *testing.T, p notebook.Page) map[string]any {
	t.Helper()
	e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "article": p, "body_included": true, "query": map[string]any{"operation": "query", "query": "lesson:" + p.ID + "@" + p.Revision}}
	bindOrientation(e, notebook.Orient(p, "owner approval", 2400))
	reply := map[string]any{"knowledge": []any{e}}
	if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{"knowledge1": p.Body}, workMinBudget); err != nil {
		t.Fatal(err)
	}
	if e["attention_limited"] != true || e["summary"] != p.Lesson || e["article"] != nil {
		t.Fatalf("precondition: the reply does not fit, so the candidate is in its shortened form: %v", e)
	}
	return e
}

func jsonOf(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func orientationOfJSON(t *testing.T, e map[string]any) map[string]any {
	t.Helper()
	o, ok := jsonOf(t, e)["orientation"].(map[string]any)
	if !ok {
		t.Fatalf("the candidate has an orientation: %v", e)
	}
	return o
}

// writtenTimes counts how often text is written in the candidate, as a whole JSON string.
func writtenTimes(t *testing.T, e map[string]any, text string) int {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(text)
	return strings.Count(string(raw), string(quoted))
}

// assertOrientationAddsNoCopy: each text the candidate holds outside its orientation is written as often as it is without the
// orientation (the orientation adds no second copy), and each of them is held at least once.
func assertOrientationAddsNoCopy(t *testing.T, e map[string]any, p notebook.Page) {
	t.Helper()
	without := maps.Clone(e)
	delete(without, "orientation")
	for key, text := range orientationTexts(p) {
		held, with := writtenTimes(t, without, text), writtenTimes(t, e, text)
		if held < 1 || with != held {
			t.Errorf("%s %q: the candidate holds it %d times and writes it %d times with its orientation", key, text, held, with)
		}
	}
	o := orientationOfJSON(t, e)
	for _, key := range []string{"id", "revision", "summary", "conditions"} {
		if _, there := o[key]; there {
			t.Errorf("the orientation repeats %s", key)
		}
	}
	for _, key := range orientationAlways {
		if _, there := o[key]; !there {
			t.Errorf("the orientation keeps %s, empty or not", key)
		}
	}
}

// a: a shortened candidate holds the four texts itself, so its orientation does not repeat them.
func TestOrientationOmitsTheTextsAShortenedCandidateHolds(t *testing.T) {
	p := orientationPage(400)
	e := shortenedByLimit(t, p)
	assertOrientationAddsNoCopy(t, e, p)
	whole := jsonOf(t, e)
	for key, want := range map[string]string{"id": p.ID, "revision": p.Revision, "summary": p.Lesson, "applies_to": p.AppliesTo} {
		if whole[key] != want {
			t.Errorf("the candidate keeps its own %s: %v", key, whole[key])
		}
	}
	if cur, _ := orientationOfJSON(t, e)["current"].([]any); len(cur) != 1 {
		t.Errorf("the interpreted passage stays in the orientation: %v", cur)
	}
}

// b: a text that leaves the candidate after the orientation was made is written by the orientation again, and only that one.
func TestOrientationWritesATextOnceTheCandidateNoLongerHoldsIt(t *testing.T) {
	p := orientationPage(400)
	texts := orientationTexts(p)
	for _, c := range orientationRepeated {
		for _, how := range []string{"deleted", "emptied", "not a string"} {
			t.Run(c.orientation+" "+how, func(t *testing.T) {
				e := shortenedByLimit(t, p)
				switch how {
				case "deleted":
					delete(e, c.candidate)
				case "emptied":
					e[c.candidate] = ""
				case "not a string":
					e[c.candidate] = 7
				}
				o := orientationOfJSON(t, e)
				if o[c.orientation] != texts[c.orientation] {
					t.Fatalf("the candidate no longer holds the %s, so the orientation writes it: %v", c.orientation, o[c.orientation])
				}
				for _, other := range orientationRepeated {
					if _, there := o[other.orientation]; there != (other.orientation == c.orientation) {
						t.Errorf("only %s comes back, %s written: %v", c.orientation, other.orientation, there)
					}
				}
			})
		}
	}
}

// c: a text that differs from the candidate's own is written by both.
func TestOrientationKeepsATextTheCandidateWritesDifferently(t *testing.T) {
	p := orientationPage(400)
	texts := orientationTexts(p)
	for _, c := range orientationRepeated {
		for name, change := range map[string]func(string) string{
			"another text":  func(s string) string { return "A different " + s },
			"one byte less": func(s string) string { return s[:len(s)-1] },
			"one byte more": func(s string) string { return s + "." },
		} {
			t.Run(c.orientation+" "+name, func(t *testing.T) {
				e := shortenedByLimit(t, p)
				own := change(texts[c.orientation])
				e[c.candidate] = own
				whole := jsonOf(t, e)
				o := whole["orientation"].(map[string]any)
				if o[c.orientation] != texts[c.orientation] || whole[c.candidate] != own {
					t.Fatalf("both the candidate's %q and the orientation's %q are written: %v / %v", own, texts[c.orientation], whole[c.candidate], o[c.orientation])
				}
			})
		}
	}
}

// walkStrings collects every string value of a decoded JSON value.
func walkStrings(v any, into map[string]bool) {
	switch v := v.(type) {
	case string:
		into[v] = true
	case []any:
		for _, x := range v {
			walkStrings(x, into)
		}
	case map[string]any:
		for _, x := range v {
			walkStrings(x, into)
		}
	}
}

// d: whatever the candidate holds, the strings a reader finds in it are the same before and after the orientation leaves a text out,
// and no other part of the orientation changes.
func TestOrientationOmissionLosesNoText(t *testing.T) {
	p := orientationPage(40)
	whole := notebook.Orient(p, "owner approval", 300)
	short := func() map[string]any {
		return map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "body_included": false}
	}
	forms := []struct {
		name   string
		build  func() map[string]any
		shrink bool // some text is held by the candidate, so the orientation gets smaller
	}{
		{"shortened", short, true},
		{"article", func() map[string]any {
			return map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "article": p, "body_included": true}
		}, true},
		{"article without top-level id and revision", func() map[string]any { return map[string]any{"ref": "knowledge1", "article": p} }, true},
		{"shortened without summary", func() map[string]any { e := short(); delete(e, "summary"); return e }, true},
		{"shortened with another summary", func() map[string]any { e := short(); e["summary"] = "Another summary."; return e }, true},
		{"article of another lesson", func() map[string]any {
			q := p
			q.Lesson, q.AppliesTo = "Another lesson.", "Another condition."
			return map[string]any{"ref": "knowledge1", "id": "0000000000000099", "revision": "0000000000000098", "article": q}
		}, false},
		{"passages only (a brief read)", func() map[string]any {
			return map[string]any{"ref": "knowledge1", "body_included": false, "text": "Exact passages."}
		}, false},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			before := f.build()
			before["orientation"] = whole
			after := f.build()
			bindOrientation(after, whole)
			bs, as := map[string]bool{}, map[string]bool{}
			walkStrings(jsonOf(t, before), bs)
			walkStrings(jsonOf(t, after), as)
			if !maps.Equal(bs, as) {
				var lost []string
				for s := range bs {
					if !as[s] {
						lost = append(lost, s)
					}
				}
				t.Fatalf("strings the candidate no longer carries: %q", lost)
			}
			bo, ao := jsonOf(t, before)["orientation"].(map[string]any), jsonOf(t, after)["orientation"].(map[string]any)
			for _, key := range []string{"id", "revision", "summary", "conditions"} {
				delete(bo, key)
				delete(ao, key)
			}
			if !reflect.DeepEqual(bo, ao) {
				t.Fatalf("the rest of the orientation is unchanged: %v / %v", bo, ao)
			}
			b, a := workContextSize(before), workContextSize(after)
			if a > b || f.shrink && a >= b {
				t.Fatalf("the written bytes go from %d to %d (smaller when a text is held by the candidate, never larger)", b, a)
			}
		})
	}
}

// The JSON of KnowledgeOrientation itself, and what a candidate that holds none of its texts writes, are as before.
func TestOrientationIsWrittenWholeWhenTheCandidateHoldsNothingOfIt(t *testing.T) {
	p := orientationPage(40)
	whole := notebook.Orient(p, "owner approval", 300)
	want, err := json.Marshal(whole)
	if err != nil {
		t.Fatal(err)
	}
	for name, e := range map[string]map[string]any{"empty": {}, "nil holder": nil, "other fields": {"ref": "knowledge1", "text": p.Lesson + "!"}} {
		bound := &workOrientation{KnowledgeOrientation: whole, holder: e}
		got, err := json.Marshal(bound)
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: the orientation is written byte for byte as KnowledgeOrientation: %s / %s (%v)", name, got, want, err)
		}
	}
	if got, _ := json.Marshal(whole); string(got) != string(want) || strings.Contains(string(got), `"id":null`) {
		t.Error("KnowledgeOrientation itself is written as before")
	}
}

// e: the article form (a candidate that carries its article page), through the real preparation.
func TestPreparedArticleCandidateOmitsTheTextsItsArticleHolds(t *testing.T) {
	b, source := loopBook(t)
	page := loopPage(t, b, source, map[string]any{"title": "Routing owner", "lesson": "Ask the owner before publishing a routed article.", "applies_to": "When a routed article is about to be published.", "body": "Owner approval is required before publishing.\n\nKeep the exception open."})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check the routing owner approval.", "query": "Routing owner"})
	if err != nil {
		t.Fatal(err)
	}
	entry := loopObject(t, first["knowledge"].([]any)[0])
	article, _ := entry["article"].(map[string]any)
	if article["lesson"] != page.Lesson || article["applies_to"] != page.AppliesTo || entry["body_included"] != true {
		t.Fatalf("precondition: the candidate carries its article page: %v", entry)
	}
	assertOrientationAddsNoCopy(t, entry, page)

	// the same candidate through the extension of a receipt (prepare with a query on the receipt)
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision})
	if err != nil {
		t.Fatal(err)
	}
	entry = loopObject(t, next["knowledge"].([]any)[0])
	if entry["article"] == nil {
		t.Fatalf("precondition: the extension carries the article page: %v", entry)
	}
	assertOrientationAddsNoCopy(t, entry, page)
}

// e: the shortened form, through the real preparation with the smallest response bound.
func TestPreparedShortCandidateOmitsTheTextsItHolds(t *testing.T) {
	b, source := loopBook(t)
	page := loopPage(t, b, source, map[string]any{"title": "Delta routing", "lesson": strings.Repeat("Retain the owner condition. ", 25), "applies_to": strings.Repeat("When checking the routing owner condition. ", 10), "body": strings.Repeat("Routing has a condition.\n\n", 400)})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check routing.", "query": "Delta routing", "max_bytes": workMinBudget})
	if err != nil {
		t.Fatal(err)
	}
	entry := loopObject(t, first["knowledge"].([]any)[0])
	if entry["attention_limited"] != true || entry["summary"] != page.Lesson || entry["article"] != nil {
		t.Fatalf("precondition: the smallest bound shortens the candidate: %v", entry)
	}
	assertOrientationAddsNoCopy(t, entry, page)
	raw, _ := json.Marshal(first)
	if len(raw) > workMinBudget {
		t.Errorf("the reply still fits its bound: %d bytes", len(raw))
	}
}

// e: a page of the memory upgrade carries its article, and its orientation does not repeat it.
func TestPreparedUpgradePageOmitsTheTextsItsArticleHolds(t *testing.T) {
	repo, root, b := upgradeBatchFixture(t, 1)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := v["memory_upgrade"].(map[string]any)["pages"].([]any)
	if len(pages) != 1 {
		t.Fatalf("precondition: one page waits: %d", len(pages))
	}
	page := loopObject(t, pages[0])
	article, _ := page["article"].(map[string]any)
	if article == nil || page["orientation"] == nil {
		t.Fatalf("precondition: the page carries its article and an orientation: %v", page)
	}
	p := notebook.Page{ID: article["id"].(string), Revision: article["revision"].(string), Lesson: article["lesson"].(string), AppliesTo: article["applies_to"].(string)}
	// a page of the upgrade has no top-level id: the article is the one place that holds the four texts
	assertOrientationAddsNoCopy(t, page, p)
}

// A candidate and its orientation are written against each other wherever they are copied: a clone is measured and written as the
// clone it is, and a clone that takes the place of its original is written against the original's map.
func TestOrientationFollowsACandidateThatIsCopied(t *testing.T) {
	p := orientationPage(40)
	e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "article": p}
	bindOrientation(e, notebook.Orient(p, "owner approval", 300))
	clone := maps.Clone(e)
	delete(clone, "article")
	// not rebound: the clone's orientation still reads the original, which holds the article
	if _, there := jsonOf(t, clone)["orientation"].(map[string]any)["summary"]; there {
		t.Fatal("control: an orientation is written against the map it was bound to")
	}
	rebindOrientation(clone)
	if o := jsonOf(t, clone)["orientation"].(map[string]any); o["summary"] != p.Lesson || o["conditions"] != p.AppliesTo {
		t.Fatalf("a clone without the article writes the texts again: %v", o)
	}
	clear(e)
	maps.Copy(e, clone)
	rebindOrientation(e)
	if o := jsonOf(t, e)["orientation"].(map[string]any); o["summary"] != p.Lesson {
		t.Fatalf("the original that took the clone's fields is written against itself: %v", o)
	}
	if _, ok := orientationOf(e["orientation"]); !ok {
		t.Fatal("the whole orientation stays readable from a bound candidate")
	}
	if o, ok := orientationOf(notebook.Orient(p, "owner approval", 300)); !ok || o.Summary != p.Lesson {
		t.Fatal("and from one as Orient made it")
	}
	if _, ok := orientationOf(map[string]any{}); ok {
		t.Fatal("and from nothing else")
	}
}

// The body of a candidate is reused only when that saves more than 512 real bytes: the reused candidate is measured as it will be
// written, once its article is gone and its orientation writes the texts the article held. Over sizes of the body, the lesson and
// the condition, each reuse saves what it is measured to save, and the candidate that took the reused fields is written against
// itself (the real saving depends on the body alone: the lesson and the condition are written once on either side).
func TestWorkRepetitionReusesABodyOnlyWhenItSavesTheBytesItIsWrittenWith(t *testing.T) {
	req := workRequest{Receipt: "receipt", Query: "routing owner"}
	reused, kept := 0, 0
	for body := 0; body <= 8; body++ {
		for _, size := range []int{0, 150, 300, 600, 900} {
			p := orientationPage(body)
			p.Lesson, p.AppliesTo = strings.Repeat("l", size)+" lesson.", strings.Repeat("c", size)+" condition."
			build := func() map[string]any {
				e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "article": p, "body_included": true}
				bindOrientation(e, notebook.Orient(p, "owner approval", 300))
				return e
			}
			w := &workState{}
			prepareWorkLoop(notebook.Book{}, w, req, []any{build()}, map[string]string{}, "task", "")
			e := build()
			written := workContextSize(e)
			prepareWorkLoop(notebook.Book{}, w, req, []any{e}, map[string]string{}, "task", "")
			if e["reused"] != true {
				kept++
				continue
			}
			reused++
			if saved := written - workContextSize(e); saved <= 512 {
				t.Fatalf("body %d, lesson and condition of %d bytes: the body is reused but the candidate goes from %d to %d bytes written", body, size, written, workContextSize(e))
			}
			// the candidate that took the fields of its clone is written against itself, not against the clone
			delete(e, "id")
			if got := jsonOf(t, e)["orientation"].(map[string]any)["id"]; got != p.ID {
				t.Fatalf("body %d, size %d: the id left the candidate, so its orientation writes it: %v", body, size, got)
			}
		}
	}
	if reused == 0 || kept == 0 {
		t.Fatalf("the sizes cover both outcomes: %d reused, %d kept whole", reused, kept)
	}
}

// The hash that tells a repeated delivery of passages from a new one is that of the candidate with its orientation as Orient made it.
func TestWorkRepetitionHashesPassagesWithTheWholeOrientation(t *testing.T) {
	p := orientationPage(40)
	build := func(bind bool) map[string]any {
		e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "body_included": false, "body_excerpts": []notebook.TextExcerpt{{Start: 0, End: 5, Text: "Owner"}}}
		if bind {
			bindOrientation(e, notebook.Orient(p, "owner approval", 300))
		} else {
			e["orientation"] = notebook.Orient(p, "owner approval", 300)
		}
		return e
	}
	w := &workState{}
	prepareWorkLoop(notebook.Book{}, w, workRequest{Receipt: "receipt", Query: "routing owner"}, []any{build(true)}, map[string]string{}, "task", "")
	if got, want := w.Loop.Excerpts[p.ID+"@"+p.Revision], workHash(build(false)); got == "" || got != want {
		t.Fatalf("the saved hash of the delivered passages is %q, that of the whole candidate %q", got, want)
	}
}

// f: the hashes the delivery compares ("is this what was delivered before?") are those of the orientation as Orient made it. The
// literal values were computed by the code before the orientation left any text out.
func TestOrientationHashesAreThoseOfTheWholeOrientation(t *testing.T) {
	p := orientationPage(40)
	whole := notebook.Orient(p, "owner approval", 300)
	build := func() map[string]any {
		return map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "body_included": false}
	}
	plain, bound := build(), build()
	plain["orientation"] = whole
	bindOrientation(bound, whole)
	if got := jsonOf(t, bound)["orientation"].(map[string]any); got["summary"] != nil {
		t.Fatal("control: this candidate holds its summary, so the orientation writes none")
	}
	const signature = "fbd2e4149840e70a8c35c139acbd751afc096008bf85b9cd3915a2f90681d103"
	const entryHash = "73f85b72561275dbdb5d05499bddc878797f56d7f8efc67a1b1d2816bfc0dde1"
	for name, e := range map[string]map[string]any{"as Orient made it": plain, "bound to its candidate": bound} {
		if got, ok := workMetadataSignature(e); !ok || got != signature {
			t.Errorf("%s: the delivery signature is %s (%v)", name, got, ok)
		}
		if got := workHash(wholeOrientationEntry(e)); got != entryHash {
			t.Errorf("%s: the hash of the whole candidate is %s", name, got)
		}
	}
	if workHash(bound) == entryHash {
		t.Error("control: the hash of the candidate as written differs, since it leaves texts out")
	}
}

// The metadata of a delivered candidate is referenced from the next delivery when the candidate is the same, with an orientation
// bound to its candidate as well.
func TestWorkDeltaReusesMetadataOfBoundCandidates(t *testing.T) {
	p := orientationPage(40)
	p.Lesson = strings.Repeat(p.Lesson+" ", 12)
	whole := notebook.Orient(p, "owner approval", 300)
	candidate := func() map[string]any {
		e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "reused": true, "body_included": false, "query": "lesson:" + p.ID + "@" + p.Revision}
		bindOrientation(e, whole)
		return e
	}
	w := &workState{Nonce: "synthetic", Reasoning: &reasoningProgress{Reading: &workReadingTrace{}}}
	first := map[string]any{"knowledge": []any{candidate()}}
	if _, err := limitWorkPreparation(notebook.Book{}, w, first, map[string]string{}, 65536); err != nil {
		t.Fatal(err)
	}
	rememberWorkMetadata(w, first)
	e := candidate()
	second := map[string]any{"knowledge": []any{e}}
	if _, err := limitWorkPreparation(notebook.Book{}, w, second, map[string]string{}, 65536); err != nil {
		t.Fatal(err)
	}
	from, _ := e["metadata_from"].(map[string]any)
	o, _ := e["orientation"].(map[string]any)
	if from == nil || o == nil || o["summary"] != nil || len(o) != 6 {
		t.Fatalf("the unchanged metadata is referenced, and the unresolved parts of the orientation stay: %v", e)
	}
	// a changed condition is a different candidate: the whole is delivered again
	changed := whole
	changed.Conditions = "Another condition."
	other := candidate()
	bindOrientation(other, changed)
	third := map[string]any{"knowledge": []any{other}}
	if _, err := limitWorkPreparation(notebook.Book{}, w, third, map[string]string{}, 65536); err != nil {
		t.Fatal(err)
	}
	if other["metadata_from"] != nil {
		t.Fatal("a candidate with another condition is not referenced to the earlier delivery")
	}
}
