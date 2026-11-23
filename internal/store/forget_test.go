package store

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func forgetStore(t *testing.T) (Store, *Forgotten) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir()) // the store resolves a path the way the system names it
	if err != nil {
		t.Fatal(err)
	}
	s := Store{Dir: dir}
	f, err := s.NewForgotten(time.Date(2026, 11, 23, 1, 2, 3, 0, time.UTC), "ses_x")
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func writeForgetFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The lines that are chosen go to the kept file as they were, every other line stays byte for byte in its place (a line that holds
// nothing, a line that is not JSON, a line without a final newline), and a file with nothing to remove is not replaced.
func TestRewriteJSONLRemovesOnlyTheChosenLines(t *testing.T) {
	s, f := forgetStore(t)
	path := filepath.Join(s.Dir, "records", "x.jsonl")
	original := "{\"id\":1}\n   \nnot json at all\n{\"id\":2,\"pad\":\"   \"}\n{\"id\":3}"
	writeForgetFile(t, path, original)
	choose := func(ids ...string) func([]byte) bool {
		return func(line []byte) bool {
			for _, id := range ids {
				if bytes.HasPrefix(line, []byte(`{"id":`+id)) {
					return true
				}
			}
			return false
		}
	}
	before, _ := os.Stat(path)
	if n, err := s.RewriteKept(path, choose("9"), f); err != nil || n != 0 {
		t.Fatalf("nothing is chosen: %d %v", n, err)
	}
	if after, _ := os.Stat(path); !os.SameFile(before, after) {
		t.Errorf("a file with nothing to remove was replaced")
	}
	n, err := s.RewriteKept(path, choose("2", "3"), f)
	if err != nil || n != 2 {
		t.Fatalf("two lines are chosen: %d %v", n, err)
	}
	if got := readForgetFile(t, path); got != "{\"id\":1}\n   \nnot json at all\n" {
		t.Errorf("the kept lines are not as they were:\n%q", got)
	}
	if got := readForgetFile(t, filepath.Join(f.Dir, "records", "x.jsonl")); got != "{\"id\":2,\"pad\":\"   \"}\n{\"id\":3}\n" {
		t.Errorf("the removed lines are not kept as they were:\n%q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".forget-") {
			t.Errorf("a temporary copy is left: %s", e.Name())
		}
	}
}

func readForgetFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// When the removed lines cannot be kept, the file is not changed.
func TestRewriteJSONLChangesNothingWhenTheLinesCannotBeKept(t *testing.T) {
	s, _ := forgetStore(t)
	path := filepath.Join(s.Dir, "records", "x.jsonl")
	writeForgetFile(t, path, "{\"id\":1}\n{\"id\":2}\n")
	_, err := s.RewriteJSONL(path, func(line []byte) bool { return bytes.Contains(line, []byte(`"id":2`)) }, func([][]byte) error { return os.ErrPermission })
	if err != os.ErrPermission {
		t.Fatalf("the failure to keep is not returned: %v", err)
	}
	if got := readForgetFile(t, path); got != "{\"id\":1}\n{\"id\":2}\n" {
		t.Errorf("the file changed though its lines were not kept: %q", got)
	}
}

// A writer that holds the file's lock finishes its line first: the rewrite waits, then keeps that line too.
func TestRewriteJSONLKeepsALineAppendedWhileItWaitsForTheLock(t *testing.T) {
	s, f := forgetStore(t)
	path := filepath.Join(s.Dir, "records", "x.jsonl")
	writeForgetFile(t, path, "{\"id\":1}\n{\"id\":2}\n")
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- s.WithAppendLock(path, func() error {
			close(held)
			<-release
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = file.WriteString("{\"id\":3}\n")
			return err
		})
	}()
	<-held
	rewritten := make(chan error, 1)
	go func() {
		_, err := s.RewriteKept(path, func(line []byte) bool { return bytes.Contains(line, []byte(`"id":2`)) }, f)
		rewritten <- err
	}()
	select {
	case err := <-rewritten:
		t.Fatalf("the file was rewritten while a writer held its lock (%v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-rewritten; err != nil {
		t.Fatal(err)
	}
	if got := readForgetFile(t, path); got != "{\"id\":1}\n{\"id\":3}\n" {
		t.Errorf("the line appended meanwhile is lost or the chosen one stayed: %q", got)
	}
}

func decisionLine(trace string) string {
	return `{"trace_id":"` + trace + `","spec_id":"q","spec_hash":"h","type":"noul","band":"act","at":"2026-11-23T00:00:00Z"}` + "\n"
}

func labelLine(trace, source string) string {
	return `{"trace_id":"` + trace + `","spec_id":"q","spec_hash":"h","type":"noul","truth":"yes","weight":1,"source":"` + source + `","at":"2026-11-23T00:00:00Z"}` + "\n"
}

// A decision a person labeled stays with every label it has; the decisions only machines labeled, or nobody did, go with their machine
// labels. A source this code does not know is a person's: it keeps the decision.
func TestForgetDecisionsKeepsWhatAPersonLabeled(t *testing.T) {
	s, f := forgetStore(t)
	writeForgetFile(t, s.RecordsFile("q"), decisionLine("t1")+decisionLine("t2")+decisionLine("t3")+decisionLine("t4")+decisionLine("t5"))
	writeForgetFile(t, s.LabelsFile("q"), labelLine("t1", "ui")+labelLine("t1", "outcome")+labelLine("t2", "outcome")+labelLine("t2", "judge")+labelLine("t4", "a-source-nobody-knows")+labelLine("t5", "ask")+labelLine("other", "ui"))
	want := map[string]bool{"t1": true, "t2": true, "t3": true, "t4": true, "t5": true}

	listed, err := s.ForgetDecisions("q", want, f, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(listed.Removed, []string{"t2", "t3"}) || listed.Labels != 2 || !reflect.DeepEqual(listed.Kept, map[string]string{"t1": "ui", "t4": "a-source-nobody-knows", "t5": "ask"}) {
		t.Fatalf("the list is wrong: %+v", listed)
	}
	if readForgetFile(t, s.RecordsFile("q")) != decisionLine("t1")+decisionLine("t2")+decisionLine("t3")+decisionLine("t4")+decisionLine("t5") {
		t.Fatalf("a list changed the records")
	}

	res, err := s.ForgetDecisions("q", want, f, true)
	if err != nil || !reflect.DeepEqual(res, listed) {
		t.Fatalf("the application differs from the list: %+v %v", res, err)
	}
	if got := readForgetFile(t, s.RecordsFile("q")); got != decisionLine("t1")+decisionLine("t4")+decisionLine("t5") {
		t.Errorf("the decisions left are wrong:\n%s", got)
	}
	if got := readForgetFile(t, s.LabelsFile("q")); got != labelLine("t1", "ui")+labelLine("t1", "outcome")+labelLine("t4", "a-source-nobody-knows")+labelLine("t5", "ask")+labelLine("other", "ui") {
		t.Errorf("the labels left are wrong:\n%s", got)
	}
	if got := readForgetFile(t, filepath.Join(f.Dir, "records", "q.jsonl")); got != decisionLine("t2")+decisionLine("t3") {
		t.Errorf("the removed decisions are not kept:\n%s", got)
	}
	if got := readForgetFile(t, filepath.Join(f.Dir, "labels", "q.jsonl")); got != labelLine("t2", "outcome")+labelLine("t2", "judge") {
		t.Errorf("the removed labels are not kept:\n%s", got)
	}
	again, err := s.ForgetDecisions("q", want, f, true)
	if err != nil || len(again.Removed) != 0 || again.Labels != 0 {
		t.Errorf("a second application removes more: %+v %v", again, err)
	}
}

// A file is kept whole under its path in the store and then removed; a path outside the store, and a link, are refused and nothing is
// removed.
func TestMoveOutKeepsTheFileAndRefusesWhatItShouldNot(t *testing.T) {
	s, f := forgetStore(t)
	path := filepath.Join(s.Dir, "sessions", "a.json")
	writeForgetFile(t, path, `{"k":"v"}`)
	if err := s.MoveOut(path, f); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
	if got := readForgetFile(t, filepath.Join(f.Dir, "sessions", "a.json")); got != `{"k":"v"}` {
		t.Errorf("the kept copy is wrong: %q", got)
	}
	outside := filepath.Join(t.TempDir(), "o.json")
	writeForgetFile(t, outside, "o")
	if err := s.MoveOut(outside, f); err == nil {
		t.Errorf("a file outside the store was moved")
	}
	target := filepath.Join(s.Dir, "sessions", "target.json")
	writeForgetFile(t, target, "t")
	link := filepath.Join(s.Dir, "sessions", "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := s.MoveOut(link, f); err == nil {
		t.Errorf("a link was moved")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("a refused link was removed: %v", err)
	}
	if err := s.MoveOut(target, f); err != nil {
		t.Fatal(err)
	}
	// a second file of the same path in one run is refused: a run keeps each path once
	writeForgetFile(t, target, "again")
	if err := s.MoveOut(target, f); err == nil {
		t.Errorf("a path kept twice in one run was overwritten")
	}
}
