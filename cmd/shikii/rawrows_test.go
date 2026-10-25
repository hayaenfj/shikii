package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// calibrate can read choice / score rows too when they carry raw (an object of probabilities per choice). It draws a line without calling a model,
// so a benchmark's LoRA arm (raw produced by another tool) can be lined up on the same scale as A and B. noul's raw stays a single number as before.
func TestCalibrateReadsChoiceRowsWithRawDistributions(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false") // min_n 2
	rows := `[
	 {"inputs":{"message":"fix a"},"raw":{"change_request":0.9,"question":0.08,"other":0.02},"truth":"change_request"},
	 {"inputs":{"message":"fix b"},"raw":{"change_request":0.85,"question":0.1,"other":0.05},"truth":"change_request"},
	 {"inputs":{"message":"why a"},"raw":{"change_request":0.1,"question":0.88,"other":0.02},"truth":"question"},
	 {"inputs":{"message":"why b"},"raw":{"change_request":0.2,"question":0.75,"other":0.05},"truth":"question"}
	]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nowhere to send (no --local): there is raw, so no call is needed
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--model", "lora-bench", "--store", filepath.Join(dir, ".shikii"), "--write"})
	})
	for _, must := range []string{"n=4", "top-1 accuracy=1.000", "wrote calibration"} {
		if !strings.Contains(out, must) {
			t.Fatalf("calibrate must score the given raw without a model; missing %q:\n%s", must, out)
		}
	}
	sp := mustLoad(t, specPath)
	if c := sp.CalibrationFor("lora-bench"); c == nil || c.N != 4 {
		t.Fatalf("the line is keyed by the given model name: %+v", sp.Calibration)
	}
	// If a raw key is not one of the spec's choices, it is an error (not silently 0)
	bad := strings.Replace(rows, `"other":0.02},"truth":"change_request"`, `"nope":0.02},"truth":"change_request"`, 1)
	if err := os.WriteFile(labeledPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--model", "lora-bench", "--store", filepath.Join(dir, ".shikii2")})
	stdout = old
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a raw key that is not an option must be an error, got %v", err)
	}
}
