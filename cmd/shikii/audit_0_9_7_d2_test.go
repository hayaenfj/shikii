package main

import "testing"

// 0.9.7 re-audit D-4: a handler registered through a middleware was taken as a writer-taking function used as a value.
func TestReauditDNilWriterFalsePositive(t *testing.T) {
	src := "package main\nimport \"net/http\"\nfunc withLog(h http.HandlerFunc) http.HandlerFunc { return func(w http.ResponseWriter, r *http.Request) { h(w, r) } }\nfunc (s *server) handleX(w http.ResponseWriter, r *http.Request) {}\nfunc (s *server) routes(mux *http.ServeMux) { mux.HandleFunc(\"/x\", withLog(s.handleX)) }\n"
	if got := nilWriters(t, map[string]string{"ok.go": src}); len(got) != 0 {
		t.Errorf("a correct registration through a middleware passes: %v", got)
	}
}
