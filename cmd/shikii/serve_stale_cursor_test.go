package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.9.5: a cursor made before a page changed is answered with 409, not 400, so the page can tell it apart from a bad request and read the
// knowledge list again from the start instead of showing "the wiki cursor does not match ...".
func TestStaleWikiCursorIs409(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	var last notebook.Page
	for i := range 40 {
		d := draft
		d.ID, d.Title = "", fmt.Sprintf("Knowledge %03d", i)
		d.AppliesTo = strings.Repeat("Knowledge scope ", 60)
		body := strings.Repeat("Detailed evidence. ", 200)
		d.Body = &body
		r, err := b.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		last = *r.Page
	}
	get := func(target string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, first := get("/v1/wiki")
	cursor, _ := first["next_cursor"].(string)
	if code != 200 || cursor == "" {
		t.Fatalf("the list comes in parts: %d %q", code, cursor)
	}
	d := draft
	d.ID, d.Title, d.Lesson = last.ID, last.Title, "Revised knowledge"
	if _, err := b.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if code, out := get("/v1/wiki?cursor=" + url.QueryEscape(cursor)); code != http.StatusConflict {
		t.Fatalf("/v1/wiki: a cursor from before a page changed is 409: %d %v", code, out["error"])
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/wiki/notebook", strings.NewReader(`{"operation":"query","cursor":"`+cursor+`"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("/v1/wiki/notebook: a cursor from before a page changed is 409: %d %s", rec.Code, rec.Body.String())
	}
}
