package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A save with an existing article's id replaces its whole body. The entries an agent saves through (shikii wiki ingest, shikii reflect
// submit, and their MCP and HTTP forms) refuse a body that drops half or more of the old one, and say to use wiki patch. These tests
// go in through each entry, not through the function behind them, so an entry that is not wired to the refusal fails here.

const overwriteBody = "## Background\n\n- Agents sent only their new sentences to an article's id.\n- A save with an id replaces the whole body.\n\n## Decided\n\n- A save that drops half or more of the old body is refused.\n- Shortening goes through wiki patch.\n"
const overwriteShort = "Only the new sentence."

// rewriteBody replaces an article's body the way the refusal sends an agent to: with a revision-checked wiki patch. Tests that need an
// article another writer has rewritten use it, since Ingest refuses a body that drops half or more of the old one.
func rewriteBody(t *testing.T, b notebook.Book, id, source, body string) notebook.Page {
	t.Helper()
	return rewriteBodyAndPassages(t, b, id, source, body, nil)
}

// rewriteBodyAndPassages is rewriteBody for an article whose quotations change with the body (passages nil keeps them).
func rewriteBodyAndPassages(t *testing.T, b notebook.Book, id, source, body string, passages *[]notebook.KnowledgePassage) notebook.Page {
	t.Helper()
	got, err := b.Query("lesson:" + id)
	if err != nil || len(got.Lessons) != 1 {
		t.Fatalf("the article to rewrite cannot be read: %v", err)
	}
	current := got.Lessons[0]
	patched, err := b.Patch(notebook.Patch{ID: id, Revision: current.Revision, Sources: []string{source}, ChangeReason: "a later writer rewrote the body", Edits: []notebook.TextEdit{{Field: "body", Before: current.Body, After: body}}, Passages: passages})
	if err != nil {
		t.Fatal(err)
	}
	return *patched.Page
}

type overwriteFixture struct {
	st      store.Store
	scope   string
	h       http.Handler
	book    notebook.Book
	sources []string
	n       int
}

func newOverwriteFixture(t *testing.T) *overwriteFixture {
	t.Helper()
	st, scope, h := reviewFixture(t)
	f := &overwriteFixture{st: st, scope: scope, h: h, book: notebook.Book{Root: st.Dir, Scope: scope}}
	for n := 1; n <= 2; n++ {
		id, _ := reviewSeed(t, st, scope, n)
		f.sources = append(f.sources, id)
	}
	return f
}

