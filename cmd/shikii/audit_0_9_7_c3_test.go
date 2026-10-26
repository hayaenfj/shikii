package main

import "testing"

// 0.9.7 re-audit C-4: more ways around the nil-writer rule.
func TestReauditCNilWriterLoopholes(t *testing.T) {
	head := "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\n"
	for name, body := range map[string]string{
		"the parameter set to nil":         "func (s *server) handleA(w http.ResponseWriter) { w = nil; defer s.lockCalibration(w)() }\n",
		"nil passed to a function literal": "func (s *server) handleA(w http.ResponseWriter) { func(x http.ResponseWriter) { defer s.lockCalibration(x)() }(nil) }\n",
		"a method value called with nil":   "func (s *server) handleA(w http.ResponseWriter) { f := s.lockCalibration; defer f(nil)() }\n",
		"a parameter typed by an alias":    "type rw = http.ResponseWriter\nfunc (s *server) lockB(w rw) func() { return nil }\nfunc (s *server) handleA(w http.ResponseWriter) { defer s.lockB(nil)() }\n",
	} {
		src := head + body
		if name == "a parameter typed by an alias" {
			src = "package main\nimport \"net/http\"\n" + body
		}
		if got := nilWriters(t, map[string]string{"bad.go": src}); len(got) == 0 {
			t.Errorf("%s: not found (a way around the rule)", name)
		}
	}
}
