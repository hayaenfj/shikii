package main

import (
	"go/ast"
	"go/constant"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Audit M of 0.31.0: the readers that read the code by its types. Each negative below is a file added to the real package (the
// strict reading, as the shipped tests read the package), so the forms are ones the package compiles with.

func auditMPackageWith(t *testing.T, name, src string) map[string]string {
	t.Helper()
	files := map[string]string{}
	paths, _ := filepath.Glob("*.go")
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(data)
	}
	files[name] = src
	return files
}

// INVARIANT 10 (the test side): "the places of cmd/shikii that ... call CaptureObserved are the allowlist of the typed test". The
// reader looks for CaptureObserved as the function of a call (a CallExpr whose Fun is the selector); a method value taken first
// and called later is the same call to the type checker, and it is not found. A workObservation converted from a struct of the
// same fields (the kind source set) is built without a composite literal of the type, and it is not found either.
func TestAuditMTheObservationReaderMissesForms(t *testing.T) {
	control := auditMPackageWith(t, "zz_new.go", "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nfunc newArchive(book notebook.Book) { _, _ = book.CaptureObserved(notebook.Observation{}) }\n")
	if problems := workObservationProblems(t, control); !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "zz_new.go:") }) {
		t.Fatalf("control: a call of CaptureObserved outside the list is found: %v", problems)
	}
	for name, src := range map[string]string{
		"a method value": "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nfunc newArchive(book notebook.Book) {\n\tsave := book.CaptureObserved\n\t_, _ = save(notebook.Observation{})\n}\n",
		"a conversion":   "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nfunc newArchive() workObservation {\n\treturn workObservation(struct {\n\t\tKind      string `json:\"kind\"`\n\t\tTitle     string `json:\"title\"`\n\t\tContent   string `json:\"content\"`\n\t\tReference string `json:\"reference,omitempty\"`\n\t\tVersion   string `json:\"version,omitempty\"`\n\t\tsource    notebook.WorkSource\n\t}{Kind: \"agent\", Title: \"A new archive\", source: notebook.WorkTrialExecution})\n}\n",
	} {
		problems := workObservationProblems(t, auditMPackageWith(t, "zz_new.go", src))
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "zz_new.go:") }) {
			t.Errorf("INVARIANT 10 (test side): %s: a new place outside the allowlist that saves an observation or builds a workObservation is not found (problems: %v)", name, problems)
		}
	}
}

// INVARIANT 1: "the functions that read a JSON body on their own are the allowlist ownBodyReaders; the test reads by type". The
// reader allows a Body handed to a function by the name decodeObject (the identifier's name, not the object it denotes), and it
// counts only the selector Body: a local function named decodeObject that reads the body with encoding/json, and a handler that
// reads the whole request with net/http/httputil.DumpRequest, are not found.
func TestAuditMTheBodyReaderMissesForms(t *testing.T) {
	control := auditMPackageWith(t, "zz_new.go", "package main\n\nimport (\n\t\"encoding/json\"\n\t\"net/http\"\n)\n\nfunc (s *server) handleAuditM(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n")
	if !slices.Contains(bodyReaders(t, control), "handleAuditM") {
		t.Fatalf("control: a handler that reads its body is found")
	}
	for name, src := range map[string]string{
		"a local function named decodeObject": "package main\n\nimport (\n\t\"encoding/json\"\n\t\"io\"\n\t\"net/http\"\n)\n\nfunc (s *server) handleAuditM(w http.ResponseWriter, r *http.Request) {\n\tdecodeObject := func(body io.Reader, v any) error { return json.NewDecoder(body).Decode(v) }\n\tvar req struct{}\n\t_ = decodeObject(r.Body, &req)\n}\n",
		"the request dumped":                  "package main\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\t\"net/http\"\n\t\"net/http/httputil\"\n)\n\nfunc (s *server) handleAuditM(w http.ResponseWriter, r *http.Request) {\n\traw, _ := httputil.DumpRequest(r, true)\n\t_, body, _ := bytes.Cut(raw, []byte(\"\\r\\n\\r\\n\"))\n\tvar req struct{}\n\t_ = json.Unmarshal(body, &req)\n}\n",
	} {
		if found := bodyReaders(t, auditMPackageWith(t, "zz_new.go", src)); !slices.Contains(found, "handleAuditM") {
			t.Errorf("INVARIANT 1: %s: a new handler that reads its JSON body on its own is not found (found: %v)", name, found)
		}
	}
}

