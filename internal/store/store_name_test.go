package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// The name "project" was used only by 0.9.7 builds in development, never released; it is not read (backward compatibility is kept for
// released versions only, 2026-10-27). A folder of that name does not change the store's name.
func TestTheDevelopmentNameProjectIsNotUsed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wiki", "lessons", "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("/work/p"))
	if got := StoreName(dir, "/work/p"); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("the name is the hash of the path, not %q", got)
	}
}
