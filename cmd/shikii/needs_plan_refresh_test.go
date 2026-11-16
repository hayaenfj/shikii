package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	shikii "github.com/hayaenfj/shikii"
)

const calibrationBlock = "calibration:\n  - model: fixture\n    spec_hash: 0123\n"

func builtInNeedsPlan(t *testing.T) []byte {
	t.Helper()
	b, err := shikii.AgentSpecsFS.ReadFile("integrations/claude-code/specs/needs-plan.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A copy of an earlier built-in text that the person did not edit (only the calibration block shikii wrote differs) is rewritten to the current text.
func TestSeedRefreshesUnchangedLegacyCopy(t *testing.T) {
	for _, legacy := range []string{"legacy/needs-plan-0.34.0.yaml", "legacy/needs-plan-0.34.1-dev.yaml"} {
		t.Run(legacy, func(t *testing.T) {
			old, err := os.ReadFile(legacy)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			copyPath := filepath.Join(dir, "needs-plan.yaml")
			if err := os.WriteFile(copyPath, append(old, calibrationBlock...), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := seedAgentSpecs(dir); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(copyPath)
			if string(got) != string(builtInNeedsPlan(t)) {
				t.Fatalf("an unchanged %s copy must be refreshed to the built-in text (got %d bytes, want %d)", legacy, len(got), len(builtInNeedsPlan(t)))
			}
		})
	}
}

// Control: a copy the person edited is kept, even though its version is old.
func TestSeedKeepsEditedCopy(t *testing.T) {
	old, err := os.ReadFile("legacy/needs-plan-0.34.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(old), "Task details delegated to the agent", "Task details the person delegated", 1)
	edited = strings.Replace(edited, "Current request", "Current ask", 1)
	if edited == string(old) {
		t.Fatal("fixture: the edit did not apply")
	}
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	if err := os.WriteFile(copyPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(copyPath)
	if string(got) != edited {
		t.Fatal("an edited copy must be kept as the person wrote it")
	}
}
