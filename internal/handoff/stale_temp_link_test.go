package handoff

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The sweep of stale temporary files does not list a folder whose name is a link: a temporary file in the folder behind it stays.
func TestStaleTempsAreNotSweptThroughAFolderThatIsALink(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(dir, "real", ".write-0123456789abcdef")
	if err := os.WriteFile(temp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * staleTempAge)
	if err := os.Chtimes(temp, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "lnk")); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	removeStaleTemps(r, "lnk")
	if _, err := os.Lstat(temp); err != nil {
		t.Fatalf("a temporary file behind a folder link was swept: %v", err)
	}
	removeStaleTemps(r, "real")
	if _, err := os.Lstat(temp); !os.IsNotExist(err) {
		t.Fatalf("premise: the stale temporary file in the folder itself is swept: %v", err)
	}
}
