package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

func TestGenericAgentSpecsKeepCodingCalibrationsSeparate(t *testing.T) {
	// Hashes of the ready-made questions of 0.4.4. Old lines are not carried over to the generalized questions.
	previous := map[string]string{
		"prompt-kind": "68ed82631e63f2a2", "needs-plan": "66d4d3ef366b8b96",
		"changes-in-scope": "9381415d9f1d3696", "done-as-asked": "2bba11b5ee687b57",
	}
	dir := t.TempDir()
	if n, err := seedAgentSpecs(dir); err != nil || n != len(hookSpecs)+len(wikiSpecs) {
		t.Fatal("prepare the ready-made questions (4 for the hook and 2 for the wiki)", n, err)
	}
	for id, oldHash := range previous {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(dir, id+".yaml")
			sp := mustLoad(t, path)
			sp.Calibration = []spec.Calibration{{Model: "fixture", SpecHash: oldHash, Method: "identity", N: 50}}
			if sp.Hash() == oldHash || sp.CalibrationFor("fixture") != nil || sp.LiveHash(oldHash) {
				t.Fatal("separate the old coding-oriented questions and calibration from the new questions")
			}
			if id == "needs-plan" && !needsPlanningContext(sp) {
				t.Fatal("keep the contract of using agreements and open items with the original text")
			}
			// Questions and lines in use are protected from re-initialization and updates.
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("\n# 利用者の質問を保持\n")...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := seedAgentSpecs(dir); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatal("do not overwrite existing questions with the generic version", err)
			}
		})
	}
}
