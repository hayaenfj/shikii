package main

import (
	"testing"
)

// Audit K of 0.31.0 (sixth round): the forms the two allowlist readers of the tests still do not see.

// INVARIANT 1: "a function that reads a JSON body on its own is allowed only in ownBodyReaders ... the test counts the forms of a
// function inside a function, another variable and a field of *http.Request". Each form below reads a request's body with a decoder
// of its own, and bodyReaders does not find it.
func TestAuditKBodyReadersMissForms(t *testing.T) {
	forms := map[string]string{
		"a handler declared as a package variable": "package main\nvar handleNew = func(w http.ResponseWriter, r *http.Request) { var x struct{}; _ = json.NewDecoder(r.Body).Decode(&x) }\n",
		"a request embedded in a struct":           "package main\ntype call struct{ *http.Request }\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) { c := call{r}; var x struct{}; _ = json.NewDecoder(c.Body).Decode(&x) }\n",
		"a variable declared with var":             "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) { var in = r; var x struct{}; _ = json.NewDecoder(in.Body).Decode(&x) }\n",
		"the body through (*r)":                    "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) { var x struct{}; _ = json.NewDecoder((*r).Body).Decode(&x) }\n",
	}
	for name, src := range forms {
		if found := bodyReaders(t, map[string]string{"new.go": src}); len(found) == 0 {
			t.Errorf("INVARIANT 1: %s: a new handler that reads its JSON body on its own is not found", name)
		}
	}
	if found := bodyReaders(t, map[string]string{"new.go": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) { var x struct{}; _ = json.NewDecoder(r.Body).Decode(&x) }\n"}); len(found) != 1 {
		t.Fatalf("control: the plain form is found: %v", found)
	}
}
