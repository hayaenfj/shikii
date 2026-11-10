package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// lessonFiles returns the article files of the project's wiki.
func lessonFiles(t *testing.T, agent string) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join(agent, "wiki", "lessons", "*", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range all {
		id := strings.TrimSuffix(filepath.Base(f), ".md")
		if len(id) == 16 && strings.Trim(id, "0123456789abcdef") == "" { // the index and the log sit in the same folder
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		t.Fatalf("the seeded articles are on disk: %v", all)
	}
	return files
}

// One request reads the wiki in several steps (delivery, reflection, classification, the memory upgrade). The steps share the
// parse-and-verify pass, so an article is parsed once per hook event however many steps read it.
func TestOneHookEventParsesEachArticleOnce(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	scope := feedbackScope(repo)
	seedLessons(t, store.Store{Dir: agent}, scope, "Keep the changelog in two languages", "Write commit bodies in full", "Name the license in every file", "Enable a project explicitly")
	articles := len(lessonFiles(t, agent))

	before := notebook.PageParses()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "Update the changelog and write the commit body for the license change."})
	parses := int(notebook.PageParses() - before)
	if parses != articles {
		t.Fatalf("each of the %d articles is parsed once in one hook event, got %d passes", articles, parses)
	}
	if eventPages != nil {
		t.Fatal("the shared view ends with the event: a later request must not inherit it")
	}
}

// Outside a hook event nothing is shared: two reads are two passes (the view must not become a cache that outlives a request).
func TestWikiReadsOutsideAHookEventShareNothing(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	scope := feedbackScope(repo)
	seedLessons(t, store.Store{Dir: agent}, scope, "Keep the changelog in two languages", "Write commit bodies in full")
	articles := len(lessonFiles(t, agent))

	before := notebook.PageParses()
	for range 2 {
		if _, err := wikiBook(agent, scope).Live(); err != nil {
			t.Fatal(err)
		}
	}
	if parses := int(notebook.PageParses() - before); parses != 2*articles {
		t.Fatalf("two reads outside an event parse the %d articles twice, got %d passes", articles, parses)
	}
}

// Inside one event a changed article is never served from the shared pass: the bytes are read and compared every time, so an
// article edited by hand between two steps is reported as changed by the second step and comes back once it is restored.
func TestSharedEventReadsSeeAChangedArticle(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	scope := feedbackScope(repo)
	ids := seedLessons(t, store.Store{Dir: agent}, scope, "Keep the changelog in two languages", "Write commit bodies in full")
	edited := ids["Write commit bodies in full"]
	var path string
	for _, f := range lessonFiles(t, agent) {
		if strings.TrimSuffix(filepath.Base(f), ".md") == edited {
			path = f
		}
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	liveIDs := func() map[string]bool {
		t.Helper()
		live, err := wikiBook(agent, scope).Live()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range live {
			out[p.ID] = true
		}
		return out
	}

	end := beginEventReads()
	defer end()
	if !liveIDs()[edited] {
		t.Fatal("the article is live before the edit")
	}
	if err := os.WriteFile(path, append(append([]byte{}, original...), []byte("\nedited by hand\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if liveIDs()[edited] {
		t.Fatal("an article edited by hand must not be served from the pass shared within the event")
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if !liveIDs()[edited] {
		t.Fatal("the restored article is live again")
	}
}
