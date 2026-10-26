package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Final audit BC-3 (B1): records/store-name was created with O_EXCL and then written. A stop between the two (kill -9, power loss) or a
// failed write (a full disk) left an empty store-name, which was never read as a name nor written again, so the name followed the
// current path and changed with a move. The name is now written whole or not at all, and a file without a valid name is replaced.
func TestFinalBC3EmptyStoreNameLetsTheNameChangeAfterAMove(t *testing.T) {
	for _, empty := range []bool{false, true} { // false is the control: the name is written and kept
		t.Run(map[bool]string{false: "control", true: "empty-store-name"}[empty], func(t *testing.T) { finalBC3(t, empty) })
	}
}

func finalBC3(t *testing.T, empty bool) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	// the state after O_EXCL created the file and the write did not happen
	if empty {
		if err := os.WriteFile(filepath.Join(dir, "records", "store-name"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := Store{Dir: dir}
	if err := s.AppendFeedback("/work/old", Feedback{ID: "0000000000000abc", Request: "fix the readme", Summary: "fixed", Reply: "thanks"}); err != nil {
		t.Fatal(err)
	}
	// the folder is moved; the hook records the old path's name first, then the store is opened at the new path
	RecordStoreNameFrom(dir, "/work/old")
	got, err := s.RecentFeedback("/work/new")
	sum := sha256.Sum256([]byte("/work/old"))
	recorded, _ := os.ReadFile(filepath.Join(dir, "records", "store-name"))
	if err != nil || len(got) != 1 || strings.TrimSpace(string(recorded)) != hex.EncodeToString(sum[:]) {
		t.Fatalf("B1: the name is written once and kept after a move: %d conversations after the move (%v), store-name=%q", len(got), err, recorded)
	}
}
