package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Even with enough cases, saving stops on separation or precision. Check the reason and the fact that nothing was saved together.
func TestCalibrationRefusalExplainsActualReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		n, min int
		choice bool
		want   string
		// reversed: a higher probability goes with "no" (the fitted slope is not positive). It is the same "cannot tell the answers apart" as an overlap,
		// so the page shows it as a quality refusal, not as a failure with the English fitting message (0.9.3: a person saw it after answering on the page)
		reversed bool
		oneSided bool // every truth is "yes": also a quality refusal (the page says only one kind of answer), not the English "need both" failure
	}{
		{"overlap", 50, 50, false, "predictions", false, false},
		{"insufficient", 2, 50, false, "short of", false, false},
		{"precision", 2, 2, true, "predictions", false, false},
		{"reversed", 50, 50, false, "predictions", true, false},
		{"one-sided", 50, 50, false, "predictions", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeNoulSpec(t, dir)
			if tc.choice {
				path = writeSpec(t, dir, "false")
			}
			sp := mustLoad(t, path)
			sp.Eval.MinN = tc.min
			specData, err := yaml.Marshal(sp)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, specData, 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			rows := []map[string]any{}
			for i := 0; i < tc.n; i++ {
				truth, raw := "no", any(0.2)
				inputs := map[string]string{"a": fmt.Sprint(i)}
				if i%2 == 0 {
					truth, raw = "yes", 0.7
				}
				if tc.oneSided {
					truth = "yes"
				}
				if tc.reversed {
					raw = 0.9
					if truth == "yes" {
						raw = 0.1
					}
				}
				if tc.n >= tc.min && !tc.choice && i == 0 {
					raw = 0.4
				}
				if tc.n >= tc.min && !tc.choice && i == 1 {
					raw = 0.6
				}
				if tc.choice {
					inputs = map[string]string{"message": fmt.Sprint(i)}
					truth = "question"
					if i%2 == 0 {
						truth = "change_request"
					}
					raw = map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
				}
				rows = append(rows, map[string]any{"inputs": inputs, "truth": truth, "raw": raw})
			}
			file := filepath.Join(dir, "labels.json")
			data, _ := json.Marshal(rows)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			err = runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wrong refusal: %v\n%s", err, report.String())
			}
			var refused *calibrationRefusal
			if !errors.As(err, &refused) || (tc.n >= tc.min && refused.reason() != "quality") {
				t.Fatalf("a refusal must be a calibrationRefusal (quality when there are enough truths), not a failure: %T %v", err, err)
			}
			if tc.n >= tc.min && (strings.Contains(err.Error(), "fewer human truths") || strings.Contains(err.Error(), "short of the")) {
				t.Fatalf("quality failure must not claim insufficient truths: %v", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("refused calibration changed spec")
			}
		})
	}
}

func TestAutoCalibrationReportsQualityWithoutLosingAnswers(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			url, path, dir := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
			for i := 0; i < 2; i++ {
				_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": fmt.Sprint(i)}})
				truth := "change_request"
				if i == 1 && !success {
					truth = "question"
				}
				code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": truth})
				if code != 200 || (i == 1 && l["calibrated"] != success) {
					t.Fatalf("label: %v", l)
				}
			}
			var status struct {
				Specs []map[string]any `json:"specs"`
			}
			getJSON(t, url+"/v1/status", &status)
			s := status.Specs[0]
			result, ok := s["calibration_result"].(map[string]any)
			want := "quality"
			if success {
				want = "written"
			}
			if !ok || result["reason"] != want || result["human_truths"] != float64(2) || result["min_n"] != float64(2) {
				t.Fatalf("structured calibration result: %v", s)
			}
			if result["hash"] != s["hash"] || result["model"] != "m" || result["at"] == "" {
				t.Fatalf("calibration identity: %v", result)
			}
			if len(readLabels(t, dir, "intent")) != 2 || (len(mustLoad(t, path).Calibration) > 0) != success {
				t.Fatal("answers or calibration changed incorrectly")
			}
		})
	}
}
