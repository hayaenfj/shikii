package main

import "testing"

// 0.9.7 audit E-5: correct code the general check judged wrong.
func TestReauditEFalsePositives(t *testing.T) {
	head := "package main\nimport (\"net/http\"; \"encoding/json\")\nvar _ = json.Unmarshal\n"
	for name, src := range map[string]string{
		"a range variable that only shares the writer's name":            head + "func (s *server) handleA(w http.ResponseWriter) { for _, word := range []string{\"a\"} { func() { for _, w := range []string{word} { _ = w } }() } }\n",
		"a local of another function literal named w decoded by address": head + "func (s *server) handleA(w http.ResponseWriter) { go func() { var v struct{}; func(w *struct{}) { _ = json.Unmarshal(nil, &w) }(&v) }() }\n",
	} {
		if got := nilWriters(t, map[string]string{"ok.go": src}); len(got) != 0 {
			t.Errorf("correct code %q is judged wrong: %v", name, got)
		}
	}
}
