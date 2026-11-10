package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// A save holds the folder's lock from reading the file to replacing it. Another save that starts in between waits and then reads
// the replaced file, so both lines are in the file. Without the lock the second save, started after the first one read the
// file, wrote its line and the first save's rename erased it. The order is fixed: the other save starts at the seam between the
// first save's write and its rename, and the first save goes on only once the other one found the lock held.
func TestSaveKeepsALineAnotherSaveWritesBetweenItsReadAndItsRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.yaml")
	if err := os.WriteFile(path, []byte(staleCopySpec), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	other.PutCalibration(Calibration{Model: "model-b", SpecHash: other.Hash(), Method: "platt", Temperature: 2, N: 2, FittedAt: "2026-11-10"})

	chmod, busy := chmodSaved, savesBusy
	defer func() { chmodSaved, savesBusy = chmod, busy }()
	waiting, done, saved := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	told := false
	savesBusy = func() {
		if !told {
			told = true
			close(waiting)
		}
	}
	started := false
	chmodSaved = func(name string, mode os.FileMode) error {
		if !started {
			started = true
			go func() { saved <- other.Save(); close(done) }()
			select {
			case <-waiting: // the other save found this save in progress and waits for it
			case <-done: // it did not wait: the check below then finds its line erased
			}
		}
		return chmod(name, mode)
	}
	first.PutCalibration(Calibration{Model: "model-a", SpecHash: first.Hash(), Method: "platt", Temperature: 1, N: 2, FittedAt: "2026-11-10"})
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}
	if err := <-saved; err != nil {
		t.Fatalf("the save that waited: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CalibrationFor("model-a") == nil || got.CalibrationFor("model-b") == nil {
		b, _ := os.ReadFile(path)
		t.Fatalf("both saves' lines are in the file:\n%s", b)
	}
}

// The lock is the folder's, taken on the folder itself: a save leaves no lock file next to the person's specs.
func TestSaveLeavesNoLockFileInTheFolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	if err := os.WriteFile(path, []byte(staleCopySpec), 0o644); err != nil {
		t.Fatal(err)
	}
	sp, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sp.PutCalibration(Calibration{Model: "model-a", SpecHash: sp.Hash(), Method: "platt", Temperature: 1, N: 2, FittedAt: "2026-11-10"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "intent.yaml" {
		t.Fatalf("the folder holds the spec alone after a save: %v %v", entries, err)
	}
}

// After a save this copy holds what the file holds: the candidates another process added meanwhile are in Variants, so the
// next save of the same copy (and whatever reads the copy) starts from the merged list.
func TestSaveLeavesThisCopyWithTheMergedCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intent.yaml")
	if err := os.WriteFile(path, []byte(staleCopySpec), 0o644); err != nil {
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
	mine.Variants = append(mine.Variants, Variant{Name: "mine", Question: "Mine?"})
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, v := range mine.Variants {
		names = append(names, v.Name)
	}
	if len(names) != 2 || names[0] != "added-meanwhile" || names[1] != "mine" {
		t.Fatalf("this copy holds the merged candidates after its save: %v", names)
	}
	// it had candidates now: saving none clears them (a copy that never had any would leave the file's alone)
	mine.Variants = nil
	if err := mine.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || len(got.Variants) != 0 {
		t.Fatalf("a copy that held candidates after its save clears them by saving none: %+v %v", got.Variants, err)
	}
}

// A save whose rename fails leaves no temporary file next to the person's spec.
func TestSaveThatCannotReplaceTheFileLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "intent.yaml")
	if err := os.WriteFile(path, []byte(staleCopySpec), 0o644); err != nil {
		t.Fatal(err)
	}
	sp, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sp.PutCalibration(Calibration{Model: "model-a", SpecHash: sp.Hash(), Method: "platt", Temperature: 1, N: 2, FittedAt: "2026-11-10"})
	chmod := chmodSaved
	defer func() { chmodSaved = chmod }()
	chmodSaved = func(name string, mode os.FileMode) error { // at the seam before the rename: a folder now stands where the spec was
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o644); err != nil {
			return err
		}
		return chmod(name, mode)
	}
	if err := sp.Save(); err == nil {
		t.Fatal("a save that cannot replace the file must fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "intent.yaml" {
		t.Fatalf("no temporary file is left in the folder: %v %v", entries, err)
	}
}
