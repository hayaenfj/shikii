package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// An agent adds to an existing article with shikii wiki patch (the way the guidance sends it first). A draft that was passed to place
// must get its truths from that patch as it does from an id save: the page added to is the same topic, the other candidates are not.
// The tests go in through each entry that saves a patch (command line, MCP, HTTP), not through the function behind them.

const patchPlaceHeading = "Changelog headings use the form version and title and never an unreleased heading"

type patchPlaceFixture struct {
	agent, scope string
	st           store.Store
	book         notebook.Book
	h            http.Handler
	source       string
	changelog    string // the ids of the three articles
	commit       string
	license      string
	draft        notebook.Draft
	placed       map[string]placeCandidate // by page id
}

// newPatchPlaceFixture makes a project with three articles, passes a draft to place (the model finds the changelog article closest), and
// returns it with the decisions recorded and no truth on any of them.
func newPatchPlaceFixture(t *testing.T) *patchPlaceFixture {
	t.Helper()
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.HasPrefix(state, "existing lesson:") { // wiki-conflict
			return 0.2
		}
		if strings.Contains(state, "existing page title: Changelog files") { // wiki-place
			return 0.9
		}
		return 0.1
	})
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	f := &patchPlaceFixture{agent: agent, scope: scope, st: store.Store{Dir: agent}, book: notebook.Book{Root: agent, Scope: scope}, source: fmt.Sprintf("%016x", 500), placed: map[string]placeCandidate{}}
	f.h = newServer(serveOpts{storeDir: agent, projectScope: scope})
	if err := f.st.AppendFeedback(scope, store.Feedback{ID: f.source, Request: "How are changelog headings written?", Summary: "Answered.", Reply: "Use version and title."}); err != nil {
		t.Fatal(err)
	}
	f.changelog = f.article(t, "Changelog files in English and Japanese", "Keep the changelog files in English and in Japanese.", "## Background\n\n- The changelog has an English file and a Japanese file.\n- Both are updated in one commit.")
	f.commit = f.article(t, "Commit messages explain the change", "Write the problem, the change and the check in a commit message.", "## Rule\n\n- A commit message has three parts.\n- The check is the command that was run.")
	f.license = f.article(t, "License files ship in every archive", "Put the three license files in every archive.", "## Rule\n\n- Every archive carries the license files.\n- Third party notices go along with them.")
	f.draft = notebook.Draft{Sources: []string{f.source}, Title: "Changelog headings", Lesson: patchPlaceHeading, Kind: "rule", Scene: "when testing", AppliesTo: "when writing changelog headings"}
	out, err := runReflect(agent, scope, reflectRequest{Operation: "place", Wiki: []notebook.Draft{f.draft}})
	if err != nil {
		t.Fatal(err)
	}
	candidates := out.(placeResult).Placements[0].Candidates
	if len(candidates) != 3 || candidates[0].PageID != f.changelog {
		t.Fatalf("the changelog article is the closest of three candidates: %+v", candidates)
	}
	for _, c := range candidates {
		if c.Conflict == nil {
			t.Fatalf("a conflict decision is recorded for each of the three candidates: %+v", c)
		}
		f.placed[c.PageID] = c
	}
	f.expectTruths(t, "after place, before any save", nil, nil)
	return f
}

