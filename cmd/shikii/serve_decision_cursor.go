package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
)

// A continuation is valid only for the same ordered snapshot and endpoint.
// Offsets are checked before slicing; a changed history requires a fresh read.
func decisionPage(w http.ResponseWriter, r *http.Request, count, limit int, snapshot any) (start, end int, next string, ok bool) {
	if count <= limit && r.URL.Query().Get("cursor") == "" {
		return 0, count, "", true
	}
	hash := sha256.New()
	if err := json.NewEncoder(hash).Encode(snapshot); err != nil {
		writeErr(w, 500, err)
		return
	}
	revision := hex.EncodeToString(hash.Sum(nil))
	type cursor struct {
		Offset   int    `json:"offset"`
		Revision string `json:"revision"`
		Path     string `json:"path"`
	}
	if token := r.URL.Query().Get("cursor"); token != "" {
		if len(token) > 512 {
			writeErr(w, 400, errors.New("invalid decision list cursor"))
			return
		}
		var c cursor
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Offset < 1 {
			writeErr(w, 400, errors.New("invalid decision list cursor"))
			return
		}
		if c.Revision != revision || c.Path != r.URL.Path || c.Offset >= count {
			writeErr(w, 409, errors.New("the decision list changed; read again from the start"))
			return
		}
		start = c.Offset
	}
	end = start + min(limit, count-start)
	if end < count {
		raw, _ := json.Marshal(cursor{Offset: end, Revision: revision, Path: r.URL.Path})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	ok = true
	return
}
