package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The search candidates of a prepared reply carried, each, the call that reads it ({"operation":"query","query":"lesson:<id>@<revision>"}:
// 80 bytes, 32 of them its id and revision again), and a candidate that was delivered as an entry of knowledge carried that entry's
// title and applies_to again. The reply now says the call once (search.candidate_query), and a candidate leaves out the title or the
// applies_to that the entry of knowledge writes, naming the entry (knowledge_ref). These tests go through the three entrances that
// write the reply (the CLI, HTTP and the MCP tool), and through the bound that shortens it.

// candidateQueryBefore is the call every candidate carried before: the expression prepare built for each candidate.
func candidateQueryBefore(id, revision string) map[string]any {
	return map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}
}

// fillCandidateQuery puts a candidate's id and revision in place of <id> and <revision> of the reply's candidate_query.
func fillCandidateQuery(template map[string]any, id, revision string) map[string]any {
	out := map[string]any{}
	for key, value := range template {
		if text, ok := value.(string); ok {
			value = strings.NewReplacer("<id>", id, "<revision>", revision).Replace(text)
		}
		out[key] = value
	}
	return out
}

// candidateProject is a project whose articles all match one search, with the entrances that prepare in it.
type candidateProject struct {
	t       *testing.T
	book    notebook.Book
	handler http.Handler
	source  string
	pages   []notebook.Page
}

const candidateSearch = "CSV export filters"

func newCandidateProject(t *testing.T, articles int) *candidateProject {
	t.Helper()
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	p := &candidateProject{t: t, book: notebook.Book{Root: root, Scope: feedbackScope(repo)}}
	p.handler = newServer(serveOpts{storeDir: root, projectScope: p.book.Scope})
	source, err := p.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	p.source = source.ID
	for i := 0; i < articles; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", 60)
		p.pages = append(p.pages, p.ingest(notebook.Draft{Title: fmt.Sprintf("CSV export filters %d", i), Body: &body, AppliesTo: fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 3)}))
	}
	return p
}

func (p *candidateProject) ingest(d notebook.Draft) notebook.Page {
	p.t.Helper()
	d.Sources, d.Lesson, d.Kind, d.Scene = []string{p.source}, "Keep the filters when exporting CSV.", "rule", "when exporting CSV"
	r, err := p.book.Ingest(d)
	if err != nil {
		p.t.Fatal(err)
	}
	return *r.Page
}

// entrances name the three ways a prepared reply is written; each returns the bytes written and the reply decoded.
var candidateEntrances = []string{"cli", "http", "mcp"}

func (p *candidateProject) prepare(entrance, work string) (string, map[string]any) {
	p.t.Helper()
	var raw string
	switch entrance {
	case "cli":
		withStdin(p.t, work, func() { raw = capture(p.t, func() error { return cmdWork([]string{"prepare"}) }) })
	case "http":
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/wiki/notebook", bytes.NewReader([]byte(`{"operation":"prepare","work":`+work+`}`)))
		req.Header.Set("Content-Type", "application/json")
		p.handler.ServeHTTP(rec, req)
		if rec.Code != 200 {
			p.t.Fatalf("http %d: %.300s", rec.Code, rec.Body.String())
		}
		raw = rec.Body.String()
	case "mcp":
		result, _ := mcpToolCall(p.t, p.handler, "shikii_wiki", `{"operation":"prepare","work":`+work+`}`)
		data, err := json.Marshal(result["structuredContent"])
		if err != nil {
			p.t.Fatal(err)
		}
		raw = string(data)
	default:
		p.t.Fatalf("no entrance %q", entrance)
	}
	var reply map[string]any
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		p.t.Fatalf("%s: %v: %.200s", entrance, err, raw)
	}
	return raw, reply
}

const candidateWhole = `{"query":"` + candidateSearch + `","request":"Check the CSV export","max_bytes":393216}`

