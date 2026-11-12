package main

import "testing"

// INVARIANT 1 ("a function that reads a JSON body on its own is in the test's allowlist, ownBodyReaders"): bodyReaders looks for
// <x>.Body only where x is a parameter of type *http.Request of a function declaration. A handler written as a function literal
// (the form of serve_ui.go:49 and project_registry.go:328), a request held in another variable, or one reached through a field is
// not seen, and the new reader passes the check.
func TestAuditIBodyReadersMissForms(t *testing.T) {
	for name, source := range map[string]string{
		"a handler as a function literal": "package main\nfunc (s *server) routesNew() http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\tvar req struct{}\n\t\t_ = json.NewDecoder(r.Body).Decode(&req)\n\t})\n}\n",
		"the request in another variable": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\treq := r\n\tvar v struct{}\n\t_ = json.NewDecoder(req.Body).Decode(&v)\n}\n",
		"the request in a field":          "package main\ntype call struct{ r *http.Request }\nfunc (c call) read() {\n\tvar v struct{}\n\t_ = json.NewDecoder(c.r.Body).Decode(&v)\n}\n",
	} {
		found := bodyReaders(t, map[string]string{"new_handler.go": source})
		t.Logf("%s: found %v", name, found)
		if len(found) == 0 {
			t.Errorf("INVARIANT 1: %s: a new function that reads its JSON body on its own is not found", name)
		}
	}
	// control: the declared handler is found
	if found := bodyReaders(t, map[string]string{"new_handler.go": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar v struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&v)\n}\n"}); len(found) != 1 {
		t.Errorf("control: %v", found)
	}
}