// article saves a new article with overwriteBody. Each call makes another one, so entries do not share a revision.
func (f *overwriteFixture) article(t *testing.T, name string) notebook.Page {
	t.Helper()
	body := overwriteBody
	saved, err := f.book.Ingest(notebook.Draft{Sources: f.sources, Title: "Saving into an existing article by " + name, Lesson: "An id save replaces the body.", AppliesTo: "saving into an existing wiki article", Kind: "rule", Scene: "when saving an article", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	return *saved.Page
}

// draft is what an agent sends to update the article: its id, the same summary and the body it chose.
func (f *overwriteFixture) draft(p notebook.Page, body string) map[string]any {
	return map[string]any{"id": p.ID, "sources": f.sources, "title": p.Title, "lesson": p.Lesson, "applies_to": p.AppliesTo, "kind": p.Kind, "scene": p.Scene, "body": body, "change_reason": "the agent's update"}
}

func (f *overwriteFixture) body(t *testing.T, id string) (revision, body string) {
	t.Helper()
	got, err := f.book.Query("lesson:" + id)
	if err != nil || len(got.Lessons) != 1 {
		t.Fatalf("the article cannot be read: %v", err)
	}
	return got.Lessons[0].Revision, got.Lessons[0].Body
}

// entry sends the draft through one entry an agent saves through and returns why it was refused ("" when it was saved).
type overwriteEntry func(t *testing.T, f *overwriteFixture, draft map[string]any) string

func commandStdin(t *testing.T, input any, run func() error) (string, error) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := stdin, stdout
	var out bytes.Buffer
	stdin, stdout = strings.NewReader(string(data)), &out
	defer func() { stdin, stdout = oldIn, oldOut }()
	err = run()
	return out.String(), err
}

func receiptError(t *testing.T, reply map[string]any) string {
	t.Helper()
	items, _ := reply["wiki"].([]any)
	if len(items) != 1 {
		t.Fatalf("a submit of one wiki draft returns one receipt: %v", reply)
	}
	text, _ := items[0].(map[string]any)["error"].(string)
	if (reply["complete"] == true) != (text == "") {
		t.Fatalf("complete and the receipt's error disagree: %v", reply)
	}
	return text
}

func overwriteEntries(t *testing.T, f *overwriteFixture) map[string]overwriteEntry {
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, f.st.Dir)
	mcpCall := func(name string, args map[string]any) map[string]any {
		return result(t, send(map[string]any{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
	}
	mcpText := func(got map[string]any) string {
		return got["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	return map[string]overwriteEntry{
		"shikii wiki ingest on the command line": func(t *testing.T, f *overwriteFixture, d map[string]any) string {
			_, err := commandStdin(t, d, func() error { return cmdWiki([]string{"ingest"}) })
			if err != nil {
				return err.Error()
			}
			return ""
		},
		"shikii reflect submit on the command line": func(t *testing.T, f *overwriteFixture, d map[string]any) string {
			out, err := commandStdin(t, map[string]any{"wiki": []any{d}}, func() error { return cmdReflect([]string{"submit"}) })
			if err != nil {
				t.Fatal(err)
			}
			var reply map[string]any
			if err := json.Unmarshal([]byte(out), &reply); err != nil {
				t.Fatalf("the submit reply is JSON: %v %s", err, out)
			}
			return receiptError(t, reply)
		},
		"shikii_wiki ingest over MCP": func(t *testing.T, f *overwriteFixture, d map[string]any) string {
			args := map[string]any{"operation": "ingest"}
			for k, v := range d {
				args[k] = v
			}
			got := mcpCall("shikii_wiki", args)
			if got["isError"] == true {
				return mcpText(got)
			}
			return ""
		},
		"shikii_reflect submit over MCP": func(t *testing.T, f *overwriteFixture, d map[string]any) string {
			got := mcpCall("shikii_reflect", map[string]any{"operation": "submit", "wiki": []any{d}})
			if got["isError"] == true {
				t.Fatalf("a submit with a refused draft is a reply with a receipt, not a tool error: %v", got)
			}
			return receiptError(t, got["structuredContent"].(map[string]any))
		},
		"wiki ingest over HTTP": func(t *testing.T, f *overwriteFixture, d map[string]any) string {
			args := map[string]any{"operation": "ingest"}
			for k, v := range d {
				args[k] = v
			}
			data, _ := json.Marshal(args)
			code, body, err := callTool(f.h, "shikii_wiki", data)
			if err != nil {
				t.Fatal(err)
			}
			if code >= 400 {
				return string(body)
			}
			return ""
		},
		"reflect submit over HTTP": func(t *testing.T, f *overwriteFixture, d map[string]any) string {
			return receiptError(t, reflectCall(t, f.h, map[string]any{"operation": "submit", "wiki": []any{d}}))
		},
	}
}

func TestAgentEntriesRefuseABodyThatReplacesMostOfAnArticle(t *testing.T) {
	f := newOverwriteFixture(t)
	entries := overwriteEntries(t, f)
	if len(entries) != 6 {
		t.Fatalf("the CLI, MCP and HTTP forms of ingest and of submit are all tried: %d entries", len(entries))
	}
	for name, entry := range entries {
		t.Run(name, func(t *testing.T) {
			p := f.article(t, name)
			refusal := entry(t, f, f.draft(p, overwriteShort))
			for _, want := range []string{"refused", p.ID, "shikii wiki patch", "shikii wiki query lesson:" + p.ID} {
				if !strings.Contains(refusal, want) {
					t.Fatalf("a save that replaces the whole body with one sentence is not refused with the cause and the way on (lacks %q): %q", want, refusal)
				}
			}
			if revision, body := f.body(t, p.ID); revision != p.Revision || body != strings.TrimSpace(overwriteBody) {
				t.Fatalf("the refused save changed the article: %s %q", revision, body)
			}
			// A body that keeps 81 of the 219 bytes of the old lines (37%) is refused as well: the line is half of the bytes, not "nothing kept".
			if refusal := entry(t, f, f.draft(p, "## Background\n\n- Agents sent only their new sentences to an article's id.\n\n## Decided\n\nThe rest is new.")); !strings.Contains(refusal, "drop 63% of it") {
				t.Fatalf("a body that keeps 37%% of the old one is not refused for dropping 63%%: %q", refusal)
			}
			// The same entry saves the whole old body with an addition: the refusal is about what is lost, not about the entry.
			if refusal := entry(t, f, f.draft(p, overwriteBody+"\n- A line the agent adds.\n")); refusal != "" {
				t.Fatalf("the old body with an addition is refused: %s", refusal)
			}
			revision, body := f.body(t, p.ID)
			if revision == p.Revision || !strings.Contains(body, "A line the agent adds.") || !strings.Contains(body, "Shortening goes through wiki patch.") {
				t.Fatalf("the addition was not saved with the old body: %s %q", revision, body)
			}
			// And a rewording of one line, which keeps the rest of the body, is saved.
			reworded := strings.Replace(body, "- Shortening goes through wiki patch.", "- Shortening is done with wiki patch.", 1)
			if refusal := entry(t, f, f.draft(p, reworded)); refusal != "" {
				t.Fatalf("a rewording of one line is refused: %s", refusal)
			}
			if _, after := f.body(t, p.ID); !strings.Contains(after, "is done with wiki patch") {
				t.Fatalf("the rewording was not saved: %q", after)
			}
		})
	}
}

// What the refusal sends the agent to: wiki patch (command line and MCP) shortens the body, and the person's edit on the Knowledge page
// does too. They are the ways the refusal names, so they must not be refused for the same short body.
func TestPatchAndTheKnowledgePageStillShortenABodyThroughTheirEntries(t *testing.T) {
	f := newOverwriteFixture(t)
	patch := func(p notebook.Page) map[string]any {
		return map[string]any{"id": p.ID, "revision": p.Revision, "sources": f.sources, "change_reason": "shorten the body", "edits": []any{map[string]any{"field": "body", "before": strings.TrimSpace(overwriteBody), "after": overwriteShort}}}
	}
	p := f.article(t, "wiki patch on the command line")
	if _, err := commandStdin(t, patch(p), func() error { return cmdWiki([]string{"patch"}) }); err != nil {
		t.Fatalf("shikii wiki patch is refused: %v", err)
	}
	if _, body := f.body(t, p.ID); body != overwriteShort {
		t.Fatalf("the patch did not shorten the body: %q", body)
	}

	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, f.st.Dir)
	p = f.article(t, "wiki patch over MCP")
	args := map[string]any{"operation": "patch", "patch": patch(p)}
	got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}}))
	if got["isError"] == true {
		t.Fatalf("shikii_wiki patch is refused: %v", got)
	}
	if _, body := f.body(t, p.ID); body != overwriteShort {
		t.Fatalf("the patch over MCP did not shorten the body: %q", body)
	}

	p = f.article(t, "the Knowledge page")
	data, _ := json.Marshal(map[string]any{"id": p.ID, "revision": p.Revision, "action": "edit", "title": p.Title, "lesson": p.Lesson, "applies_to": p.AppliesTo, "body": overwriteShort, "kind": p.Kind, "scene": p.Scene})
	req := httptest.NewRequest("POST", "/v1/wiki/article", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("the person's edit on the Knowledge page is refused: %d %s", rec.Code, rec.Body.String())
	}
	if _, body := f.body(t, p.ID); body != overwriteShort {
		t.Fatalf("the person's edit did not shorten the body: %q", body)
	}
}