// read sends a call the way the entrance takes it and returns the lesson it reads.
func (p *candidateProject) read(entrance string, call map[string]any) map[string]any {
	p.t.Helper()
	var reply map[string]any
	switch entrance {
	case "mcp":
		arguments, _ := json.Marshal(call)
		result, _ := mcpToolCall(p.t, p.handler, "shikii_wiki", string(arguments))
		reply, _ = result["structuredContent"].(map[string]any)
	case "cli":
		out := capture(p.t, func() error { return cmdWiki([]string{call["operation"].(string), call["query"].(string)}) })
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			p.t.Fatalf("%v: %.200s", err, out)
		}
	}
	lessons, _ := reply["lessons"].([]any)
	if len(lessons) != 1 {
		p.t.Fatalf("%s: the call %v reads %d lessons: %.300v", entrance, call, len(lessons), reply)
	}
	return lessons[0].(map[string]any)
}

func searchOf(reply map[string]any) map[string]any {
	context, _ := reply["knowledge_context"].(map[string]any)
	search, _ := context["search"].(map[string]any)
	return search
}

func candidatesOf(reply map[string]any) []map[string]any {
	out := []map[string]any{}
	list, _ := searchOf(reply)["candidates"].([]any)
	for _, raw := range list {
		out = append(out, raw.(map[string]any))
	}
	return out
}

func entriesOf(reply map[string]any) []map[string]any {
	out := []map[string]any{}
	list, _ := reply["knowledge"].([]any)
	for _, raw := range list {
		out = append(out, raw.(map[string]any))
	}
	return out
}

// countKeys counts the members named key anywhere in a decoded JSON value.
func countKeys(v any, key string) int {
	n := 0
	switch v := v.(type) {
	case []any:
		for _, x := range v {
			n += countKeys(x, key)
		}
	case map[string]any:
		for k, x := range v {
			if k == key {
				n++
			}
			n += countKeys(x, key)
		}
	}
	return n
}

// entryText is the title or applies_to an entry of knowledge writes: in its own field, or in its article page.
func entryText(entry map[string]any, key string) string {
	if own, _ := entry[key].(string); own != "" {
		return own
	}
	article, _ := entry["article"].(map[string]any)
	text, _ := article[key].(string)
	return text
}

// entryByRef returns the entry of knowledge with that ref.
func entryByRef(reply map[string]any, ref string) map[string]any {
	for _, e := range entriesOf(reply) {
		if e["ref"] == ref {
			return e
		}
	}
	return nil
}

// candidateText reads a candidate's title or applies_to the way an agent does: from the candidate, or from the entry it names.
func candidateText(reply, candidate map[string]any, key string) (text string, found bool) {
	if own, ok := candidate[key].(string); ok {
		return own, true
	}
	ref, _ := candidate["knowledge_ref"].(string)
	if entry := entryByRef(reply, ref); entry != nil && entry["id"] == candidate["id"] && entry["revision"] == candidate["revision"] {
		if text = entryText(entry, key); text != "" {
			return text, true
		}
	}
	return "", false
}

// a (CLI, HTTP, MCP; the first search and one added to a receipt): no candidate carries a query, the reply says one call once, with
// its @<revision>, and the call filled in with each candidate's id and revision is, to the byte, the query the candidate carried.
func TestPreparedCandidatesCarryOneQueryTemplateInsteadOfTheirOwnQuery(t *testing.T) {
	p := newCandidateProject(t, 30)
	check := func(t *testing.T, label string, raw string, reply map[string]any) {
		t.Helper()
		candidates := candidatesOf(reply)
		if len(candidates) != 30 {
			t.Fatalf("%s: %d candidates", label, len(candidates))
		}
		if n := countKeys(reply, "candidate_query"); n != 1 {
			t.Fatalf("%s: the reply says the call %d times, once is wanted", label, n)
		}
		if n := strings.Count(raw, "@\\u003crevision\\u003e"); n != 1 {
			t.Fatalf("%s: the call with its @<revision> stands %d times in the reply as written, once is wanted", label, n)
		}
		template, _ := searchOf(reply)["candidate_query"].(map[string]any)
		if template["operation"] != "query" || template["query"] != "lesson:<id>@<revision>" || len(template) != 2 {
			t.Fatalf("%s: the call is %v", label, template)
		}
		for i, c := range candidates {
			if _, has := c["query"]; has {
				t.Fatalf("%s: candidate %d still carries its own query: %v", label, i, c)
			}
			id, revision := c["id"].(string), c["revision"].(string)
			filled, want := fillCandidateQuery(template, id, revision), candidateQueryBefore(id, revision)
			got, _ := json.Marshal(filled)
			before, _ := json.Marshal(want)
			if !bytes.Equal(got, before) {
				t.Fatalf("%s: candidate %d: the call filled in is %s, the query it carried is %s", label, i, got, before)
			}
		}
		if next, _ := reply["knowledge_context"].(map[string]any)["next_action"].(string); !strings.Contains(next, "candidate_query") || !strings.Contains(next, "Candidate query reads that exact article revision") {
			t.Fatalf("%s: next_action says where the call is: %s", label, next)
		}
		for _, e := range entriesOf(reply) {
			if q, _ := e["query"].(map[string]any); q["operation"] != "query" || q["query"] != "lesson:"+e["id"].(string)+"@"+e["revision"].(string) {
				t.Fatalf("%s: an entry of knowledge keeps its own query as it was: %v", label, e["query"])
			}
		}
	}
	for _, entrance := range candidateEntrances {
		t.Run(entrance, func(t *testing.T) {
			raw, reply := p.prepare(entrance, candidateWhole)
			check(t, entrance, raw, reply)
			// a search added to the receipt writes the same reply
			receipt, _ := reply["receipt"].(string)
			raw, reply = p.prepare(entrance, `{"receipt":"`+receipt+`","query":"`+candidateSearch+`","max_bytes":393216}`)
			check(t, entrance+" added to a receipt", raw, reply)
		})
	}
}

