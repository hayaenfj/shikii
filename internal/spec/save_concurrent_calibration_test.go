package spec

import (
	"os"
	"path/filepath"
	"testing"
)

const twoRungSpec = "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\n  context: \"{{ message }}\\nBefore: {{ previous }}\"\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"

func writeSpecFile(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "intent.yaml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLoadSpec(t *testing.T, path string) *Spec {
	t.Helper()
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func line(model, hash string, temperature float64) Calibration {
	return Calibration{Model: model, SpecHash: hash, Method: "platt", Temperature: temperature, N: 2, FittedAt: "2026-11-10"}
}

// Two calibrations that overlap (the CLI for one model, serve's automatic calibration for another) each load the
// spec, fit, and save. The order is fixed: both load, the first saves its line, the second saves its line. The
// first line must still be in the file.
func TestSaveKeepsTheLineAnotherCalibrationWroteForAnotherModel(t *testing.T) {
	path := writeSpecFile(t, staleCopySpec)
	first, second := mustLoadSpec(t, path), mustLoadSpec(t, path)

	first.PutCalibration(line("model-a", first.Hash(), 1))
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	second.PutCalibration(line("model-b", second.Hash(), 2))
	if err := second.Save(); err != nil {
		t.Fatal(err)
	}

	got := mustLoadSpec(t, path)
	if got.CalibrationFor("model-b") == nil {
		t.Fatalf("the second calibration's own line must be written: %+v", got.Calibration)
	}
	if got.CalibrationFor("model-a") == nil {
		t.Fatalf("the second save erased the line the first calibration wrote for another model: %+v", got.Calibration)
	}
}

// The same with the two rungs of one model: serve calibrates only the rung that has enough truths, so the minimal
// rung and the context rung can be fitted by two runs that overlap.
func TestSaveKeepsTheLineAnotherCalibrationWroteForTheOtherRung(t *testing.T) {
	path := writeSpecFile(t, twoRungSpec)
	minimal, context := mustLoadSpec(t, path), mustLoadSpec(t, path)

	minimal.PutCalibration(line("m", minimal.Hash(), 1))
	if err := minimal.Save(); err != nil {
		t.Fatal(err)
	}
	context.PutCalibration(line("m", context.ContextHash(), 2))
	if err := context.Save(); err != nil {
		t.Fatal(err)
	}

	got := mustLoadSpec(t, path)
	if c := got.ContextSpec().CalibrationFor("m"); c == nil || c.Temperature != 2 {
		t.Fatalf("the context rung's line must be written: %+v", got.Calibration)
	}
	if c := got.CalibrationFor("m"); c == nil || c.Temperature != 1 {
		t.Fatalf("saving the context rung's line erased the minimal rung's line written meanwhile: %+v", got.Calibration)
	}
}

// A copy that still holds an old line it never touched must not put it back over the newer line another
// calibration wrote for the same model and wording.
func TestSaveDoesNotPutBackAStaleLineItNeverTouched(t *testing.T) {
	path := writeSpecFile(t, staleCopySpec)
	seed := mustLoadSpec(t, path)
	seed.PutCalibration(line("model-a", seed.Hash(), 1))
	if err := seed.Save(); err != nil {
		t.Fatal(err)
	}

	stale, fresh := mustLoadSpec(t, path), mustLoadSpec(t, path)
	fresh.PutCalibration(line("model-a", fresh.Hash(), 3)) // a newer fit for model-a
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	stale.PutCalibration(line("model-b", stale.Hash(), 2)) // touches only model-b
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}

	got := mustLoadSpec(t, path)
	if c := got.CalibrationFor("model-a"); c == nil || c.Temperature != 3 {
		t.Fatalf("the stale copy put back model-a's old line over the newer one: %+v", got.Calibration)
	}
	if got.CalibrationFor("model-b") == nil {
		t.Fatalf("model-b's line must be written: %+v", got.Calibration)
	}
}

// Guard: what a single writer does stays the same. A line this copy replaced is replaced (not duplicated), a
// line this copy dropped stays dropped, and a line for a wording that is no longer live is replaced by
// PutCalibration. Merging with the file must not bring any of them back.
func TestSaveStillReplacesAndDropsTheLinesThisCopyChanged(t *testing.T) {
	path := writeSpecFile(t, staleCopySpec)
	s := mustLoadSpec(t, path)
	s.PutCalibration(line("model-a", s.Hash(), 1))
	s.PutCalibration(line("model-b", s.Hash(), 2))
	s.PutCalibration(line("model-c", "0000000000000000", 9)) // a wording that is not live
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	mine := mustLoadSpec(t, path)
	mine.PutCalibration(line("model-a", mine.Hash(), 5)) // replaced
	kept := mine.Calibration[:0:0]
	for _, c := range mine.Calibration {
		if c.Model != "model-b" { // dropped by this copy
			kept = append(kept, c)
		}
	}
	mine.Calibration = kept
	mine.PutCalibration(line("model-c", mine.Hash(), 6)) // replaces model-c's line for the dead wording
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}

	got := mustLoadSpec(t, path)
	byModel := map[string][]Calibration{}
	for _, c := range got.Calibration {
		byModel[c.Model] = append(byModel[c.Model], c)
	}
	if l := byModel["model-a"]; len(l) != 1 || l[0].Temperature != 5 {
		t.Fatalf("model-a's line must be replaced, not duplicated: %+v", got.Calibration)
	}
	if len(byModel["model-b"]) != 0 {
		t.Fatalf("a line this copy dropped must stay dropped: %+v", got.Calibration)
	}
	if l := byModel["model-c"]; len(l) != 1 || l[0].Temperature != 6 {
		t.Fatalf("model-c's line for the dead wording must be replaced: %+v", got.Calibration)
	}
	if len(got.Calibration) != 2 {
		t.Fatalf("want exactly model-a and model-c: %+v", got.Calibration)
	}

	// The same when another calibration wrote a line in between: what this copy loaded and dropped stays dropped.
	dropping, other := mustLoadSpec(t, path), mustLoadSpec(t, path)
	other.PutCalibration(line("model-d", other.Hash(), 4))
	if err := other.Save(); err != nil {
		t.Fatal(err)
	}
	dropping.Calibration = nil
	if err := dropping.Save(); err != nil {
		t.Fatal(err)
	}
	for _, c := range mustLoadSpec(t, path).Calibration {
		if c.Model == "model-a" || c.Model == "model-c" {
			t.Fatalf("a line this copy loaded and dropped came back from the file: %+v", c)
		}
	}
}
