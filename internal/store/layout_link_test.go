package store

import (
	"os"
	"path/filepath"
	"testing"
)

// The store's project.json is not read through a link (like the store's name): a link there names no project.
func TestInitializedAtDoesNotReadAProjectFileThatIsALink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "other.json"), []byte(`{"scope":"/elsewhere"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.json", filepath.Join(dir, "project.json")); err != nil {
		t.Fatal(err)
	}
	if got := initializedAt(dir); got != "" {
		t.Fatalf("project.json is a link, yet the file behind it was read: %q", got)
	}
	if err := os.Remove(filepath.Join(dir, "project.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), []byte(`{"scope":"/here"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := initializedAt(dir); got != "/here" {
		t.Fatalf("a regular project.json was not read: %q", got)
	}
}
