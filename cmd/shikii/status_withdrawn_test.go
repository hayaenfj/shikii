package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// A person decides what becomes of a withdrawn article, so `shikii status` and the page's status say how many are waiting; the line is
// absent when none is, and the number follows the person's restore and delete.
func TestStatusSaysHowManyWithdrawnArticlesWaitForAPerson(t *testing.T) {
	repo := gitRepo(t)
	storeDir := hookEnv(t, fakeAgentModel(t, func(string) float64 { return .6 }), repo)
	book := notebook.Book{Root: storeDir, Scope: storeScope(storeDir)}
	h := newServer(serveOpts{storeDir: storeDir, projectScope: book.Scope})
	line := func() string {
		out, err := captureErr(func() error { return cmdStatus([]string{"--store", storeDir}) })
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "withdrawn articles:") {
				return l
			}
		}
		return ""
	}
	page := func() any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var out struct {
			Summary struct {
				Wiki map[string]any `json:"wiki"`
			} `json:"summary"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
		}
		return out.Summary.Wiki["withdrawn"]
	}
	if l := line(); l != "" || page() != 0.0 {
		t.Fatalf("no line and a count of 0 while nothing is withdrawn: %q %v", l, page())
	}
	var withdrawn []notebook.Page
	for n, title := range []string{"First", "Second"} {
		src, err := book.CaptureObserved(notebook.Observation{Kind: "agent", Title: "Work record " + title, Content: "what the work did " + title, Reference: "test", Version: "v1", ObservedAt: time.Unix(int64(n+1), 0)})
		if err != nil {
			t.Fatal(err)
		}
		r, err := book.Ingest(notebook.Draft{Title: title, Lesson: "Summary of " + title, AppliesTo: "Docs", Kind: "howto", Scene: "Docs", Sources: []string{src.ID}})
		if err != nil {
			t.Fatal(err)
		}
		w, err := book.Withdraw(notebook.Withdrawal{ID: r.Page.ID, Revision: r.Page.Revision, Sources: []string{src.ID}, Reason: "the request was deleted"})
		if err != nil {
			t.Fatal(err)
		}
		withdrawn = append(withdrawn, w)
	}
	if l := line(); !strings.HasPrefix(l, "withdrawn articles: 2,") || !strings.Contains(l, "Knowledge page") || page() != 2.0 {
		t.Fatalf("status says two withdrawn articles wait for a person: %q %v", l, page())
	}
	if _, err := book.Manage(notebook.Change{ID: withdrawn[0].ID, Revision: withdrawn[0].Revision, Action: "restore"}); err != nil {
		t.Fatal(err)
	}
	if l := line(); !strings.HasPrefix(l, "withdrawn articles: 1,") || page() != 1.0 {
		t.Fatalf("a restored article no longer waits: %q %v", l, page())
	}
	if _, err := book.Manage(notebook.Change{ID: withdrawn[1].ID, Revision: withdrawn[1].Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	if l := line(); l != "" || page() != 0.0 {
		t.Fatalf("a deleted article no longer waits: %q %v", l, page())
	}
}

// The hook hands an article to a request that matches it, stops handing it once a work withdrew it, and hands it again when the person
// restores it (each time in a session of its own, so that "already given in this session" does not hide the difference).
func TestTheHookDoesNotDeliverAWithdrawnArticleUntilAPersonRestoresIt(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	if memoryEffectPrompt(t, scope, "s1") == "" {
		t.Fatal("precondition: a request that matches the article is given it")
	}
	withdrawn, err := book.Withdraw(notebook.Withdrawal{ID: draft.ID, Revision: revision, Sources: draft.Sources, Reason: "the request was deleted"})
	if err != nil {
		t.Fatal(err)
	}
	if got := memoryEffectPrompt(t, scope, "s2"); got != "" {
		t.Fatalf("a withdrawn article is given to no request: %s", got)
	}
	if _, err := book.Manage(notebook.Change{ID: withdrawn.ID, Revision: withdrawn.Revision, Action: "restore"}); err != nil {
		t.Fatal(err)
	}
	if memoryEffectPrompt(t, scope, "s3") == "" {
		t.Fatal("a restored article is given to a request that matches it again")
	}
}