// a: the call, sent as the reply says, reads that article at that revision (the MCP tool and the CLI). Without its @<revision> the
// call reads the latest article, which is not the one the search found once the article changed.
func TestTheCandidateQueryFilledInReadsTheRevisionTheSearchFound(t *testing.T) {
	p := newCandidateProject(t, 30)
	_, reply := p.prepare("mcp", candidateWhole)
	template, _ := searchOf(reply)["candidate_query"].(map[string]any)
	candidates := candidatesOf(reply)
	if len(candidates) != 30 || template == nil {
		t.Fatalf("%d candidates, template %v", len(candidates), template)
	}
	for _, entrance := range []string{"mcp", "cli"} {
		for i, c := range candidates {
			id, revision := c["id"].(string), c["revision"].(string)
			if lesson := p.read(entrance, fillCandidateQuery(template, id, revision)); lesson["id"] != id || lesson["revision"] != revision {
				t.Fatalf("%s: candidate %d: the call reads %v@%v, not %s@%s", entrance, i, lesson["id"], lesson["revision"], id, revision)
			}
		}
	}
	// the last candidate changes after the search found it
	last := candidates[len(candidates)-1]
	id, found := last["id"].(string), last["revision"].(string)
	var changed notebook.Page
	for _, page := range p.pages {
		if page.ID == id {
			body := page.Body + "\n\nAn added condition."
			changed = p.ingest(notebook.Draft{ID: id, Title: page.Title, Body: &body, AppliesTo: page.AppliesTo})
		}
	}
	if changed.Revision == "" || changed.Revision == found {
		t.Fatalf("control: the article did not change: %q after %q", changed.Revision, found)
	}
	for _, entrance := range []string{"mcp", "cli"} {
		if lesson := p.read(entrance, fillCandidateQuery(template, id, found)); lesson["revision"] != found {
			t.Errorf("%s: the call filled in reads revision %v, the search found %s", entrance, lesson["revision"], found)
		}
		if lesson := p.read(entrance, map[string]any{"operation": "query", "query": "lesson:" + id}); lesson["revision"] != changed.Revision {
			t.Errorf("%s: control: the call without @<revision> reads %v, the latest is %s", entrance, lesson["revision"], changed.Revision)
		}
	}
}

