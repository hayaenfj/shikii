package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// Re-audit 0.9.2 (test hole): removing the size check of sourceRecordDigest survived every test, because an oversized record
// also has a different hash. The streamed hash refuses a record over the store's line limit and takes one at the limit.
func TestSourceRecordDigestRefusesOverTheLineLimit(t *testing.T) {
	dir := t.TempDir()
	at, over := filepath.Join(dir, "at.json"), filepath.Join(dir, "over.json")
	if err := os.WriteFile(at, []byte(strings.Repeat("a", store.MaxLineBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(over, []byte(strings.Repeat("a", store.MaxLineBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if hash, err := sourceRecordDigest(at); err != nil || hash != digest([]byte(strings.Repeat("a", store.MaxLineBytes))) {
		t.Fatalf("a record at the line limit is hashed whole: %v", err)
	}
	if _, err := sourceRecordDigest(over); err == nil || !strings.Contains(err.Error(), "line limit") {
		t.Fatalf("a record over the line limit is refused: %v", err)
	}
}
