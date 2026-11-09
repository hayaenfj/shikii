package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	shikii "github.com/hayaenfj/shikii"
)

// handleUI serves the page (web/ built with Svelte). The build output web/dist is embedded in the binary (shikii.WebFS), so there are no external loads.
// "/" is index.html and "/assets/..." its JS and CSS. Any other unknown path is 404 (/v1/... is taken by the mux first).
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r) // no folder listing (/assets/). There is no HTML path other than "/"
		return
	}
	w.Header().Set("Cache-Control", "no-cache") // dist uses the same name every version (assets/index.js), so do not hold on to an old version
	webFiles.ServeHTTP(w, r)
}

var webFiles = http.FileServer(http.FS(mustSub(shikii.WebFS, "web/dist")))

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// writeUnknownSpec is the reply when the spec id cannot be resolved. If the id is empty, it says "a spec is needed" (when a first-time user sent only a trace_id to /v1/redo,
// `unknown spec ""` did not tell them what to add).
func writeUnknownSpec(w http.ResponseWriter, id string) {
	if id == "" {
		writeErr(w, 400, errors.New(`spec is required: the decision's spec id (the file name in decisions/ without .yaml, as in /v1/queue?spec=<id>)`))
		return
	}
	writeErr(w, 404, fmt.Errorf("unknown spec %q", id))
}

// localHostOnly passes only when Host is a local name (127.0.0.1, localhost, ::1). It refuses requests that came from another site's name through DNS rebinding
// (including GETs that read records and work records). A separate guard from the Origin check on POST (acceptJSONPost).
func localHostOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		switch strings.ToLower(strings.Trim(host, "[]")) {
		case "127.0.0.1", "localhost", "::1":
			h.ServeHTTP(w, r)
		default:
			writeErr(w, 403, fmt.Errorf("host %q is not allowed; open the page as http://127.0.0.1:<port>/ or http://localhost:<port>/", host))
		}
	})
}

// pageScope is the project path a serve of absStore keys its page lock and record by: the same spelling the hook uses, whatever path
// reached the folder (links resolved: 0.9.7 full audit 1, D4) and whatever case spelled it on a case-insensitive disk (final audit DF-1).
func pageScope(absStore string) string {
	scope := filepath.Dir(absStore)
	if real, err := filepath.EvalSymlinks(scope); err == nil {
		scope = real
	}
	return canonicalScope(scope)
}
