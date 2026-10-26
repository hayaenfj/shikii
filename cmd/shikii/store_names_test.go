package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.7: nothing inside .shikii is named by the project's path. A name made from the path breaks when the folder is moved or renamed, and
// before 0.9.7 each version found another one (the wiki, its sources, conversations, memory deliveries, memory reviews). The real hook flows
// run here (a request, the end of the turn, a delivered lesson, an assessment), then every name under the store is checked for the path's hash.
func TestNothingInTheStoreIsNamedByTheProjectPath(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "s1")
	id := memoryEffectComplete(t, st, scope, "s1")
	memoryEffectSubmit(t, h, id, "lesson:"+draft.ID+":"+revision, "helpful", "\u78ba\u8a8d\u3092\u8db3\u3057\u305f", "synthetic test result")
	sum := sha256.Sum256([]byte(scope))
	hash := hex.EncodeToString(sum[:])
	checked := 0
	_ = filepath.WalkDir(st.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		checked++
		if strings.Contains(d.Name(), hash) {
			rel, _ := filepath.Rel(st.Dir, p)
			t.Errorf("%s is named by the project's path: use a fixed name, so moving the folder keeps it", rel)
		}
		return nil
	})
	if checked < 20 {
		t.Fatalf("precondition: the flows wrote the store (%d names)", checked)
	}
}
