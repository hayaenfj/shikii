package main

import (
	"testing"
)

// Audit N of 0.31.0: forms the typed readers of the tests let through. Each reader reads types, and still decides by a name or by
// one shape of expression somewhere: a form outside that shape is not found.

// bodyReaders allows a Body handed to a function called decodeObject, decodeReflect or decodeReview by the name of the identifier,
// not by the object it denotes: a local function of that name that reads the body itself is allowed. A handler that reads its body
// through net/http/httputil (no Body in the code) is not found either.
func TestAuditNBodyReadersMissForms(t *testing.T) {
	control := bodyReaders(t, map[string]string{"new_handler.go": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n"})
	if len(control) != 1 {
		t.Fatalf("control: a handler that decodes its body is found: %v", control)
	}
	for name, src := range map[string]string{
		"a local function named like the reader": "package main\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tdecodeObject := func(body io.Reader, v any) error { return json.NewDecoder(body).Decode(v) }\n\tvar req struct{}\n\t_ = decodeObject(r.Body, &req)\n}\n",
		"httputil.DumpRequest":                   "package main\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"net/http/httputil\"\n)\nfunc (s *server) handleNew(w http.ResponseWriter, r *http.Request) {\n\tdump, _ := httputil.DumpRequest(r, true)\n\tvar req struct{}\n\t_ = json.Unmarshal(dump[bytes.Index(dump, []byte(\"\\r\\n\\r\\n\"))+4:], &req)\n}\n",
	} {
		if got := bodyReaders(t, map[string]string{"new_handler.go": src}); len(got) == 0 {
			t.Errorf("INVARIANT 1: %s: a new handler that reads its JSON body on its own is not found", name)
		}
	}
}

// workObservationProblems finds a call of CaptureObserved only as a call whose function is the selector itself: the method taken as a
// value, or the selector in parentheses, saves an observation outside the list and is not found.
func TestAuditNObservationSaverForms(t *testing.T) {
	for name, src := range map[string]string{
		"a method value":         "package main\nfunc newArchive(book notebook.Book) { save := book.CaptureObserved; _, _ = save(notebook.Observation{Kind: \"agent\", Title: \"A new archive\"}) }\n",
		"the selector in parens": "package main\nfunc newArchive(book notebook.Book) { _, _ = (book.CaptureObserved)(notebook.Observation{Kind: \"agent\", Title: \"A new archive\"}) }\n",
		"a method expression":    "package main\nfunc newArchive(book notebook.Book) { _, _ = notebook.Book.CaptureObserved(book, notebook.Observation{}) }\n",
	} {
		if problems := workObservationProblems(t, map[string]string{"new.go": src}); len(problems) == 0 {
			t.Errorf("INVARIANT 10: %s: an observation saved outside observationSavers is not found", name)
		}
	}
}

// specPathBuilders finds a question file path joined with filepath.Join only when ".yaml" is inside the call's arguments, or a sum
// whose constant parts hold "decisions/": the suffix added to the joined path, or a Sprintf, is not found.
func TestAuditNSpecPathForms(t *testing.T) {
	for name, src := range map[string]string{
		"the suffix after Join": "package main\nimport \"path/filepath\"\nfunc load(agent, id string) string { return filepath.Join(agent, \"decisions\", id) + \".yaml\" }\n",
		"Sprintf":               "package main\nimport \"fmt\"\nfunc load(root, id string) string { return fmt.Sprintf(\"%s/decisions/%s.yaml\", root, id) }\n",
	} {
		if problems := specPathBuilders(t, map[string]string{"load.go": src}); len(problems) == 0 {
			t.Errorf("%s: a question file path built outside hookSpecPath is not found", name)
		}
	}
}
