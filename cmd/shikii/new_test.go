package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

// A spec can be made from flags alone, and Load and Validate pass on it as is.
func TestNewWritesAValidSpecFromFlags(t *testing.T) {
	dir := t.TempDir()
	out := capture(t, func() error {
		return cmdNew([]string{"--dir", dir, "--type", "choice",
			"--question", "What does the customer want?",
			"--option", "refund=Money back for an order",
			"--option", "status=Where is my order",
			"--input", "message", "--pii", "true", "--send-to", "local",
			"--act", "0.8", "--ask", "0.55", "--min-n", "40",
			"--title-ja", "問い合わせの種類", "inquiry-kind"})
	})
	path := filepath.Join(dir, "inquiry-kind.yaml")
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatalf("generated spec must load: %v\n%s", err, out)
	}
	if sp.ID != "inquiry-kind" || sp.Type != spec.Choice || len(sp.Options) != 2 || sp.Options[0].ID != "refund" ||
		sp.Thresholds.Act != 0.8 || sp.Thresholds.Ask != 0.55 || sp.Eval.MinN != 40 || !sp.PIISensitive() ||
		sp.State.Required[0] != "message" || !strings.Contains(sp.State.Template, "{{ message }}") || sp.Display["ja"].Title != "問い合わせの種類" {
		t.Fatalf("spec content: %+v", sp)
	}
	if !strings.Contains(out, "shikii decide "+path) || !strings.Contains(out, "-i message=") {
		t.Fatalf("new must tell how to try the spec:\n%s", out)
	}
	// The second run does not overwrite
	if err := cmdNew([]string{"--dir", dir, "--type", "noul", "--question", "x", "--input", "a", "--pii", "false", "inquiry-kind"}); err == nil {
		t.Fatal("must refuse to overwrite an existing spec without --force")
	}
}

// What is missing is asked on stdin (you get a spec just by answering questions).
func TestNewAsksForWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	old := stdin
	stdin = strings.NewReader("Is this report a duplicate of an open bug?\nreport\nyes\n")
	defer func() { stdin = old }()
	capture(t, func() error {
		return cmdNew([]string{"--dir", dir, "--type", "noul", "--send-to", "local", "dup-bug"})
	})
	sp, err := spec.Load(filepath.Join(dir, "dup-bug.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Type != spec.Noul || !strings.HasPrefix(sp.Question, "Is this report") || sp.State.Required[0] != "report" || !sp.PIISensitive() {
		t.Fatalf("answers must land in the spec: %+v", sp)
	}
	// When stdin runs out, it fails saying what is missing (it never silently uses a default).
	stdin = strings.NewReader("")
	err = cmdNew([]string{"--dir", dir, "--type", "noul", "--send-to", "local", "another"})
	if err == nil || !strings.Contains(err.Error(), "question") {
		t.Fatalf("must fail naming the missing piece, got %v", err)
	}
}

// Start from a sample: --from copies a spec from examples/ and changes only the id and what is missing.
func TestNewFromTemplate(t *testing.T) {
	dir := t.TempDir()
	tpl := writeSpec(t, dir, "false") // id: intent
	outDir := filepath.Join(dir, "out")
	capture(t, func() error {
		return cmdNew([]string{"--dir", outDir, "--from", tpl, "--question", "Changed question?", "my-intent"})
	})
	sp, err := spec.Load(filepath.Join(outDir, "my-intent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if sp.ID != "my-intent" || sp.Question != "Changed question?" || len(sp.Options) != 3 || len(sp.Calibration) != 0 {
		t.Fatalf("template copy: %+v", sp)
	}
	if _, err := os.Stat(tpl); err != nil {
		t.Fatal("template must be untouched")
	}
}

// The minimum per type is rejected already at new (choice needs at least 2 options, score at least 2 levels).
func TestNewRejectsIncompleteSpecs(t *testing.T) {
	dir := t.TempDir()
	if err := cmdNew([]string{"--dir", dir, "--type", "choice", "--question", "q", "--input", "m", "--pii", "false", "--option", "only=one", "one"}); err == nil {
		t.Fatal("choice with one option must be rejected")
	}
	if err := cmdNew([]string{"--dir", dir, "--type", "score", "--question", "q", "--input", "m", "--pii", "false", "--level", "only", "lv"}); err == nil {
		t.Fatal("score with one level must be rejected")
	}
	if err := cmdNew([]string{"--dir", dir, "--type", "noul", "--question", "q", "--input", "m", "--pii", "false", "--act", "0.5", "--ask", "0.7", "th"}); err == nil {
		t.Fatal("ask above act must be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "one.yaml")); err == nil {
		t.Fatal("a rejected spec must not be written")
	}
}
