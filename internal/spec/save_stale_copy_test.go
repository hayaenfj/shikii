package spec

import (
	"os"
	"path/filepath"
	"testing"
)

const staleCopySpec = "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"

// A copy loaded while the spec had no candidates (calibrate --write, which only adds a line) must not erase
// the candidates another process saved in the meantime. The order is fixed: load, other save, our save.
func TestSaveKeepsVariantsAddedAfterACopyWithoutVariantsWasLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.yaml")
	if err := os.WriteFile(path, []byte(staleCopySpec), 0o644); err != nil {
		t.Fatal(err)
	}
	calibrating, err := Load(path) // calibrate --write: loaded before the candidates exist
	if err != nil {
		t.Fatal(err)
	}
	proposing, err := Load(path) // hook variants / the page / tune --propose
	if err != nil {
		t.Fatal(err)
	}
	proposing.Variants = append(proposing.Variants, Variant{Name: "a-from-agent", Question: "Does it ask for something?"})
	if err := proposing.Save(); err != nil {
		t.Fatal(err)
	}

	calibrating.PutCalibration(Calibration{Model: "m", SpecHash: calibrating.Hash(), Method: "platt", Temperature: 1, N: 2, FittedAt: "2026-11-10"})
	if err := calibrating.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CalibrationFor("m") == nil {
		t.Fatalf("the calibration line must be written: %+v", got.Calibration)
	}
	if len(got.Variants) != 1 || got.Variants[0].Name != "a-from-agent" {
		t.Fatalf("a save that never touched variants erased the candidate another process added: %+v", got.Variants)
	}
}

// Guard for the fix: a copy that loaded candidates and emptied them still clears the key (a write that clears
// all candidates), also when another process added one in the meantime. Only a copy that never had any leaves
// the file's candidates alone.
func TestSaveStillClearsVariantsWhenThisCopyLoadedSomeAndEmptiedThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.yaml")
	if err := os.WriteFile(path, []byte(staleCopySpec), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Variants = []Variant{{Name: "one", Question: "One?"}, {Name: "two", Question: "Two?"}}
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	mine, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	other.Variants = append(other.Variants, Variant{Name: "added-meanwhile", Question: "Added?"})
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	mine.Variants = nil
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Variants) != 0 {
		t.Fatalf("a copy that had candidates and emptied them must clear the key: %+v", got.Variants)
	}
}
