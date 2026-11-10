package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const personSpec = "id: intent\nversion: 1\ntype: noul\nquestion: Is it a request?\nstate:\n  template: \"{{ message }}\"\n  required: [message]\npolicy:\n  send_to: [local]\n  pii: true\nthresholds:\n  act: 0.75\n  ask: 0.55\neval:\n  min_n: 2\n"

func saveALine(t *testing.T, path string) {
	t.Helper()
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.PutCalibration(Calibration{Model: "m", SpecHash: s.Hash(), Method: "platt", Temperature: 1, N: 2, FittedAt: "2026-11-10"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
}

// The spec is the person's file. A mode the person set (private: 0600, or shared with a group: 0664) must be the
// mode after calibrate --write saves a line into it.
func TestSaveKeepsTheModeThePersonSet(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640, 0o664, 0o644} {
		path := filepath.Join(t.TempDir(), "intent.yaml")
		if err := os.WriteFile(path, []byte(personSpec), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil { // WriteFile's mode goes through the umask; Chmod does not
			t.Fatal(err)
		}
		saveALine(t, path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != mode {
			t.Errorf("the person's spec was %04o and is %04o after Save", mode, got)
		}
		if b, _ := os.ReadFile(path); !strings.Contains(string(b), "calibration:") {
			t.Errorf("mode %04o: the line was not written: %s", mode, b)
		}
	}
}

// A person who keeps the spec somewhere else (a folder under version control) and links it into the decisions
// folder must still have a link after Save, and the line must be in the file the link leads to.
func TestSaveWritesThroughALinkAndKeepsIt(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept")
	decisions := filepath.Join(dir, "decisions")
	for _, d := range []string{kept, decisions} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(kept, "intent.yaml")
	if err := os.WriteFile(real, []byte(personSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(decisions, "intent.yaml")
	if err := os.Symlink(filepath.Join("..", "kept", "intent.yaml"), link); err != nil { // a relative link, as ln -s writes it
		t.Fatal(err)
	}

	saveALine(t, link)

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Save replaced the person's link with a file of its own (mode %v)", info.Mode())
	}
	if target, _ := os.Readlink(link); target != filepath.Join("..", "kept", "intent.yaml") {
		t.Fatalf("the link now points to %q", target)
	}
	b, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "calibration:") {
		t.Fatalf("the file the link leads to did not get the line:\n%s", b)
	}
	if realInfo, _ := os.Stat(real); realInfo.Mode().Perm() != 0o600 {
		t.Fatalf("the linked file was 0600 and is %04o after Save", realInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(decisions)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the decisions folder must hold only the link: %v", entries)
	}
	if got, err := Load(link); err != nil || got.CalibrationFor("m") == nil {
		t.Fatalf("the spec read through the link must have the line: %v", err)
	}
}
