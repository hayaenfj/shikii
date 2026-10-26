package main

import "testing"

// 0.9.7 audit E-1: ways around the general nil-writer check; the allowlist of lock callers catches them.
func TestReauditENilWriterBypasses(t *testing.T) {
	head := "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\n"
	for name, src := range map[string]string{
		"helper taking a handler-typed parameter calls it with nil": head + "func run(f func(http.ResponseWriter)) { f(nil) }\nfunc (s *server) handleA(w http.ResponseWriter) { run(func(w http.ResponseWriter) { defer s.lockCalibration(w)() }) }\n",
		"returned handler literal is called with nil":               head + "func (s *server) mk() func(http.ResponseWriter) { return func(w http.ResponseWriter) { defer s.lockCalibration(w)() } }\nfunc (s *server) handleA(w http.ResponseWriter) { s.mk()(nil) }\n",
		"package-level function literal called with nil":            head + "var g = func(s *server, w http.ResponseWriter) { defer s.lockCalibration(w)() }\nfunc (s *server) handleA(w http.ResponseWriter) { g(s, nil) }\n",
		"package-level method expression called with nil":           head + "var g = (*server).lockCalibration\nfunc (s *server) handleA(w http.ResponseWriter) { defer g(s, nil)() }\n",
		"address taken inside parentheses":                          head + "func (s *server) handleA(w http.ResponseWriter) { p := &(w); *p = nil; defer s.lockCalibration(w)() }\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": src}); len(got) == 0 {
			t.Errorf("bypass %q is not found", name)
		}
	}
}
