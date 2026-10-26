package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.7: a store keeps its name (records/store-name, the hash of the path it was first opened at) for good, so moving the folder renames
// nothing and no records are moved. The real hook flows run (a request, the end of the turn, a delivered lesson, an assessment), the
// folder is moved and reconnected by the next request, the flows run again, and the names under the store are the same as before.
func TestMovingTheFolderRenamesNothingInTheStore(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	flows := func(st store.Store, scope, session string) {
		memoryEffectPrompt(t, scope, session)
		id := memoryEffectComplete(t, st, scope, session)
		memoryEffectSubmit(t, h, id, "lesson:"+draft.ID+":"+revision, "helpful", "added a check", "synthetic test result")
	}
	names := func(root string) []string {
		var out []string
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && (filepath.Base(filepath.Dir(p)) == "feedback" || filepath.Base(filepath.Dir(p)) == "memories" || filepath.Base(filepath.Dir(p)) == "memory-reviews" || filepath.Base(filepath.Dir(p)) == "wiki-sources" || filepath.Base(filepath.Dir(p)) == "lessons") {
				out = append(out, filepath.Base(p))
			}
			return nil
		})
		sort.Strings(out)
		return out
	}
	flows(st, scope, "s1")
	name := store.StoreName(st.Dir, scope)
	before := names(st.Dir)
	moved := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(scope, moved); err != nil {
		t.Fatal(err)
	}
	moved = feedbackScope(moved)
	t.Chdir(moved)
	movedStore := store.Store{Dir: filepath.Join(moved, ".shikii")}
	flows(movedStore, moved, "s2")
	after := names(movedStore.Dir)
	if store.StoreName(movedStore.Dir, moved) != name {
		t.Fatalf("the store keeps its name after the move: %s -> %s", name, store.StoreName(movedStore.Dir, moved))
	}
	for _, n := range after {
		found := false
		for _, b := range before {
			found = found || b == n
		}
		if !found {
			t.Errorf("a new name appeared after the move: %s (before: %v)", n, before)
		}
	}
	if len(before) < 5 {
		t.Fatalf("precondition: the flows wrote the store: %v", before)
	}
}
