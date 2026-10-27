package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

var builtInSpec = []byte("name: needs-plan\nquestion: Should the plan be agreed first?\n")

// A built-in file already there is the person's from then on: shortened, emptied or whole, init and the hook leave it as it is (0.9.10
// audit a-1: a rewrite of a part brought back what the person had cut).
func TestPatch0910BuiltInFileAlreadyThereStays(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	for _, text := range []string{"name: needs-plan\n", "", string(builtInSpec), "name: mine\n"} {
		path := filepath.Join(t.TempDir(), "needs-plan.yaml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		created, err := seedFile(path, builtInSpec)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); string(b) != text || created {
			t.Fatalf("%q became %q (created %v)", text, b, created)
		}
	}
}

// The whole path on a disk without hard links: init stopped while writing a built-in file leaves nothing at its path, so the next init
// places it in full (0.9.9 audit: it left a part, which every later init kept).
func TestPatch0910StoppedSeedWithoutHardLinksIsPlacedNextTime(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	write := store.WriteTemp
	store.WriteTemp = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, syscall.EINTR
	}
	t.Cleanup(func() { store.Link, store.WriteTemp = os.Link, write })
	path := filepath.Join(t.TempDir(), "needs-plan.yaml")
	if _, err := seedFile(path, builtInSpec); err == nil {
		t.Fatal("the stopped write is an error")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("nothing is left at the path: %v", err)
	}
	store.WriteTemp = write
	if created, err := seedFile(path, builtInSpec); err != nil || !created {
		t.Fatalf("the next init places it: %v %v", created, err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(builtInSpec) {
		t.Fatalf("got %q", b)
	}
}