func (f *patchPlaceFixture) article(t *testing.T, title, lesson, body string) string {
	t.Helper()
	saved, err := f.book.Ingest(notebook.Draft{Sources: []string{f.source}, Title: title, Lesson: lesson, AppliesTo: "when testing", Kind: "rule", Scene: "when testing", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	return saved.Page.ID
}

func (f *patchPlaceFixture) read(t *testing.T, id string) notebook.Page {
	t.Helper()
	got, err := f.book.Query("lesson:" + id)
	if err != nil || len(got.Lessons) != 1 {
		t.Fatalf("the article cannot be read: %v", err)
	}
	return got.Lessons[0]
}

// addPatch adds a line to the end of an article's body the way the guidance says: the last line as before, that line plus the new one as after.
func (f *patchPlaceFixture) addPatch(t *testing.T, id, sentence string) map[string]any {
	t.Helper()
	p := f.read(t, id)
	lines := strings.Split(p.Body, "\n")
	last := lines[len(lines)-1]
	return map[string]any{"id": p.ID, "revision": p.Revision, "sources": []string{f.source}, "change_reason": "add to the article", "edits": []any{map[string]any{"field": "body", "before": last, "after": last + "\n- " + sentence}}}
}

// expectTruths checks the labels on the decisions of the placed draft: place and conflict map a page id to {truth, weight} (a page not in
// the map must have no label).
func (f *patchPlaceFixture) expectTruths(t *testing.T, when string, place, conflict map[string][2]any) {
	t.Helper()
	labels := func(spec string, judged func(placeCandidate) *placeJudgement, want map[string][2]any) {
		got, err := f.st.LabeledTraces(spec)
		if err != nil {
			t.Fatal(err)
		}
		for id, c := range f.placed {
			j := judged(c)
			if j == nil {
				continue
			}
			l, has := got[j.TraceID]
			w, wanted := want[id]
			switch {
			case wanted && !has:
				t.Errorf("%s: %s has no truth on the page %s, want %v (all: %+v)", when, spec, id, w, got)
			case !wanted && has:
				t.Errorf("%s: %s has a truth on the page %s that nothing chose: %+v", when, spec, id, l)
			case wanted && has && (l.Truth != w[0] || l.Weight != w[1] || l.Source != "outcome"):
				t.Errorf("%s: %s on the page %s is %q weight %v source %q, want %v from outcome", when, spec, id, l.Truth, l.Weight, l.Source, w)
			}
		}
	}
	labels("wiki-place", func(c placeCandidate) *placeJudgement { return &c.SameTopic }, place)
	labels("wiki-conflict", func(c placeCandidate) *placeJudgement { return c.Conflict }, conflict)
}

// patchEntry sends the patch through one entry and returns why it was refused ("" when it was saved).
type patchEntry func(t *testing.T, f *patchPlaceFixture, patch map[string]any) string

func patchEntries() map[string]patchEntry {
	return map[string]patchEntry{
		"shikii wiki patch on the command line": func(t *testing.T, f *patchPlaceFixture, patch map[string]any) string {
			if _, err := commandStdin(t, patch, func() error { return cmdWiki([]string{"patch"}) }); err != nil {
				return err.Error()
			}
			return ""
		},
		"shikii_wiki patch over MCP": func(t *testing.T, f *patchPlaceFixture, patch map[string]any) string {
			send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, f.agent)
			args := map[string]any{"operation": "patch", "patch": patch}
			got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}}))
			if got["isError"] == true {
				return got["content"].([]any)[0].(map[string]any)["text"].(string)
			}
			return ""
		},
		"wiki patch over HTTP": func(t *testing.T, f *patchPlaceFixture, patch map[string]any) string {
			data, _ := json.Marshal(map[string]any{"operation": "patch", "patch": patch})
			code, body, err := callTool(f.h, "shikii_wiki", data)
			if err != nil {
				t.Fatal(err)
			}
			if code >= 400 {
				return string(body)
			}
			return ""
		},
	}
}

func TestAPatchOfAPlacedDraftAttachesTheTruthThroughEveryEntry(t *testing.T) {
	entries := patchEntries()
	if len(entries) != 3 {
		t.Fatalf("the command line, MCP and HTTP forms of wiki patch are all tried: %d entries", len(entries))
	}
	for name, entry := range entries {
		t.Run(name, func(t *testing.T) {
			f := newPatchPlaceFixture(t)
			// A patch that adds text unrelated to the placed draft attaches no truth to its decisions, though the article patched was a candidate.
			if refusal := entry(t, f, f.addPatch(t, f.license, "The archive also carries the third party notice file.")); refusal != "" {
				t.Fatalf("the unrelated patch is refused: %s", refusal)
			}
			f.expectTruths(t, "after a patch unrelated to the draft", nil, nil)

			// A patch that is refused (it names a revision the article does not have) saves nothing and attaches no truth, even with the draft's text.
			stale := f.addPatch(t, f.changelog, patchPlaceHeading+".")
			stale["revision"] = "0000000000000000"
			if refusal := entry(t, f, stale); refusal == "" {
				t.Fatal("a patch against a revision the article does not have is saved")
			}
			f.expectTruths(t, "after a refused patch", nil, nil)

			// The patch that adds the draft's lesson to the changelog article: that article is the same topic, the others are not, and it does
			// not disagree (it was added to, not replaced).
			if refusal := entry(t, f, f.addPatch(t, f.changelog, patchPlaceHeading+".")); refusal != "" {
				t.Fatalf("the patch is refused: %s", refusal)
			}
			if body := f.read(t, f.changelog).Body; !strings.Contains(body, patchPlaceHeading) {
				t.Fatalf("the patch did not add the sentence: %q", body)
			}
			f.expectTruths(t, "after the patch that adds the draft",
				map[string][2]any{f.changelog: {"yes", placeChosenWeight}, f.commit: {"no", placeOtherWeight}, f.license: {"no", placeOtherWeight}},
				map[string][2]any{f.changelog: {"no", placeOtherWeight}})
		})
	}
}