// The snippets of the shipped negatives are read against typedPrelude, whose workObservation has a field report bool and no field
// source (the type of work.go has source notebook.WorkSource): a negative that sets the kind of a source is read against a type
// that has no such field, its error is let pass (a snippet is not strict), and the reader is blind to it. The same line in a file of
// the real package is found.
func TestAuditMThePreludeIsNotTheType(t *testing.T) {
	src := "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nfunc newArchive(o workObservation) workObservation {\n\to.source = notebook.WorkResult\n\treturn o\n}\n"
	inPackage := workObservationProblems(t, auditMPackageWith(t, "zz_new.go", src))
	asSnippet := workObservationProblems(t, map[string]string{"new.go": src})
	t.Logf("in the package: %v; as a snippet: %v", inPackage, asSnippet)
	if len(asSnippet) == 0 && slices.ContainsFunc(inPackage, func(p string) bool { return strings.HasPrefix(p, "zz_new.go:") }) {
		t.Errorf("a snippet that sets source is not found against typedPrelude (its workObservation has report, not source), while the same file in the package is")
	}
}

// INVARIANT 10 (type side): "the title is the kind's" (REDESIGN: the table of titles is no longer needed). A reader that tells a
// work source by the title written out as a string, not by notebook.WorkSourceOf, is a second table: it compares without the
// spaces WorkSourceOf leaves out, and it would not follow a change of the constant. Every string literal of the code that equals a
// title of a work source, outside internal/notebook/work_sources.go.
func TestAuditMNoTitleIsWrittenOutOfItsKind(t *testing.T) {
	titles := map[string]bool{}
	for _, k := range workSources() {
		titles[k.Title()] = true
	}
	var found []string
	for _, dir := range []string{".", "../../internal/notebook", "../../internal/store"} {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		sources := map[string]string{}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sources[filepath.Base(path)] = string(data)
		}
		found = append(found, workTitleConstants(t, sources, titles, true)...)
	}
	for _, f := range found {
		t.Errorf("a title of a work source written out of its kind: %s", f)
	}
	// negative: a title written as a constant, a sum of constants or a literal is found, by its value
	for name, src := range map[string]string{
		"a literal":         "package main\nfunc f(title string) bool { return title == \"Work result and remaining conditions\" }\n",
		"a constant":        "package main\nconst done = \"Work result and remaining conditions\"\nfunc f(title string) bool { return title == done }\n",
		"a sum of literals": "package main\nfunc f(title string) bool { return title == \"Work result\"+\" and remaining conditions\" }\n",
	} {
		if got := workTitleConstants(t, map[string]string{"f.go": src}, titles, false); len(got) == 0 {
			t.Errorf("negative (%s): a title of a work source written out of its kind is not found", name)
		}
	}
}

// workTitleConstants reads the sources by their types and returns where a string constant (a literal, a named constant, a sum of
// them) has the value of a title of a work source, outside work_sources.go, which owns the titles.
func workTitleConstants(t *testing.T, sources map[string]string, titles map[string]bool, strict bool) []string {
	t.Helper()
	var found []string
	for _, tf := range typedSources(t, sources, strict) {
		if tf.prelude || tf.name == "work_sources.go" {
			continue
		}
		info := tf.info
		typedDecls(tf.file, func(name string, root ast.Node) {
			ast.Inspect(root, func(n ast.Node) bool {
				e, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String && titles[constant.StringVal(tv.Value)] {
					found = append(found, tf.name+":"+name)
					return false // the outermost constant expression counts once
				}
				return true
			})
		})
	}
	return found
}
