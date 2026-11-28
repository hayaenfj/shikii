package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A search or a list of the articles of a wiki used to carry the hash of every source of every lesson it returned (64 hexadecimal
// digits each; 8% of the bytes of the search replies of a real project, 2026-11), and nothing takes such a hash in: a save names
// its sources by id (Draft.Sources, Patch.Sources are lists of strings), and the revision of an article is made from the whole
// article including the hash and text_hash of each source, so an article id with its revision already fixes them. The search and
// the list now name each source by its id alone; an article read by name (lesson:<id> or lesson:<id>@<revision>) still carries
// them, and so does everything that is saved or answers a save.

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// sourceHashFixture is a project with two articles: "Export audit workflow" cites a captured observation and a conversation whose
// long original text is archived with the citation (so its source has a text_hash as well as a hash), and "Release workflow"
// cites the observation only.
type sourceHashFixture struct {
	t               *testing.T
	agent, scope    string
	h               http.Handler
	book            notebook.Book
	audit, release  notebook.Page
	observed, spoke string
}

func newSourceHashFixture(t *testing.T) sourceHashFixture {
	t.Helper()
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	scope := feedbackScope(repo)
	b := notebook.Book{Root: agent, Scope: scope}
	st := store.Store{Dir: agent}
	observed, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters.", Reference: "go test ./export", Version: "1", ObservedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	request, summary := "Audit the export", strings.Repeat("A", 1500)
	rec := store.Record{TraceID: "0000000000000012", SpecID: "done-as-asked", Inputs: map[string]string{"request": request, "summary": summary}, At: time.Now()}
	rec.InputsHash = store.HashInputs(rec.Inputs)
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRecordText(rec, map[string]string{"summary": summary + strings.Repeat("B", 18000) + "END-OF-ORIGINAL"}); err != nil {
		t.Fatal(err)
	}
	const spoke = "0000000000000013"
	if err := st.AppendFeedback(scope, store.Feedback{ID: spoke, Request: request, Summary: summary, Reply: "Check the totals too", Targets: []store.ReviewTarget{{Spec: rec.SpecID, TraceID: rec.TraceID}}}); err != nil {
		t.Fatal(err)
	}
	body, rationale := "## Steps\nRun the export check and compare the totals.", "The check and the conversation both found the filters kept."
	save := func(sources []string, title, lesson, appliesTo string) notebook.Page {
		t.Helper()
		r, err := b.Ingest(notebook.Draft{Sources: sources, Title: title, Lesson: lesson, AppliesTo: appliesTo, Kind: "rule", Scene: "when auditing", Body: &body, Rationale: &rationale})
		if err != nil {
			t.Fatal(err)
		}
		return *r.Page
	}
	f := sourceHashFixture{t: t, agent: agent, scope: scope, h: newServer(serveOpts{storeDir: agent, projectScope: scope}), book: b, observed: observed.ID, spoke: spoke}
	f.audit = save([]string{observed.ID, spoke}, "Export audit workflow", "Audit the export filters and totals.", "export audit")
	f.release = save([]string{observed.ID}, "Release workflow", "Read the release notes before tagging.", "release")
	if len(f.audit.Sources) != 2 || len(f.release.Sources) != 1 || f.audit.Sources[0].TextHash+f.audit.Sources[1].TextHash == "" {
		t.Fatalf("the fixture changed: the conversation's original text was not archived with its citation: %+v", f.audit.Sources)
	}
	return f
}

// wikiEntry is one way an agent reaches the wiki: the command line, the HTTP endpoint, or the MCP tool.
type wikiEntry struct {
	name string
	// call sends an operation with its fields and returns the decoded reply.
	call func(t *testing.T, operation string, fields map[string]any) map[string]any
}

func (f sourceHashFixture) entries() []wikiEntry {
	decode := func(t *testing.T, raw []byte) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("the reply is a JSON object: %v: %.300s", err, raw)
		}
		return out
	}
	return []wikiEntry{
		{"CLI", func(t *testing.T, operation string, fields map[string]any) map[string]any {
			t.Helper()
			if operation == "query" {
				args := []string{"query"}
				if q, _ := fields["query"].(string); q != "" {
					args = append(args, q)
				}
				if ids, _ := fields["sources"].([]string); len(ids) > 0 {
					args = append(args, ids...)
				}
				return decode(t, []byte(capture(t, func() error { return cmdWiki(args) })))
			}
			if operation == "prepare" { // the command line has no wiki prepare: it is work prepare
				body, _ := json.Marshal(fields["work"])
				var out string
				var err error
				withStdin(t, string(body), func() { out, err = captureErr(func() error { return cliCall([]string{"work", "prepare"}) }) })
				if err != nil {
					t.Fatal(err)
				}
				return decode(t, []byte(out))
			}
			if patch, ok := fields["patch"]; ok { // the command line takes the patch itself on standard input
				fields = map[string]any{}
				for k, v := range patch.(map[string]any) {
					fields[k] = v
				}
			}
			body, _ := json.Marshal(fields)
			return notebookCommand(t, operation, string(body))
		}},
		{"HTTP", func(t *testing.T, operation string, fields map[string]any) map[string]any {
			t.Helper()
			body, _ := json.Marshal(withOperation(operation, fields))
			req := httptest.NewRequest(http.MethodPost, "/v1/wiki/notebook", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			f.h.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("%s: %d %s", operation, rec.Code, rec.Body.String())
			}
			return decode(t, rec.Body.Bytes())
		}},
		{"MCP", func(t *testing.T, operation string, fields map[string]any) map[string]any {
			t.Helper()
			body, _ := json.Marshal(withOperation(operation, fields))
			result, _ := mcpToolCall(t, f.h, "shikii_wiki", string(body))
			structured, ok := result["structuredContent"].(map[string]any)
			if !ok {
				t.Fatalf("an MCP reply carries its structured content: %v", result)
			}
			return structured
		}},
	}
}

