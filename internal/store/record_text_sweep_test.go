package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// textFile writes an original text for trace and makes it look written at the given time.
func textFile(t *testing.T, st Store, trace string, at time.Time) string {
	t.Helper()
	rec := textRecord("prompt-kind", trace, at)
	if err := st.SaveRecordText(rec, map[string]string{"request": rec.Inputs["request"] + " and the long original"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Dir, "records", "text", trace+".json")
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

// A text whose decision is gone from every question (left behind by a run that could not tell a line's trace) is removed by a
// later sweep; a text whose trace still has a decision line in any question stays.
func TestSweepRemovesTextsLeftWithoutADecision(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	old := time.Now().Add(-48 * time.Hour)
	if err := st.AppendRecord(textRecord("needs-plan", "bbbbbbbbbbbbbbbb", old)); err != nil {
		t.Fatal(err)
	}
	orphan := textFile(t, st, "aaaaaaaaaaaaaaaa", old)
	kept := textFile(t, st, "bbbbbbbbbbbbbbbb", old)
	n, blocked, err := st.SweepRecordTexts(time.Now().Add(-24*time.Hour), false)
	if err != nil || n != 1 || blocked {
		t.Fatalf("a dry run counts the one text left without a decision: n=%d blocked=%v err=%v", n, blocked, err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("a dry run removes nothing: %v", err)
	}
	if n, blocked, err := st.SweepRecordTexts(time.Now().Add(-24*time.Hour), true); err != nil || n != 1 || blocked {
		t.Fatalf("the sweep removes it: n=%d blocked=%v err=%v", n, blocked, err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the text left without a decision is removed: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a text whose decision is kept stays: %v", err)
	}
}

// The sweep removes nothing when a records file holds a line that names no decision, nothing younger than the age given, and
// nothing younger than an hour whatever the age (a decision's text may be written just before its line). The guard records
// are not decisions and do not keep a text.
func TestSweepKeepsWhatItCannotTellAndWhatIsFresh(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	old := time.Now().Add(-48 * time.Hour)
	orphan := textFile(t, st, "aaaaaaaaaaaaaaaa", old)
	fresh := textFile(t, st, "cccccccccccccccc", time.Now().Add(-10*time.Minute))
	if err := os.WriteFile(filepath.Join(st.Dir, "records", "guards.jsonl"), []byte(`{"turn_id":"t","guard":"g","trace_id":"aaaaaaaaaaaaaaaa"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir, "records", "needs-plan.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, blocked, err := st.SweepRecordTexts(time.Now(), true); err != nil || n != 0 || !blocked {
		t.Fatalf("a line without a trace blocks the sweep: n=%d blocked=%v err=%v", n, blocked, err)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("nothing is removed while a line names no decision: %v", err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir, "records", "needs-plan.jsonl"), nil, 0o600); err != nil { // the line is repaired
		t.Fatal(err)
	}
	if n, _, err := st.SweepRecordTexts(time.Now().Add(-72*time.Hour), true); err != nil || n != 0 {
		t.Fatalf("a text younger than the age given stays: n=%d err=%v", n, err)
	}
	if n, blocked, err := st.SweepRecordTexts(time.Now(), true); err != nil || n != 1 || blocked {
		t.Fatalf("with any age, the old orphan goes and the fresh one stays: n=%d blocked=%v err=%v", n, blocked, err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a text younger than an hour stays whatever the age: %v", err)
	}
}

// The sweep never follows a link out of the store: a linked text folder is left alone.
func TestSweepDoesNotFollowALinkedTextFolder(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	outside := t.TempDir()
	victim := filepath.Join(outside, "aaaaaaaaaaaaaaaa.json")
	if err := os.WriteFile(victim, []byte(`{"inputs_hash":"x","inputs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(victim, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(st.Dir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(st.Dir, "records", "text")); err != nil {
		t.Fatal(err)
	}
	st.SweepRecordTexts(time.Now(), true)
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a file outside the store was removed through a link: %v", err)
	}
}
