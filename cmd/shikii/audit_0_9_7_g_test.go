package main

import (
	"strings"
	"testing"
)

// 0.9.7 audit G-1: the allowlist stopped at handler(): the whole server run with a recorder or nil (s.handler().ServeHTTP(rec, r)) reached
// the listed handlers again. A writer other than the function's own may be passed to ServeHTTP only where writerArgAllowed says (MCP's
// in-process recorder, which has no write deadline); a registration counts only as mux.HandleFunc inside (*server).handler.
func TestReauditGTheWholeServerIsBoundToo(t *testing.T) {
	head := "package main\nimport (\"net/http\"; \"net/http/httptest\")\nvar _ = httptest.NewRecorder\n"
	for name, body := range map[string]string{
		"the whole server run with a recorder": "func (s *server) handleBatch(w http.ResponseWriter, r *http.Request) { s.handler().ServeHTTP(httptest.NewRecorder(), r) }\n",
		"the whole server run with nil":        "func (s *server) handleBatch(w http.ResponseWriter, r *http.Request) { s.handler().ServeHTTP(nil, r) }\n",
		"a registration by another type":       "type other struct{}\nfunc (o *other) handler(mux *http.ServeMux, s *server) { mux.HandleFunc(\"/x\", s.handleLabel) }\n",
		"a HandleFunc that is not a mux":       "type fake struct{}\nfunc (f fake) HandleFunc(p string, h func(http.ResponseWriter, *http.Request)) { h(nil, nil) }\nfunc (s *server) handler() { var f fake; f.HandleFunc(\"/x\", s.handleLabel) }\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": head + body}); len(got) == 0 {
			t.Errorf("%s: not found", name)
		}
	}
}

// 0.9.7 audit G-3: the full organizing notice and the notice for a missing query still taught lesson:<id>, which submit refuses; the key
// query returns is lesson:<id>:<revision>.
func TestReauditGNoticesTeachTheLessonKeyQueryReturns(t *testing.T) {
	for name, text := range map[string]string{"full": planningNotice("s", "0123456789abcdef"), "short": planningNoticeShort("s", "0123456789abcdef"), "no query": planningNoQueryNotice} {
		if !strings.Contains(text, "lesson:<id>:<revision>") {
			t.Errorf("%s: names the key query returns: %q", name, text)
		}
	}
}
