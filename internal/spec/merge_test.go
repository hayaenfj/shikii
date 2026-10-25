package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Save does not drop candidates (variants) another process added in the meantime: it merges the file's candidates with ours by name.
// Names reject separators and spaces (they go into wiki/runs file names).
func TestSaveMergesVariantsWrittenByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	src := "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := Load(path) // an old copy (e.g. background tune or calibrate --write)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Load(path) // the page's entry: a person adds candidate A
	if err != nil {
		t.Fatal(err)
	}
	fresh.Variants = append(fresh.Variants, Variant{Name: "a-from-page", Question: "Does it ask for something?"})
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	// the old copy adds candidate B and saves → A must not disappear
	stale.Variants = append(stale.Variants, Variant{Name: "b-from-tune", Question: "Is this a request for work?"})
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, v := range got.Variants {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "a-from-page,b-from-tune" {
		t.Fatalf("both candidates must survive, in file order then new ones: %v", names)
	}
	// on the same name ours wins (e.g. rewriting after adoption)
	got.Variants[0].Question = "Does it ask for anything?"
	if err := got.Save(); err != nil {
		t.Fatal(err)
	}
	again, _ := Load(path)
	if again.Variants[0].Question != "Does it ask for anything?" || len(again.Variants) != 2 {
		t.Fatalf("same name must be replaced, not duplicated: %+v", again.Variants)
	}
	// name validation
	for _, bad := range []string{"", "x/../../outside", "with space", "a\nb", strings.Repeat("n", 61)} {
		again.Variants = []Variant{{Name: bad, Question: "q"}}
		if err := again.Validate(); err == nil {
			t.Fatalf("variant name %q must be refused", bad)
		}
	}
}
