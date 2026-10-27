package store

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func withoutHardLinks(t *testing.T) {
	t.Helper()
	old := Link
	Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { Link = old })
}

// On a disk without hard links PlaceOnce wrote the file in place, so a stop in the middle left a part of it at the path, which every later
// init kept (0.9.9 audit). The whole file is now written to a temp file first and renamed into place: a stopped write leaves nothing there.
func TestPatch0910StoppedWriteWithoutHardLinksLeavesNothing(t *testing.T) {
	withoutHardLinks(t)
	old := WriteTemp
	t.Cleanup(func() { WriteTemp = old })
	WriteTemp = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, syscall.EINTR
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "needs-plan.yaml")
	if _, err := PlaceOnce(path, []byte("name: needs-plan\n"), 0o644); err == nil {
		t.Fatal("a stopped write is an error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a stopped write leaves nothing in the folder: %v", entries)
	}
	WriteTemp = old
	if placed, err := PlaceOnce(path, []byte("name: needs-plan\n"), 0o644); err != nil || !placed {
		t.Fatalf("the next write places it: %v %v", placed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "name: needs-plan\n" {
		t.Fatalf("got %q", b)
	}
}

// Two writers at once on a disk without hard links (two inits making the store id): only one places its file, and the other sees it.
func TestPatch0910TwoWritersWithoutHardLinksPlaceOne(t *testing.T) {
	withoutHardLinks(t)
	for i := 0; i < 200; i++ {
		path := filepath.Join(t.TempDir(), "store-id")
		var start sync.WaitGroup
		start.Add(2)
		Link = func(string, string) error {
			start.Done()
			start.Wait() // both writers leave Link together
			return &os.LinkError{Op: "link", Err: syscall.ENOTSUP}
		}
		placed := make([]bool, 2)
		var done sync.WaitGroup
		for w := range placed {
			done.Add(1)
			go func() {
				defer done.Done()
				placed[w], _ = PlaceOnce(path, []byte{'a' + byte(w), '\n'}, 0o600)
			}()
		}
		done.Wait()
		b, _ := os.ReadFile(path)
		if placed[0] == placed[1] || placed[0] && string(b) != "a\n" || placed[1] && string(b) != "b\n" {
			t.Fatalf("round %d: placed %v, file %q", i, placed, b)
		}
	}
}

// 0.9.10 confirmation audit a-2: a file named <path>.placing that shikii did not make is never removed.
func TestPatch0910OthersPlacingFileStays(t *testing.T) {
	withoutHardLinks(t)
	path := filepath.Join(t.TempDir(), "store-id")
	if err := os.WriteFile(path+".placing", []byte("notes of another tool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if placed, err := PlaceOnce(path, []byte("id\n"), 0o600); err != nil || !placed {
		t.Fatal(placed, err)
	}
	if b, err := os.ReadFile(path + ".placing"); err != nil || string(b) != "notes of another tool\n" {
		t.Fatalf("a .placing file shikii did not make stays: %q %v", b, err)
	}
}

// 0.9.10 confirmation audit s-1: Linux's exFAT and FAT drivers refuse chmod (EPERM), and PlaceOnce stopped on its temp file's mode there,
// so init never worked on such a disk. Modes mean nothing there: a refused chmod is let pass.
func TestPatch0910RefusedChmodDoesNotStop(t *testing.T) {
	withoutHardLinks(t)
	old := ChmodTemp
	t.Cleanup(func() { ChmodTemp = old })
	ChmodTemp = func(*os.File, os.FileMode) error { return &os.PathError{Op: "chmod", Err: syscall.EPERM} }
	path := filepath.Join(t.TempDir(), "store-id")
	if placed, err := PlaceOnce(path, []byte("id\n"), 0o600); err != nil || !placed {
		t.Fatalf("a refused chmod does not stop it: %v %v", placed, err)
	}
}
