package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// A row of a labeled file follows the weight rule of every other truth: 0 or absent is a firm 1, a value in (0, 1] is kept,
// and anything else is an error naming the row. A negative weight used to become a firm truth and 5 went into the fit.
func TestLabeledFileRefusesAWeightOutsideZeroToOne(t *testing.T) {
	dir := t.TempDir()
	sp, err := spec.Load(writeSpec(t, dir, "false"))
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: filepath.Join(dir, "store")}
	rows := func(weight string) string {
		path := filepath.Join(dir, "rows.json")
		row := `[{"inputs":{"message":"fix the typo"},"truth":"change_request","raw":{"change_request":0.8,"question":0.1,"other":0.1}` + weight + `}]`
		if err := os.WriteFile(path, []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, bad := range []string{`,"weight":-0.5`, `,"weight":5`, `,"weight":1.0001`} {
		labels, _, err := labelsFromFile(sp, rows(bad), endpointFlags{}, st)
		if err == nil || !strings.Contains(err.Error(), "row 0: weight must be in (0, 1]") {
			t.Errorf("a row with %s must be refused with its row number, got %v %+v", bad, err, labels)
		}
	}
	for given, want := range map[string]float64{``: 1, `,"weight":0`: 1, `,"weight":1`: 1, `,"weight":0.25`: 0.25} {
		labels, _, err := labelsFromFile(sp, rows(given), endpointFlags{}, st)
		if err != nil || len(labels) != 1 || labels[0].Weight != want {
			t.Errorf("a row with %q is a truth of weight %v: %+v %v", given, want, labels, err)
		}
	}
}

// `shikii check` (the rows of eval.labeled that bring their own probabilities) and the truth bank of tune, bias and the wiki
// questions read the same file: they refuse the same rows. (check turned a negative weight into 1 and took 5 as it was; the
// truth bank turned all of them into 1.)
func TestCheckAndTheTruthBankRefuseAFileRowWeightOutsideZeroToOne(t *testing.T) {
	dir := t.TempDir()
	sp, err := spec.Load(writeSpec(t, dir, "false"))
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: filepath.Join(dir, "store")}
	rows := func(weight string) string {
		path := filepath.Join(dir, "rows.json")
		row := `[{"inputs":{"message":"fix the typo"},"truth":"change_request","raw":{"change_request":0.8,"question":0.1,"other":0.1}` + weight + `}]`
		if err := os.WriteFile(path, []byte(row), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, bad := range []string{`,"weight":-0.5`, `,"weight":5`, `,"weight":1.0001`} {
		sp.Eval.Labeled = rows(bad)
		if labels, err := collectLabels(sp, st, "m", sp.Eval.Labeled); err == nil || !strings.Contains(err.Error(), "row 0: weight must be in (0, 1]") {
			t.Errorf("check: a row with %s must be refused with its row number, got %v %+v", bad, err, labels)
		}
		if bank, _, err := truthBank(sp, st, rows(bad)); err == nil || !strings.Contains(err.Error(), "row 0: weight must be in (0, 1]") {
			t.Errorf("truth bank: a row with %s must be refused with its row number, got %v (%d rows)", bad, err, len(bank))
		}
	}
	for given, want := range map[string]float64{``: 1, `,"weight":0`: 1, `,"weight":0.25`: 0.25} {
		sp.Eval.Labeled = rows(given)
		if labels, err := collectLabels(sp, st, "m", sp.Eval.Labeled); err != nil || len(labels) != 1 || labels[0].Weight != want {
			t.Errorf("check: a row with %q is a truth of weight %v: %+v %v", given, want, labels, err)
		}
		if bank, _, err := truthBank(sp, st, rows(given)); err != nil || len(bank) != 1 || bank[0].weight != want {
			t.Errorf("truth bank: a row with %q is a truth of weight %v: %+v %v", given, want, bank, err)
		}
	}
}

// The three readers refuse the same file: a row one of them does not use (check skips rows without their own probabilities, the
// truth bank rows without inputs) is still read for its weight.
func TestEveryReaderRefusesTheWeightOfARowItDoesNotUse(t *testing.T) {
	dir := t.TempDir()
	sp, err := spec.Load(writeSpec(t, dir, "false"))
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: filepath.Join(dir, "store")}
	write := func(rows string) string {
		path := filepath.Join(dir, "rows.json")
		if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	sp.Eval.Labeled = write(`[{"inputs":{"message":"fix the typo"},"truth":"change_request","weight":5}]`)
	if labels, err := collectLabels(sp, st, "m", sp.Eval.Labeled); err == nil || !strings.Contains(err.Error(), "row 0: weight must be in (0, 1]") {
		t.Errorf("check: a row without raw and with weight 5 must be refused, got %v %+v", err, labels)
	}
	noInputs := write(`[{"truth":"change_request","raw":{"change_request":0.8,"question":0.1,"other":0.1},"weight":-0.5}]`)
	if bank, _, err := truthBank(sp, st, noInputs); err == nil || !strings.Contains(err.Error(), "row 0: weight must be in (0, 1]") {
		t.Errorf("truth bank: a row without inputs and with weight -0.5 must be refused, got %v (%d rows)", err, len(bank))
	}
	// with a weight inside the range, such rows are skipped as before
	sp.Eval.Labeled = write(`[{"inputs":{"message":"fix the typo"},"truth":"change_request","weight":0.5}]`)
	if labels, err := collectLabels(sp, st, "m", sp.Eval.Labeled); err != nil || len(labels) != 0 {
		t.Errorf("check skips a row without raw: %v %+v", err, labels)
	}
	if bank, _, err := truthBank(sp, st, write(`[{"truth":"change_request","raw":{"change_request":0.8,"question":0.1,"other":0.1}}]`)); err != nil || len(bank) != 0 {
		t.Errorf("the truth bank skips a row without inputs: %v (%d rows)", err, len(bank))
	}
}