func withOperation(operation string, fields map[string]any) map[string]any {
	out := map[string]any{"operation": operation}
	for k, v := range fields {
		out[k] = v
	}
	return out
}

func (e wikiEntry) query(t *testing.T, text string) map[string]any {
	t.Helper()
	return e.call(t, "query", map[string]any{"query": text})
}

// replyLessonsByID returns the lessons of a query reply by article id.
func replyLessonsByID(t *testing.T, reply map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	items, _ := reply["lessons"].([]any)
	for _, item := range items {
		lesson := item.(map[string]any)
		out[lesson["id"].(string)] = lesson
	}
	return out
}

func replySourcesOf(t *testing.T, lesson map[string]any) []map[string]any {
	t.Helper()
	raw, ok := lesson["sources"].([]any)
	if !ok {
		t.Fatalf("a lesson names its sources: %v", lesson)
	}
	out := []map[string]any{}
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

func replySourceIDsOf(t *testing.T, lesson map[string]any) []string {
	t.Helper()
	ids := []string{}
	for _, s := range replySourcesOf(t, lesson) {
		ids = append(ids, s["id"].(string))
	}
	return ids
}

func sourceIDsOfPage(p notebook.Page) []string {
	ids := []string{}
	for _, s := range p.Sources {
		ids = append(ids, s.ID)
	}
	return ids
}

// withoutSourceHashes is a lesson as a search or a list returns it: the lesson as it is read by name, with each source reduced to its id.
func withoutSourceHashes(t *testing.T, lesson map[string]any) map[string]any {
	t.Helper()
	out := map[string]any{}
	for k, v := range lesson {
		out[k] = v
	}
	ids := []any{}
	for _, s := range replySourcesOf(t, lesson) {
		ids = append(ids, map[string]any{"id": s["id"]})
	}
	out["sources"] = ids
	return out
}

// keyOf is the file of a saved source or article, found without the store's name.
func keyOf(t *testing.T, pattern string) string {
	t.Helper()
	found, err := filepath.Glob(pattern)
	if err != nil || len(found) != 1 {
		t.Fatalf("one file is expected at %s: %v %v", pattern, found, err)
	}
	return found[0]
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// a. A search of words and the list of the articles name each source by its id alone, through every entry; the same article read
// by name (its id, or its id with a revision) carries the hash of each source as before, and a source is read as before.
func TestSourceHashesAreLeftOutOfASearchAndAListButNotOfAnArticleReadByName(t *testing.T) {
	f := newSourceHashFixture(t)
	articles := map[string]notebook.Page{f.audit.ID: f.audit, f.release.ID: f.release}
	for _, entry := range f.entries() {
		t.Run(entry.name, func(t *testing.T) {
			listed := map[string]map[string]any{}
			for _, text := range []string{"workflow", ""} { // a word search that finds both articles, and the list of the articles
				lessons := replyLessonsByID(t, entry.query(t, text))
				if len(lessons) != 2 {
					t.Fatalf("query %q returns both articles: %v", text, lessons)
				}
				for id, lesson := range lessons {
					for _, s := range replySourcesOf(t, lesson) {
						if len(s) != 1 || s["id"] == nil {
							t.Errorf("query %q names each source of %s by its id alone, not by %v", text, id, s)
						}
					}
					if got, want := replySourceIDsOf(t, lesson), sourceIDsOfPage(articles[id]); !slices.Equal(got, want) {
						t.Errorf("query %q names the sources of %s as %v, not %v", text, id, got, want)
					}
					listed[id] = lesson
				}
			}
			for id, page := range articles {
				for _, query := range []string{"lesson:" + id, "lesson:" + id + "@" + page.Revision} {
					named := replyLessonsByID(t, entry.query(t, query))[id]
					if named == nil {
						t.Fatalf("%s reads the article", query)
					}
					got := replySourcesOf(t, named)
					if len(got) != len(page.Sources) {
						t.Fatalf("%s names every source: %v", query, got)
					}
					for i, want := range page.Sources {
						ref := got[i]
						if ref["id"] != want.ID || ref["hash"] != want.Hash || !hexDigest.MatchString(want.Hash) || ref["text_hash"] != nilIfEmpty(want.TextHash) {
							t.Errorf("%s keeps the hashes of source %s: %v, saved as %+v", query, want.ID, ref, want)
						}
					}
					if want := withoutSourceHashes(t, named); !reflect.DeepEqual(listed[id], want) {
						t.Errorf("%s: the listed lesson is the named lesson with its sources reduced to ids, and nothing else changes:\nlisted: %v\nnamed:  %v", id, listed[id], want)
					}
				}
			}
			// the hash a named read returns is the hash of the saved record of the source
			for _, s := range f.audit.Sources {
				path := keyOf(t, filepath.Join(f.agent, "records", "wiki-sources", "*", s.ID+".json"))
				if got := fileDigest(t, path); got != s.Hash {
					t.Errorf("the hash of source %s is the hash of its saved record: %s, %s", s.ID, got, s.Hash)
				}
			}
			// reading the sources themselves is not changed: the saved text comes back as it was captured
			read := entry.call(t, "query", map[string]any{"sources": []string{f.observed}})
			observations, _ := read["observations"].([]any)
			if len(observations) != 1 || observations[0].(map[string]any)["content"] != "The synthetic export kept its filters." {
				t.Errorf("a source is read as before: %v", read)
			}
			material, _ := entry.call(t, "query", map[string]any{"query": f.spoke})["source_material"].([]any)
			if len(material) != 1 || material[0].(map[string]any)["id"] != f.spoke {
				t.Errorf("a conversation is read by its id as before: %v", material)
			}
		})
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// The bytes of a search reply: each source is written as {"id":...} alone (the listing of the same article read by name writes
// {"id":...,"hash":...,"text_hash":...} around it).
func TestSourceHashesAreLeftOutOfTheBytesOfASearchReply(t *testing.T) {
	f := newSourceHashFixture(t)
	release := f.release.Sources[0]
	out := capture(t, func() error { return cmdWiki([]string{"query", "Release workflow"}) })
	if want := `"sources":[{"id":"` + release.ID + `"}]`; !strings.Contains(out, want) {
		t.Errorf("a search reply writes the source of an article as its id alone: want %s in\n%s", want, out)
	}
	if strings.Contains(out, release.Hash) || strings.Contains(out, `"hash"`) || strings.Contains(out, `"text_hash"`) {
		t.Errorf("a search reply carries no hash of a source:\n%s", out)
	}
	out = capture(t, func() error { return cmdWiki([]string{"query", "lesson:" + f.release.ID}) })
	if want := `"sources":[{"id":"` + release.ID + `","hash":"` + release.Hash + `"}]`; !strings.Contains(out, want) {
		t.Errorf("an article read by name writes its source with its hash: want %s in\n%s", want, out)
	}
}

// b. Every other byte of the reply is what it was: the reply as it was written before (the view as it comes out of the search, with
// its hashes), taken apart, with the hash and text_hash of each source left out and written again in the order a reply is written
// in, is the reply as it is written now.
func TestSourceHashesAreTheOnlyBytesALeftOutReplyLacks(t *testing.T) {
	f := newSourceHashFixture(t)
	for _, query := range []string{"workflow", "Export audit workflow", ""} {
		before, err := f.book.QueryPage(query, "")
		if err != nil || len(before.Lessons) == 0 {
			t.Fatalf("query %q: %v %v", query, before.Lessons, err)
		}
		written, err := orderedReply(before)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(written))
		dec.UseNumber()
		var tree map[string]any
		if err := dec.Decode(&tree); err != nil {
			t.Fatal(err)
		}
		for _, lesson := range tree["lessons"].([]any) {
			for _, source := range lesson.(map[string]any)["sources"].([]any) {
				delete(source.(map[string]any), "hash")
				delete(source.(map[string]any), "text_hash")
			}
		}
		want, err := orderReplyValue(tree, true, true)
		if err != nil {
			t.Fatal(err)
		}
		got, err := orderedReply(before.WithSourceIDsOnly())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("query %q: the reply without the hashes is not the earlier reply minus its hashes:\n got %s\nwant %s", query, got, want)
		}
		if len(got) >= len(written) || bytes.Contains(got, []byte(`"hash"`)) {
			t.Errorf("query %q: the fixture changed, or the hashes are still there (%d bytes against %d)", query, len(got), len(written))
		}
	}
}

// c. The hash stays in the saved article and in the saved source whatever is asked, and a save that names its sources by the ids a
// search gave still goes through and keeps every hash.
func TestSourceHashesStayInTheSavedArticlesAndSavesFromListedIDsKeepThem(t *testing.T) {
	f := newSourceHashFixture(t)
	pageFile := func(p notebook.Page) string {
		return keyOf(t, filepath.Join(f.agent, "wiki", "lessons", "*", p.ID+".md"))
	}
	files := func() map[string][]byte {
		t.Helper()
		out := map[string][]byte{}
		for _, pattern := range []string{filepath.Join(f.agent, "wiki", "lessons", "*", "*.md"), filepath.Join(f.agent, "records", "wiki-sources", "*", "*")} {
			found, _ := filepath.Glob(pattern)
			for _, path := range found {
				if data, err := os.ReadFile(path); err == nil {
					out[path] = data
				}
			}
		}
		return out
	}
	hasHashLines := func(p notebook.Page) {
		t.Helper()
		data, err := os.ReadFile(pageFile(p))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range p.Sources {
			if !bytes.Contains(data, []byte("hash: "+s.Hash)) {
				t.Errorf("the saved article %s keeps the hash of source %s", p.ID, s.ID)
			}
			if s.TextHash != "" && !bytes.Contains(data, []byte("text_hash: "+s.TextHash)) {
				t.Errorf("the saved article %s keeps the text_hash of source %s", p.ID, s.ID)
			}
		}
	}
	hasHashLines(f.audit)
	hasHashLines(f.release)
	before := files()
	for _, entry := range f.entries() {
		entry.query(t, "workflow")
		entry.query(t, "")
		entry.query(t, "lesson:"+f.audit.ID)
	}
	if after := files(); !reflect.DeepEqual(before, after) {
		t.Fatal("asking the wiki changes no saved article and no saved source")
	}

	// a save that names the sources by the ids of a search reply: a patch through MCP and an update through the command line
	entries := f.entries()
	listed := replyLessonsByID(t, entries[1].query(t, ""))
	release, audit := listed[f.release.ID], listed[f.audit.ID]
	patched := entries[2].call(t, "patch", map[string]any{"patch": map[string]any{
		"id": f.release.ID, "revision": release["revision"], "sources": replySourceIDsOf(t, release), "change_reason": "Say what to read.",
		"edits": []any{map[string]any{"field": "lesson", "before": "Read the release notes before tagging.", "after": "Read the release notes and the tag before tagging."}}}})
	page, _ := patched["page"].(map[string]any)
	if page == nil || page["lesson"] != "Read the release notes and the tag before tagging." {
		t.Fatalf("a patch that names its sources by listed ids is saved: %v", patched)
	}
	ingested := entries[0].call(t, "ingest", map[string]any{"id": f.audit.ID, "sources": replySourceIDsOf(t, audit), "title": f.audit.Title, "lesson": "Audit the export filters, the totals and the order.", "applies_to": f.audit.AppliesTo, "kind": "rule", "scene": "when auditing"})
	if saved, _ := ingested["page"].(map[string]any); saved == nil || saved["lesson"] != "Audit the export filters, the totals and the order." {
		t.Fatalf("an update that names its sources by listed ids is saved: %v", ingested)
	}
	for _, p := range []notebook.Page{f.release, f.audit} {
		hasHashLines(p) // the saved articles still hold every hash
		named := replyLessonsByID(t, entries[1].query(t, "lesson:"+p.ID))[p.ID]
		for i, s := range replySourcesOf(t, named) {
			if s["hash"] != p.Sources[i].Hash || s["text_hash"] != nilIfEmpty(p.Sources[i].TextHash) {
				t.Errorf("after the save, article %s still reads with the hashes it was saved with: %v", p.ID, s)
			}
		}
	}
}

// d. What is saved and what answers a save or a reflection still carries each source with its hash: only a search and a list change.
func TestSourceHashesStayInTheRepliesOfASaveAPrepareAndAReflection(t *testing.T) {
	f := newSourceHashFixture(t)
	entries := f.entries()
	needHashes := func(where string, sources any, want []notebook.Source) {
		t.Helper()
		got, _ := sources.([]any)
		if len(got) != len(want) {
			t.Fatalf("%s names every source: %v", where, got)
		}
		for i, w := range want {
			ref, _ := got[i].(map[string]any)
			if ref["id"] != w.ID || ref["hash"] != w.Hash || ref["text_hash"] != nilIfEmpty(w.TextHash) {
				t.Errorf("%s keeps the hashes of source %s: %v, saved as %+v", where, w.ID, ref, w)
			}
		}
	}
	for _, entry := range entries {
		patched := entry.call(t, "patch", map[string]any{"patch": map[string]any{
			"id": f.release.ID, "revision": currentRevision(t, f, f.release.ID), "sources": []string{f.observed}, "change_reason": "Touch it (" + entry.name + ").",
			"edits": []any{map[string]any{"field": "applies_to", "before": currentField(t, f, f.release.ID, "applies_to"), "after": "release " + entry.name}}}})
		needHashes(entry.name+" patch reply", patched["page"].(map[string]any)["sources"], f.release.Sources)
		prepared := entry.call(t, "prepare", map[string]any{"work": map[string]any{"query": "Export audit workflow", "request": "Audit the export."}})
		found := false
		for _, item := range prepared["knowledge"].([]any) {
			if article, _ := item.(map[string]any)["article"].(map[string]any); article["id"] == f.audit.ID {
				needHashes(entry.name+" prepare reply", article["sources"], f.audit.Sources)
				found = true
			}
		}
		if !found {
			t.Fatalf("%s prepare returns the article: %v", entry.name, prepared["knowledge"])
		}
	}
	ingested := entries[0].call(t, "ingest", map[string]any{"id": f.audit.ID, "sources": sourceIDsOfPage(f.audit), "title": f.audit.Title, "lesson": "Audit the export filters and the totals again.", "applies_to": f.audit.AppliesTo, "kind": "rule", "scene": "when auditing"})
	needHashes("ingest reply", ingested["page"].(map[string]any)["sources"], f.audit.Sources)
}

func currentRevision(t *testing.T, f sourceHashFixture, id string) string {
	t.Helper()
	v, err := f.book.Query("lesson:" + id)
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(v, err)
	}
	return v.Lessons[0].Revision
}

func currentField(t *testing.T, f sourceHashFixture, id, field string) string {
	t.Helper()
	v, err := f.book.Query("lesson:" + id)
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(v, err)
	}
	if field == "applies_to" {
		return v.Lessons[0].AppliesTo
	}
	t.Fatalf("no field %s", field)
	return ""
}

// d. The reply of a reflection names the sources of its lessons with their hashes too.
func TestSourceHashesStayInTheReplyOfAReflectionQuery(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "s1")
	memoryEffectComplete(t, st, scope, "s1")
	book := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := book.Query("lesson:" + draft.ID)
	if err != nil || len(v.Lessons) != 1 {
		t.Fatal(v, err)
	}
	want := v.Lessons[0].Sources
	lessons, _ := reflectCall(t, h, map[string]any{"operation": "query"})["lessons"].([]any)
	if len(lessons) != 1 {
		t.Fatalf("the reflection returns the lesson delivered to the conversation: %v", lessons)
	}
	got, _ := lessons[0].(map[string]any)["sources"].([]any)
	if len(got) != len(want) || len(want) == 0 {
		t.Fatalf("the reflection names every source: %v", got)
	}
	for i, w := range want {
		if ref := got[i].(map[string]any); ref["id"] != w.ID || ref["hash"] != w.Hash || !hexDigest.MatchString(w.Hash) {
			t.Errorf("the reflection keeps the hash of source %s: %v", w.ID, ref)
		}
	}
}
