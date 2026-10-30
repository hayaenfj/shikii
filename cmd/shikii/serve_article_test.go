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
	src, err := b.Capture(notebook.Observation{Kind: "test", Title: "Fixture", Content: "Article source", Reference: "test", Version: "v1", ObservedAt: time.Unix(1, 0)})
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