// b: a search that finds no candidate says no call.
func TestASearchWithoutCandidatesSaysNoCandidateQuery(t *testing.T) {
	p := newCandidateProject(t, 3)
	for _, entrance := range candidateEntrances {
		t.Run(entrance, func(t *testing.T) {
			_, reply := p.prepare(entrance, `{"query":"zzqxv unmatchable","request":"Check nothing","max_bytes":393216}`)
			if n := len(candidatesOf(reply)); n != 0 {
				t.Fatalf("control: %d candidates", n)
			}
			if reply["knowledge_context"] == nil {
				t.Fatal("control: the reply carries the context of the search")
			}
			if n := countKeys(reply, "candidate_query"); n != 0 {
				t.Fatalf("the reply says a call for candidates it does not have (%d times)", n)
			}
			receipt, _ := reply["receipt"].(string)
			_, again := p.prepare(entrance, `{"receipt":"`+receipt+`","query":"zzqxv other unmatchable","max_bytes":393216}`)
			if n := len(candidatesOf(again)); n != 0 || countKeys(again, "candidate_query") != 0 {
				t.Fatalf("a search added to the receipt: %d candidates, the call said %d times", n, countKeys(again, "candidate_query"))
			}
		})
	}
}

// c: a candidate that was delivered as an entry of knowledge writes neither its title nor its applies_to and names the entry; the
// entry holds both, equal to the search's. A candidate that was not delivered keeps both.
func TestACandidateInKnowledgeNamesItsEntryInsteadOfRepeatingItsTexts(t *testing.T) {
	p := newCandidateProject(t, 30)
	found, err := p.book.QueryPage(candidateSearch, "")
	if err != nil || found.Search == nil || found.NextCursor != "" || len(found.Search.Candidates) != 30 {
		t.Fatalf("control: the whole ranking is one page: %v %v", err, found.NextCursor)
	}
	want := map[string]notebook.SearchCandidate{}
	for _, c := range found.Search.Candidates {
		want[c.ID] = c
	}
	for _, entrance := range candidateEntrances {
		t.Run(entrance, func(t *testing.T) {
			_, reply := p.prepare(entrance, candidateWhole)
			delivered := map[string]string{}
			for _, e := range entriesOf(reply) {
				delivered[e["id"].(string)] = e["ref"].(string)
			}
			if len(delivered) == 0 || len(delivered) == 30 {
				t.Fatalf("control: %d of 30 articles were delivered; some but not all are wanted", len(delivered))
			}
			for i, c := range candidatesOf(reply) {
				id := c["id"].(string)
				expect := want[id]
				ref, wasDelivered := delivered[id]
				if wasDelivered {
					if c["title"] != nil || c["applies_to"] != nil || c["knowledge_ref"] != ref {
						t.Fatalf("candidate %d was delivered as %s and writes %v", i, ref, c)
					}
				} else if c["title"] != expect.Title || c["applies_to"] != expect.AppliesTo || c["knowledge_ref"] != nil {
					t.Fatalf("candidate %d was not delivered and writes %v, the search found %q / %q", i, c, expect.Title, expect.AppliesTo)
				}
				title, ok1 := candidateText(reply, c, "title")
				applies, ok2 := candidateText(reply, c, "applies_to")
				if !ok1 || !ok2 || title != expect.Title || applies != expect.AppliesTo {
					t.Fatalf("candidate %d: an agent reads %q / %q, the search found %q / %q", i, title, applies, expect.Title, expect.AppliesTo)
				}
			}
		})
	}
}

