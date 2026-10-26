package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.4 audit: when no line fits (every truth on one side, or a higher probability goes with "no"), --force must not write an empty
// calibration block (method "") over the spec; it is refused like any other refusal and the spec stays as it was.
func TestForceDoesNotWriteAnEmptyLineWhenNoneFits(t *testing.T) {
	for _, name := range []string{"one-sided", "reversed"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeNoulSpec(t, dir) // min_n 4
			before, _ := os.ReadFile(path)
			rows := []map[string]any{}
			for i := 0; i < 8; i++ {
				truth, raw := "no", 0.9
				if i%2 == 0 {
					truth, raw = "yes", 0.1
				}
				if name == "one-sided" {
					truth, raw = "yes", 0.2+0.05*float64(i)
				}
				rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
			}
			file := filepath.Join(dir, "labels.json")
			data, _ := json.Marshal(rows)
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			err := runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true, force: true})
			var refused *calibrationRefusal
			if !errors.As(err, &refused) {
				t.Fatalf("--force with no fitted line must be refused, got %v\n%s", err, report.String())
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Fatalf("--force wrote a block although no line fits:\n%s", after)
			}
		})
	}
}

// 0.9.4 audit: a noul label file whose truth is a string other than yes / no (e.g. "true", "Yes") is a broken file, not "the answers cannot be told apart".
// Before, such rows counted as "no", so a file of "true" strings became a one-sided refusal.
func TestNoulLabelFileRejectsTruthOtherThanYesNo(t *testing.T) {
	dir := t.TempDir()
	path := writeNoulSpec(t, dir)
	rows := []map[string]any{}
	for i := 0; i < 6; i++ {
		rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": "true", "raw": 0.8})
	}
	file := filepath.Join(dir, "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	err := runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true})
	var refused *calibrationRefusal
	if err == nil || errors.As(err, &refused) || !strings.Contains(err.Error(), "yes or no") {
		t.Fatalf("a noul truth of \"true\" must be an error that names yes / no, got %T %v\n%s", err, err, report.String())
	}
}

// writeNamedNoulSpec writes a noul spec with the given id (min_n 4) into dir.
func writeNamedNoulSpec(t *testing.T, dir, id string) string {
	t.Helper()
	path := filepath.Join(dir, id+".yaml")
	src := "id: " + id + "\nversion: 1\ntype: noul\nquestion: Same case?\nstate:\n  template: \"{{ a }}\"\n  required: [a]\npolicy:\n  send_to: [local]\n  pii: false\nthresholds:\n  act: 0.70\n  ask: 0.50\neval:\n  min_n: 4\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 0.9.4 audit: the page's quality note says "the AI will try other wordings". Only the hook's questions get other wordings tried
// (variantsContext / hook variants submit accept hookSpecs only), so the result says whether that is so, and a one-sided set of truths
// is told as one kind of answer, not as "this wording cannot tell the answers apart".
func TestServeRefusalSaysWhetherWordingsAreTriedAndOneAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, id, want  string
		oneSided, tries bool
	}{
		{"own question, reversed", "own-check", "quality", false, false},
		{"hook question, reversed", "needs-plan", "quality", false, true},
		{"hook question, one-sided", "needs-plan", "one_answer", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			specsDir := filepath.Join(dir, "decisions")
			if err := os.MkdirAll(specsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeNamedNoulSpec(t, specsDir, tc.id)
			table := map[string]float64{}
			for i := 0; i < 4; i++ {
				table[fmt.Sprint(i)] = 0.9
				if i%2 == 0 {
					table[fmt.Sprint(i)] = 0.1
				}
			}
			model := fakeNoulByMessage(t, "m", table)
			var ef endpointFlags
			ef.model, ef.local = "m", model.URL
			srv := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true}))
			t.Cleanup(srv.Close)
			for i := 0; i < 4; i++ {
				_, d := postJSON(t, srv.URL+"/v1/decide", map[string]any{"spec": tc.id, "inputs": map[string]string{"a": fmt.Sprint(i)}})
				truth := "no"
				if i%2 == 0 || tc.oneSided {
					truth = "yes"
				}
				if code, l := postJSON(t, srv.URL+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": truth}); code != 200 {
					t.Fatalf("label: %d %v", code, l)
				}
			}
			var status struct {
				Specs []map[string]any `json:"specs"`
			}
			getJSON(t, srv.URL+"/v1/status", &status)
			result, _ := status.Specs[0]["calibration_result"].(map[string]any)
			if result["reason"] != tc.want || result["tries_wordings"] != tc.tries {
				t.Fatalf("want reason %s, tries_wordings %v: %v", tc.want, tc.tries, result)
			}
		})
	}
}
