package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestArticleHTTPChangesAreScopedAndRevisionChecked(t *testing.T) {
	root := t.TempDir()
	b := notebook.Book{Root: filepath.Join(root, ".shikii"), Scope: root}
	src, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Fixture", Content: "Article source", Reference: "test", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.Ingest(notebook.Draft{Title: "Article", Lesson: "Summary", AppliesTo: "Docs", Kind: "howto", Scene: "Docs", Sources: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	h := localHostOnly(newServer(serveOpts{storeDir: b.Root, projectScope: root}))
	c := notebook.Change{ID: r.Page.ID, Revision: r.Page.Revision, Action: "edit", Title: "New title", Lesson: "New summary", AppliesTo: "Docs", Body: "Plain <script>text</script>", Kind: "howto", Scene: "Docs"}
	do := func(method, body, origin, contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8095/v1/wiki/article", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	data, _ := json.Marshal(c)
	for _, tc := range []struct {
		method, body, origin, contentType string
		code                              int
	}{
		{"GET", "", "", "application/json", 405},
		{"POST", string(data), "https://other.invalid", "application/json", 403},
		{"POST", string(data), "", "text/plain", 415},
		{"POST", string(data) + "{}", "", "application/json", 400},
		{"POST", `{"action":"delete","id":"../outside","revision":"0000000000000000"}`, "", "application/json", 400},
		{"POST", `{"project":"/elsewhere"}`, "", "application/json", 400},
		{"POST", `{"body":"` + strings.Repeat("x", 128<<10) + `"}`, "", "application/json", 413},
	} {
		if w := do(tc.method, tc.body, tc.origin, tc.contentType); w.Code != tc.code {
			t.Fatalf("boundary %s %s: %d want %d", tc.method, tc.origin, w.Code, tc.code)
		}
	}
	w := do("POST", string(data), "http://127.0.0.1:8095", "application/json")
	var edited notebook.Page
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &edited) != nil || edited.Title != c.Title {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", string(data), "", "application/json"); w.Code != 409 {
		t.Fatalf("stale edit: %d", w.Code)
	}
	c.Action, c.Revision = "delete", edited.Revision
	data, _ = json.Marshal(c)
	if w := do("POST", string(data), "", "application/json"); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 0 {
		t.Fatalf("deleted article remains: %+v %v", v, err)
	}
}

// An article a work withdrew is listed on the Knowledge page apart from the live ones. The person restores it or deletes it there; the
// page cannot withdraw (only the program that made the article does, from the command line), edit a withdrawn article, or act on a
// revision other than the withdrawn one.
func TestArticleHTTPWithdrawnArticleIsRestoredOrDeletedByThePerson(t *testing.T) {
	root := t.TempDir()
	b := notebook.Book{Root: filepath.Join(root, ".shikii"), Scope: root}
	article := func(n int, title string) (notebook.Page, notebook.Page) {
		src, err := b.CaptureObserved(notebook.Observation{Kind: "agent", Title: "Work record " + title, Content: "what the work did " + title, Reference: "test", Version: "v1", ObservedAt: time.Unix(int64(n), 0)})
		if err != nil {
			t.Fatal(err)
		}
		r, err := b.Ingest(notebook.Draft{Title: title, Lesson: "Summary of " + title, AppliesTo: "Docs", Kind: "howto", Scene: "Docs", Sources: []string{src.ID}})
		if err != nil {
			t.Fatal(err)
		}
		w, err := b.Withdraw(notebook.Withdrawal{ID: r.Page.ID, Revision: r.Page.Revision, Sources: []string{src.ID}, Reason: "the request of " + title + " was deleted"})
		if err != nil {
			t.Fatal(err)
		}
		return *r.Page, w
	}
	h := localHostOnly(newServer(serveOpts{storeDir: b.Root, projectScope: root}))
	post := func(c map[string]any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(c)
		req := httptest.NewRequest("POST", "http://127.0.0.1:8095/v1/wiki/article", strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	list := func() (lessons, withdrawn []notebook.WithdrawnArticle) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8095/v1/wiki", nil))
		var got struct {
			Lessons   []notebook.WithdrawnArticle `json:"lessons"`
			Withdrawn []notebook.WithdrawnArticle `json:"withdrawn"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatalf("list: %d %s", w.Code, w.Body.String())
		}
		return got.Lessons, got.Withdrawn
	}
	liveBefore, withdrawnBefore := article(1, "First")
	deletedBefore, deletedWithdrawn := article(2, "Second")
	lessons, withdrawn := list()
	if len(lessons) != 0 || len(withdrawn) != 2 || withdrawn[0].Withdrawal.Reason == "" || len(withdrawn[0].Withdrawal.Work) != 1 {
		t.Fatalf("the page lists the withdrawn articles apart from the live ones, with why and from which work: %+v %+v", lessons, withdrawn)
	}
	for name, c := range map[string]map[string]any{
		"a withdrawal sent by the page":  {"id": liveBefore.ID, "revision": withdrawnBefore.Revision, "action": "withdraw"},
		"an edit of a withdrawn article": {"id": liveBefore.ID, "revision": withdrawnBefore.Revision, "action": "edit", "title": "Edited", "lesson": "Edited", "applies_to": "Docs", "kind": "howto", "scene": "Docs"},
	} {
		if w := post(c); w.Code < 400 {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := post(map[string]any{"id": liveBefore.ID, "revision": liveBefore.Revision, "action": "restore"}); w.Code != 409 {
		t.Fatalf("a restore that names the revision from before the withdrawal: %d", w.Code)
	}
	if w := post(map[string]any{"id": deletedBefore.ID, "revision": deletedBefore.Revision, "action": "delete"}); w.Code != 409 {
		t.Fatalf("a delete that names the revision from before the withdrawal: %d", w.Code)
	}
	if _, withdrawn := list(); len(withdrawn) != 2 {
		t.Fatalf("refused requests leave both articles withdrawn: %+v", withdrawn)
	}
	var restored, deleted notebook.Page
	if w := post(map[string]any{"id": liveBefore.ID, "revision": withdrawnBefore.Revision, "action": "restore"}); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &restored) != nil || restored.Status != "hypothesis" {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	if w := post(map[string]any{"id": deletedBefore.ID, "revision": deletedWithdrawn.Revision, "action": "delete"}); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &deleted) != nil || deleted.Status != "deleted" {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	lessons, withdrawn = list()
	if len(lessons) != 1 || lessons[0].ID != liveBefore.ID || len(withdrawn) != 0 {
		t.Fatalf("the restored article is live again and the deleted one is gone: %+v %+v", lessons, withdrawn)
	}
}