// e: every candidate of the reply, put back as it was before (its query from the call, its title and applies_to from its entry), is
// the candidate the search made: the same ranking, ids, revisions, fields and words. The strings a reader finds are the same, and
// the only new ones are the call and the refs.
func TestTheStringsOfTheCandidatesAreTheSameBeforeAndAfter(t *testing.T) {
	p := newCandidateProject(t, 30)
	found, err := p.book.QueryPage(candidateSearch, "")
	if err != nil || found.Search == nil || len(found.Search.Candidates) != 30 {
		t.Fatal("control:", err)
	}
	var before []any
	for _, c := range found.Search.Candidates {
		raw, _ := json.Marshal(c)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		m["query"] = candidateQueryBefore(c.ID, c.Revision)
		before = append(before, m)
	}
	beforeStrings := map[string]bool{}
	walkStrings(before, beforeStrings)
	for _, entrance := range candidateEntrances {
		t.Run(entrance, func(t *testing.T) {
			_, reply := p.prepare(entrance, candidateWhole)
			template, _ := searchOf(reply)["candidate_query"].(map[string]any)
			var rebuilt []any
			for _, c := range candidatesOf(reply) {
				m := map[string]any{}
				for key, value := range c {
					m[key] = value
				}
				id, revision := c["id"].(string), c["revision"].(string)
				m["query"] = fillCandidateQuery(template, id, revision)
				if ref, ok := m["knowledge_ref"].(string); ok {
					entry := entryByRef(reply, ref)
					m["title"], m["applies_to"] = entryText(entry, "title"), entryText(entry, "applies_to")
					delete(m, "knowledge_ref")
				}
				rebuilt = append(rebuilt, m)
			}
			if !reflect.DeepEqual(rebuilt, before) {
				got, _ := json.Marshal(rebuilt)
				want, _ := json.Marshal(before)
				t.Fatalf("the candidates, put back, are not what the search made:\n got %.600s\nwant %.600s", got, want)
			}
			for i, c := range candidatesOf(reply) {
				if want := found.Search.Candidates[i]; c["id"] != want.ID || c["revision"] != want.Revision || !reflect.DeepEqual(c["fields"], toAny(want.Fields)) || !reflect.DeepEqual(c["terms"], toAny(want.Terms)) {
					t.Fatalf("candidate %d is not the search's number %d: %v", i, i, c)
				}
			}
			// the strings: all the old ones are readable in the search or in the entries, and the search's new ones are the call and the refs
			readable, inSearch := map[string]bool{}, map[string]bool{}
			walkStrings(searchOf(reply), inSearch)
			walkStrings(searchOf(reply), readable)
			walkStrings(reply["knowledge"], readable)
			for _, c := range candidatesOf(reply) {
				walkStrings(fillCandidateQuery(template, c["id"].(string), c["revision"].(string)), readable)
			}
			for text := range beforeStrings {
				if !readable[text] {
					t.Errorf("the string %q was in the candidates and cannot be read now", text)
				}
			}
			var added []string
			for text := range inSearch {
				if !beforeStrings[text] {
					added = append(added, text)
				}
			}
			sort.Strings(added)
			refs := map[string]bool{}
			for _, e := range entriesOf(reply) {
				refs[e["ref"].(string)] = true
			}
			for _, text := range added {
				if text != "lesson:<id>@<revision>" && !refs[text] {
					t.Errorf("the search holds a string that was not there before and is neither the call nor a ref: %q", text)
				}
			}
			if !slices.Contains(added, "lesson:<id>@<revision>") {
				t.Errorf("control: the call is among the new strings: %v", added)
			}
		})
	}
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// d: the candidate is written against the knowledge the reply holds when it is written. The bound takes the title, the article or
// the whole entry out of knowledge after the candidates were made, and then the candidate writes what it had left out. Whatever the
// bound, a text is in the candidate or in the entry it names, and what the bound itself took off is named in output.
type candidateCheck struct {
	ref, restored, entriesGone int
}

// checkCandidateTexts holds a bounded reply to the rule above. want says each candidate's title and applies_to as the search made them.
func checkCandidateTexts(t *testing.T, label string, reply map[string]any, want map[string][2]string, delivered map[string]bool, count *candidateCheck) {
	t.Helper()
	omitted := ""
	if output, ok := reply["output"].(map[string]any); ok {
		omitted = fmt.Sprint(output["omitted"])
	}
	inKnowledge := map[string]bool{}
	for _, e := range entriesOf(reply) {
		inKnowledge[e["id"].(string)] = true
	}
	candidates := candidatesOf(reply)
	if len(candidates) == 0 {
		if countKeys(reply, "candidate_query") != 0 {
			t.Errorf("%s: the call is said for no candidate", label)
		}
		return
	}
	if countKeys(reply, "candidate_query") != 1 {
		t.Errorf("%s: the call is said %d times", label, countKeys(reply, "candidate_query"))
	}
	for i, c := range candidates {
		id := c["id"].(string)
		if _, has := c["query"]; has {
			t.Errorf("%s: candidate %d carries a query", label, i)
		}
		for n, key := range []string{"title", "applies_to"} {
			text, ok := candidateText(reply, c, key)
			switch {
			case ok && text != want[id][n]:
				t.Errorf("%s: candidate %d (%s) reads %q as its %s, the search found %q", label, i, id, text, key, want[id][n])
			case !ok && !(key == "applies_to" && strings.Contains(omitted, "knowledge_context.search.candidates[].applies_to")):
				t.Errorf("%s: candidate %d (%s) writes no %s and no entry holds it, and output does not say it was taken off: %v", label, i, id, key, c)
			}
		}
		if ref, named := c["knowledge_ref"].(string); named {
			count.ref++
			if entry := entryByRef(reply, ref); entry == nil || entry["id"] != id || entry["revision"] != c["revision"] {
				t.Errorf("%s: candidate %d names %s, which is not its entry in this reply", label, i, ref)
			}
		}
		if inKnowledge[id] && c["title"] != nil {
			count.restored++
		}
	}
	for _, c := range candidates {
		if id := c["id"].(string); delivered[id] && !inKnowledge[id] && c["title"] != nil {
			count.entriesGone++
		}
	}
}

// candidateShape is a reply of prepare as limitWorkPreparation bounds it: entries of knowledge as the search delivered them (the
// article with its text), the candidates of the search for them and for articles that were not delivered, and the context of the search.
func candidateShape(entries, others int) (map[string]any, *workState, map[string]string, map[string][2]string) {
	w := &workState{Sources: map[string]string{}}
	quoted := map[string]string{}
	list, candidates := []any{}, []any{}
	want := map[string][2]string{}
	for i := 1; i <= entries+others; i++ {
		page := limitShapePage(i, 400)
		if i <= entries {
			e := limitShapeEntry(i, 400)
			quoted[e["ref"].(string)] = "quote"
			list = append(list, e)
		}
		candidates = append(candidates, map[string]any{"id": page.ID, "revision": page.Revision, "title": page.Title, "applies_to": page.AppliesTo, "fields": []any{"title", "body"}, "terms": []any{"export"}})
		want[page.ID] = [2]string{page.Title, page.AppliesTo}
	}
	reply := map[string]any{"receipt": "r-" + strings.Repeat("c", 100), "task": strings.Repeat("t", 1500), "task_context": map[string]any{"role": "user"}, "how_to_record": compactWorkHowTo, "knowledge": list,
		"knowledge_context": map[string]any{"search": map[string]any{"candidates": candidates, "candidate_query": workCandidateQuery(), "total": entries + others}, "next_cursor": "c0"},
		"knowledge_query":   map[string]any{"operation": "query", "query": "export audit", "cursor": "c0"}}
	w.candidateCursor = func(keep int) string { return fmt.Sprintf("cursor-%d", keep) }
	return reply, w, quoted, want
}

func TestACandidateWritesWhatItsEntryNoLongerHoldsWhateverTheBound(t *testing.T) {
	var count candidateCheck
	runs := 0
	// the hook measures the reply with the rest of its text around it, and the envelope of output (which it adds last) can then be
	// bigger than the room the bound keeps for it: the penalty stands for that, so that the last fields go after the candidates were cut
	penalty := func(extra int) func(any) int {
		return func(v any) int {
			n := workContextSize(v)
			if m, ok := v.(map[string]any); ok && m["output"] != nil {
				n += extra
			}
			return n
		}
	}
	delivered := map[string]bool{}
	first, _, _, _ := candidateShape(10, 20)
	for _, e := range entriesOf(loopObject(t, first)) {
		delivered[e["id"].(string)] = true
	}
	for _, leaveOutLast := range []bool{false, true} {
		for _, sizing := range []func(any) int{nil, penalty(1500), penalty(9000)} {
			for bound := 40000; bound >= workMinBudget; bound -= 400 {
				reply, w, quoted, want := candidateShape(10, 20)
				w.leaveOutLast = leaveOutLast
				if _, err := limitWorkPreparation(notebook.Book{}, w, reply, quoted, bound, sizing); err != nil {
					continue // a bound the reply cannot be made to fit is refused, as before
				}
				runs++
				checkCandidateTexts(t, fmt.Sprintf("last fields %v, penalty %v, bound %d", leaveOutLast, sizing != nil, bound), loopObject(t, reply), want, delivered, &count)
			}
		}
	}
	// the sweep reaches each kind of reply: a text left to its entry, an entry that was lightened or lost its title and so is written again
	// by the candidate, and an entry the bound left out
	if runs < 50 || count.ref == 0 || count.restored == 0 || count.entriesGone == 0 {
		t.Fatalf("the bounds did not reach every case: %d replies, %+v", runs, count)
	}
}

// d, through the preparation of a real project (the same entrance the hook, the CLI and the MCP tool call): from the smallest bound a
// caller can name up, with and without the last fields the hook's third try may leave out, and with the measure of the hook, which adds
// the text around the reply (the envelope of output costs more than the bound keeps for it, as it can in the hook).
func TestACandidateWritesWhatItsEntryNoLongerHoldsInARealPreparation(t *testing.T) {
	p := newCandidateProject(t, 30)
	found, err := p.book.QueryPage(candidateSearch, "")
	if err != nil || found.Search == nil {
		t.Fatal(err)
	}
	want := map[string][2]string{}
	for _, c := range found.Search.Candidates {
		want[c.ID] = [2]string{c.Title, c.AppliesTo}
	}
	var count candidateCheck
	whole, err := prepareWork(p.book.Root, p.book.Scope, workRequest{Query: candidateSearch, Request: "Check the CSV export", MaxBytes: 393216})
	if err != nil {
		t.Fatal(err)
	}
	delivered := map[string]bool{}
	for _, e := range entriesOf(loopObject(t, whole)) {
		delivered[e["id"].(string)] = true
	}
	envelope := func(v any) int {
		n := workContextSize(v)
		if m, ok := v.(map[string]any); ok && m["output"] != nil {
			n += 1500
		}
		return n
	}
	runs := 0
	for _, c := range []struct {
		leaveOutLast bool
		size         func(any) int
	}{{false, nil}, {true, nil}, {true, envelope}} {
		for bound := workMinBudget; bound <= 24000; bound += 500 {
			v, err := prepareWork(p.book.Root, p.book.Scope, workRequest{Query: candidateSearch, Request: "Check the CSV export", MaxBytes: bound, leaveOutLast: c.leaveOutLast, responseSize: c.size})
			if err != nil {
				continue // a bound the reply cannot be made to fit is refused, as before
			}
			runs++
			checkCandidateTexts(t, fmt.Sprintf("last fields %v, measure %v, bound %d", c.leaveOutLast, c.size != nil, bound), loopObject(t, v), want, delivered, &count)
		}
	}
	if runs < 50 || count.ref == 0 || count.restored == 0 {
		t.Fatalf("the bounds did not reach a reply whose entries lost their texts: %d replies, %+v", runs, count)
	}
}

// c: a text is left out only when the entry writes the very same non-empty string. A text that differs by a character, an empty
// text, an entry that holds the text in neither of its places, and an entry of another revision leave the candidate as the search made it.
func TestACandidateLeavesOutOnlyATextTheEntryWritesTheSame(t *testing.T) {
	page := limitShapePage(1, 400)
	build := func(entry map[string]any, candidate map[string]any) map[string]any {
		c := map[string]any{"id": page.ID, "revision": page.Revision, "title": page.Title, "applies_to": page.AppliesTo, "fields": []any{"title"}, "terms": []any{"export"}}
		for key, value := range candidate {
			c[key] = value
		}
		reply := map[string]any{"knowledge": []any{entry}, "knowledge_context": map[string]any{"search": map[string]any{"candidates": []any{c}}}}
		bindCandidates(reply)
		return loopObject(t, reply)["knowledge_context"].(map[string]any)["search"].(map[string]any)["candidates"].([]any)[0].(map[string]any)
	}
	article := func() map[string]any {
		return map[string]any{"ref": "knowledge1", "id": page.ID, "revision": page.Revision, "article": page}
	}
	shortened := func() map[string]any {
		return map[string]any{"ref": "knowledge1", "id": page.ID, "revision": page.Revision, "title": page.Title, "applies_to": page.AppliesTo}
	}
	for _, c := range []struct {
		name       string
		entry, own map[string]any
		titleOut   bool // the candidate leaves its title out
		appliesOut bool // the candidate leaves its applies_to out
	}{
		{"an article page that writes both", article(), nil, true, true},
		{"a shortened entry that writes both", shortened(), nil, true, true},
		{"a title that differs by a character", shortened(), map[string]any{"title": page.Title + "."}, false, true},
		{"an applies_to that differs by a character", shortened(), map[string]any{"applies_to": page.AppliesTo + "."}, true, false},
		{"an empty title and an empty entry title", func() map[string]any { e := shortened(); e["title"] = ""; return e }(), map[string]any{"title": ""}, false, true},
		{"an entry that lost its applies_to", func() map[string]any { e := shortened(); delete(e, "applies_to"); return e }(), nil, true, false},
		{"an entry that holds neither", map[string]any{"ref": "knowledge1", "id": page.ID, "revision": page.Revision}, nil, false, false},
		{"an entry of another revision", func() map[string]any { e := shortened(); e["revision"] = "0000000000000999"; return e }(), nil, false, false},
		{"an entry of another article", func() map[string]any { e := shortened(); e["id"] = "0000000000000999"; return e }(), nil, false, false},
		{"an entry without a ref", func() map[string]any { e := shortened(); delete(e, "ref"); return e }(), nil, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := build(c.entry, c.own)
			wantTitle, wantApplies := page.Title, page.AppliesTo
			if v, ok := c.own["title"]; ok {
				wantTitle, _ = v.(string)
			}
			if v, ok := c.own["applies_to"]; ok {
				wantApplies, _ = v.(string)
			}
			if c.titleOut != (got["title"] == nil) || c.appliesOut != (got["applies_to"] == nil) {
				t.Fatalf("title left out %v, applies_to left out %v: %v", c.titleOut, c.appliesOut, got)
			}
			if !c.titleOut && got["title"] != wantTitle || !c.appliesOut && got["applies_to"] != wantApplies {
				t.Fatalf("a text that stays is written as the search made it: %v", got)
			}
			if named := c.titleOut || c.appliesOut; named != (got["knowledge_ref"] == "knowledge1") || (!named && got["knowledge_ref"] != nil) {
				t.Fatalf("knowledge_ref %v, wanted %v: %v", got["knowledge_ref"], named, got)
			}
			if got["id"] != page.ID || got["revision"] != page.Revision || got["fields"] == nil || got["terms"] == nil {
				t.Fatalf("id, revision, fields and terms are always written: %v", got)
			}
		})
	}
}