// The notes of a work record save a patch too (a note with a target and its edits). It goes through the same save.
func TestAWorkRecordNoteThatPatchesAPlacedDraftsArticleAttachesTheTruth(t *testing.T) {
	f := newPatchPlaceFixture(t)
	prepared, err := workCall(t, f.book, "prepare", map[string]any{"query": "lesson:" + f.changelog, "request": "Check the changelog headings"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, raw := range prepared["knowledge"].([]any) {
		if entry := raw.(map[string]any); entry["id"] == f.changelog {
			ref, _ = entry["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("the prepared work does not hold the article to patch: %v", prepared["knowledge"])
	}
	p := f.read(t, f.changelog)
	note := func(sentence string) map[string]any {
		return map[string]any{"target": ref, "sources": []string{"new1"}, "change_reason": "add to the article", "edits": []any{map[string]any{"field": "body", "before": "- Both are updated in one commit.", "after": "- Both are updated in one commit.\n- " + sentence}}}
	}
	saved, err := workCall(t, f.book, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Heading check", "content": "The headings were checked."}}, "notes": []any{note(patchPlaceHeading + ".")}})
	if err != nil {
		t.Fatal(err)
	}
	if body := f.read(t, f.changelog).Body; !strings.Contains(body, patchPlaceHeading) || f.read(t, f.changelog).Revision == p.Revision {
		t.Fatalf("the note's patch was not saved: %v %q", saved["errors"], body)
	}
	f.expectTruths(t, "after a work record note that patches the article",
		map[string][2]any{f.changelog: {"yes", placeChosenWeight}, f.commit: {"no", placeOtherWeight}, f.license: {"no", placeOtherWeight}},
		map[string][2]any{f.changelog: {"no", placeOtherWeight}})
}

// The decisions point at the lessons the pages had when the draft was placed. A patch that rewrites the lesson of the page it adds to
// still finds them, because the pages are read before the patch is saved.
func TestAPatchThatRewritesTheLessonStillAttachesTheTruth(t *testing.T) {
	f := newPatchPlaceFixture(t)
	p := f.read(t, f.changelog)
	patch := &notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{f.source}, ChangeReason: "fold the new rule into the lesson", Edits: []notebook.TextEdit{
		{Field: "lesson", Before: p.Lesson, After: strings.TrimSuffix(p.Lesson, ".") + "; " + patchPlaceHeading + "."},
		{Field: "body", Before: "- Both are updated in one commit.", After: "- Both are updated in one commit.\n- " + patchPlaceHeading + "."},
	}}
	if _, err := runNotebook(f.agent, f.scope, notebookRequest{Operation: "patch", Patch: patch}); err != nil {
		t.Fatal(err)
	}
	if got := f.read(t, f.changelog).Lesson; got == p.Lesson {
		t.Fatalf("the patch did not rewrite the lesson: %q", got)
	}
	f.expectTruths(t, "after a patch that rewrites the lesson",
		map[string][2]any{f.changelog: {"yes", placeChosenWeight}, f.commit: {"no", placeOtherWeight}, f.license: {"no", placeOtherWeight}},
		map[string][2]any{f.changelog: {"no", placeOtherWeight}})
}

// Attaching the truth never fails the patch: when the decisions cannot be read, the patch is saved, no truth is attached, and the cause is
// written to the log as it is for an id save.
func TestAPatchIsSavedWhenTheDecisionsCannotBeRead(t *testing.T) {
	f := newPatchPlaceFixture(t)
	records := f.st.RecordsFile("wiki-place")
	if err := os.Remove(records); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(records, 0o755); err != nil { // a folder where the file of decisions was: reading it fails
		t.Fatal(err)
	}
	if _, err := runNotebook(f.agent, f.scope, notebookRequest{Operation: "patch", Patch: patchOf(t, f.addPatch(t, f.changelog, patchPlaceHeading+"."))}); err != nil {
		t.Fatalf("a patch whose truths cannot be attached is refused: %v", err)
	}
	if body := f.read(t, f.changelog).Body; !strings.Contains(body, patchPlaceHeading) {
		t.Fatalf("the patch was not saved: %q", body)
	}
	if labels, _ := f.st.LabeledTraces("wiki-place"); len(labels) != 0 {
		t.Fatalf("a truth was attached from decisions that could not be read: %+v", labels)
	}
	home, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	if log, _ := os.ReadFile(filepath.Join(home, "hook.log")); !strings.Contains(string(log), "wiki place:") {
		t.Fatalf("the cause is not in the log: %q", log)
	}
}

func patchOf(t *testing.T, m map[string]any) *notebook.Patch {
	t.Helper()
	data, _ := json.Marshal(m)
	var p notebook.Patch
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return &p
}
