package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func (s *server) handleArticle(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	var change notebook.Change
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&change); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeErr(w, 413, errors.New("article request exceeds 128 KiB"))
		} else {
			writeErr(w, 400, errors.New("invalid article request"))
		}
		return
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		writeErr(w, 400, errors.New("article request must contain exactly one JSON object"))
		return
	}
	p, err := (notebook.Book{Root: s.o.storeDir, Scope: s.o.projectScope}).Manage(change)
	if err != nil {
		code := 400
		if errors.Is(err, notebook.ErrArticleConflict) {
			code = 409
		}
		if errors.Is(err, notebook.ErrArticleMissing) {
			code = 404
		}
		writeErr(w, code, err)
		return
	}
	writeJSON(w, 200, p)
}