// g: the hashes the delivery compares are those of the entry; a candidate that names the entry changes none of them. The values are
// those of TestOrientationHashesAreThoseOfTheWholeOrientation, which the code before the candidates left their texts out computed.
func TestCandidatesLeaveTheHashesOfTheEntriesAsTheyWere(t *testing.T) {
	p := orientationPage(40)
	whole := notebook.Orient(p, "owner approval", 300)
	e := map[string]any{"ref": "knowledge1", "id": p.ID, "revision": p.Revision, "title": p.Title, "summary": p.Lesson, "applies_to": p.AppliesTo, "rationale": p.Rationale, "body_included": false}
	bindOrientation(e, whole)
	candidate := map[string]any{"id": p.ID, "revision": p.Revision, "title": p.Title, "applies_to": p.AppliesTo, "fields": []any{"title"}, "terms": []any{"owner"}}
	reply := map[string]any{"receipt": "r", "task": "t", "knowledge": []any{e}, "knowledge_context": map[string]any{"search": map[string]any{"candidates": []any{candidate}, "candidate_query": workCandidateQuery()}}}
	if _, err := limitWorkPreparation(notebook.Book{}, &workState{Sources: map[string]string{}}, reply, map[string]string{}, 65536); err != nil {
		t.Fatal(err)
	}
	named := loopObject(t, reply)["knowledge_context"].(map[string]any)["search"].(map[string]any)["candidates"].([]any)[0].(map[string]any)
	if named["knowledge_ref"] != "knowledge1" || named["title"] != nil {
		t.Fatalf("control: the candidate names the entry: %v", named)
	}
	const signature = "fbd2e4149840e70a8c35c139acbd751afc096008bf85b9cd3915a2f90681d103"
	const entryHash = "73f85b72561275dbdb5d05499bddc878797f56d7f8efc67a1b1d2816bfc0dde1"
	if got, ok := workMetadataSignature(e); !ok || got != signature {
		t.Errorf("the delivery signature of the entry is %s (%v)", got, ok)
	}
	if got := workHash(wholeOrientationEntry(e)); got != entryHash {
		t.Errorf("the hash of the whole entry is %s", got)
	}
}
