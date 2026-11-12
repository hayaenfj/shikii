package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// Audit O of 0.31.0: the typed readers after the fixes of M-U1, M-U3 and N-U3. Each negative is a file added to (or a function
// appended to a file of) the real package, read strictly as the shipped tests read it, so every form compiles.

// TestAuditMNoTitleIsWrittenOutOfItsKind (shipped with the fix of M-U3) skips every file named work_sources.go, and cmd/shikii has
// one of its own (prepareWorkSource and the budget of originals), besides internal/notebook/work_sources.go, which owns the
// titles. A title written out as a literal in cmd/shikii/work_sources.go is not found; the same function in another file is.
func TestAuditOTitleReaderSkipsTheCommandsWorkSources(t *testing.T) {
	titles := map[string]bool{}
	for _, k := range workSources() {
		titles[k.Title()] = true
	}
	const reader = "\n\nfunc auditOIsResult(title string) bool { return title == \"Work result and remaining conditions\" }\n"
	control := auditMPackageWith(t, "zz_new.go", "package main\n"+reader)
	if got := workTitleConstants(t, control, titles, true, ""); !slices.ContainsFunc(got, func(p string) bool { return strings.HasPrefix(p, "zz_new.go:") }) {
		t.Fatalf("control: a title written out in a new file is found: %v", got)
	}
	data, err := os.ReadFile("work_sources.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "package main") {
		t.Fatalf("precondition: cmd/shikii has a work_sources.go of its own")
	}
	files := auditMPackageWith(t, "work_sources.go", string(data)+reader)
	got := workTitleConstants(t, files, titles, true, "")
	t.Logf("found with the reader appended to cmd/shikii/work_sources.go: %v", got)
	if !slices.ContainsFunc(got, func(p string) bool { return strings.Contains(p, "auditOIsResult") }) {
		t.Errorf("INVARIANT 10 (test side): a title written out in cmd/shikii/work_sources.go is not found: the reader skips every file of that name, not only internal/notebook/work_sources.go")
	}
}

// The observation reader of the fix of M-U1 counts CaptureObserved when the selection's object is the notebook's method, and a
// field set when the selection's receiver is workObservation. A call through an interface of package main (whose method is an
// object of main) and a field set through a struct that embeds workObservation (whose receiver is the outer struct) are the same
// save and the same build to the compiler, and neither is found.
func TestAuditOObservationReaderMissesInterfaceAndEmbedding(t *testing.T) {
	control := auditMPackageWith(t, "zz_new.go", "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nfunc auditOSave(book notebook.Book) { _, _ = book.CaptureObserved(notebook.Observation{}) }\n")
	if problems := workObservationProblems(t, control); !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "zz_new.go:") }) {
		t.Fatalf("control: a call of CaptureObserved outside the list is found: %v", problems)
	}
	for name, src := range map[string]string{
		"through an interface of main":           "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\ntype auditOSaver interface {\n\tCaptureObserved(notebook.Observation) (notebook.Observation, error)\n}\n\nfunc auditOSave(book notebook.Book) {\n\tvar s auditOSaver = book\n\t_, _ = s.CaptureObserved(notebook.Observation{Kind: \"agent\", Title: \"A new archive\"})\n}\n",
		"a field of an embedded workObservation": "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\ntype auditOWrapped struct{ workObservation }\n\nfunc auditOArchive() workObservation {\n\tvar w auditOWrapped\n\tw.source = notebook.WorkResult()\n\tw.Content = \"a report written outside workArchive\"\n\treturn w.workObservation\n}\n",
	} {
		problems := workObservationProblems(t, auditMPackageWith(t, "zz_new.go", src))
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, "zz_new.go:") }) {
			t.Errorf("INVARIANT 10 (test side): %s: a new place outside the allowlist that saves an observation or sets a field of a workObservation is not found (problems: %v)", name, problems)
		}
	}
}

// The body reader of the fix of M-U1/N-U3 allows a Body handed to decodeObject by the object, yet still allows
// `r.Body = <anything>.MaxBytesReader(w, r.Body, n)` by the selector's name: a method of package main named MaxBytesReader that
// decodes the body it is given reads the request's JSON on its own, unseen. A second function with a listed name (a method
// handleArticle of another type) is merged with the listed one, as found is keyed by the name alone.
func TestAuditOBodyReaderAllowsByName(t *testing.T) {
	control := auditMPackageWith(t, "zz_new.go", "package main\n\nimport (\n\t\"encoding/json\"\n\t\"net/http\"\n)\n\nfunc (s *server) handleAuditO(w http.ResponseWriter, r *http.Request) {\n\tr.Body = http.MaxBytesReader(w, r.Body, 1<<20)\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n")
	if !slices.Contains(bodyReaders(t, control), "handleAuditO") {
		t.Fatalf("control: a handler that bounds and then reads its body is found")
	}
	byName := auditMPackageWith(t, "zz_new.go", "package main\n\nimport (\n\t\"encoding/json\"\n\t\"io\"\n\t\"net/http\"\n)\n\ntype auditOBound struct{}\n\nfunc (auditOBound) MaxBytesReader(w http.ResponseWriter, body io.ReadCloser, n int64) io.ReadCloser {\n\tvar req struct{}\n\t_ = json.NewDecoder(body).Decode(&req)\n\treturn body\n}\n\nfunc (s *server) handleAuditO(w http.ResponseWriter, r *http.Request) {\n\tr.Body = auditOBound{}.MaxBytesReader(w, r.Body, 1<<20)\n}\n")
	if found := bodyReaders(t, byName); !slices.Contains(found, "handleAuditO") && !slices.Contains(found, "MaxBytesReader") {
		t.Errorf("INVARIANT 1 (test side): a method named MaxBytesReader that decodes the body is allowed by its name: found %v", found)
	}
	merged := auditMPackageWith(t, "zz_new.go", "package main\n\nimport (\n\t\"encoding/json\"\n\t\"net/http\"\n)\n\ntype auditOOther struct{}\n\nfunc (o *auditOOther) handleArticle(w http.ResponseWriter, r *http.Request) {\n\tvar req struct{}\n\t_ = json.NewDecoder(r.Body).Decode(&req)\n}\n")
	found := bodyReaders(t, merged)
	unlisted := 0
	for _, name := range found {
		if ownBodyReaders[name] == "" {
			unlisted++
		}
	}
	if unlisted == 0 && len(found) == len(ownBodyReaders) {
		t.Errorf("INVARIANT 1 (test side): a second handleArticle (of another type) that reads its body passes the checks of TestEveryToolAnswersWhatItCannotReadInTheReadersWords: found %v", found)
	}
}
