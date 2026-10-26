package main

import "testing"

// 0.9.7 re-audit D-3: three more ways around the nil-writer rule.
func TestReauditDNilWriterLoopholes(t *testing.T) {
	head := "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\n"
	for name, body := range map[string]string{
		"the parameter set to nil by range":                "func (s *server) handleA(w http.ResponseWriter) { for _, w = range []http.ResponseWriter{nil} {}; defer s.lockCalibration(w)() }\n",
		"the parameter set to nil through a pointer":       "func (s *server) handleA(w http.ResponseWriter) { p := &w; *p = nil; defer s.lockCalibration(w)() }\n",
		"a function literal in a variable called with nil": "func (s *server) handleA(w http.ResponseWriter) { f := func(x http.ResponseWriter) { defer s.lockCalibration(x)() }; f(nil) }\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": head + body}); len(got) == 0 {
			t.Errorf("the way around %q is found", name)
		}
	}
}
