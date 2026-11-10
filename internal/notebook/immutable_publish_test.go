package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImmutablePublishPreservesConflictsAndCleansTemps(t *testing.T) {
	for _, tc := range []struct {
		name     string
		write    func(string, []byte) error
		conflict string
	}{
		{"record", immutable, "an immutable wiki source or revision has changed"},
		{"original", immutableLarge, "a saved original cannot be changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "saved.json")
			data := []byte(strings.Repeat("x", 70<<10))
			for range 2 {
				if err := tc.write(path, data); err != nil {
					t.Fatal(err)
				}
			}
			if err := tc.write(path, []byte("different")); err == nil || err.Error() != tc.conflict {
				t.Fatal("conflict changed", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatal("existing content replaced", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatal("temporary file leaked", entries, err)
			}
			if err := tc.write(filepath.Join(dir, "missing", "file"), data); err == nil {
				t.Fatal("missing parent accepted")
			}
		})
	}
}
