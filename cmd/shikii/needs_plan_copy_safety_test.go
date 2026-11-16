package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const needsPlanTestCalibration = "calibration:\n  - model: fixture\n    spec_hash: 0123\n"

func legacyNeedsPlanText(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("legacy/needs-plan-0.34.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Control: an earlier built-in copy with a calibration block is refreshed, and a second seeding changes nothing.
func TestSeedingTwiceChangesNothingAfterRefresh(t *testing.T) {
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	if err := os.WriteFile(copyPath, append(legacyNeedsPlanText(t), needsPlanTestCalibration...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(copyPath)
	n, err := seedAgentSpecs(dir)
	if err != nil {
		t.Fatalf("second seeding failed: %v", err)
	}
	second, _ := os.ReadFile(copyPath)
	if n != 0 || string(first) != string(second) {
		t.Fatalf("second seeding must change nothing: copied=%d same=%v", n, string(first) == string(second))
	}
}

// Control: an unparseable copy is the person's and is kept as written.
func TestUnparseableNeedsPlanCopyIsKept(t *testing.T) {
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	broken := "id: needs-plan\nquestion: [unclosed\n"
	if err := os.WriteFile(copyPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatalf("an unparseable copy must not stop seeding: %v", err)
	}
	if got, _ := os.ReadFile(copyPath); string(got) != broken {
		t.Fatal("an unparseable copy must be kept as written")
	}
}

// A refreshed copy keeps the permission bits of the copy the person has (0644 for a seeded file).
func TestRefreshedCopyKeepsPermissionBits(t *testing.T) {
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	if err := os.WriteFile(copyPath, legacyNeedsPlanText(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(copyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("a refreshed copy changed its permission bits: got %o, want 644", info.Mode().Perm())
	}
}

// An unreadable copy the person owns is kept, and seeding goes on: every hook event of the project seeds first.
func TestUnreadableNeedsPlanCopyDoesNotStopSeeding(t *testing.T) {
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	if err := os.WriteFile(copyPath, legacyNeedsPlanText(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(copyPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(copyPath, 0o644) })
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatalf("an unreadable copy stops seeding (and every hook call that seeds): %v", err)
	}
}

// A decisions folder that cannot take the temporary file keeps the old copy, and seeding goes on.
func TestReadOnlyDecisionsFolderDoesNotStopSeeding(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil { // every other built-in file is present, so only the refresh needs to write
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	if err := os.WriteFile(copyPath, legacyNeedsPlanText(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatalf("a read-only decisions folder stops seeding: %v", err)
	}
}

// A comment the person added to an otherwise unchanged copy is theirs: the refresh must keep it.
func TestPersonCommentSurvivesRefresh(t *testing.T) {
	dir := t.TempDir()
	copyPath := filepath.Join(dir, "needs-plan.yaml")
	noted := "# my note: keep this wording while I test\n" + string(legacyNeedsPlanText(t))
	if err := os.WriteFile(copyPath, []byte(noted), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(copyPath); !strings.Contains(string(got), "my note") {
		t.Fatal("a comment the person wrote in the copy was overwritten by the refresh")
	}
}
