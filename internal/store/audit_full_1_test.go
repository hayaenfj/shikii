package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func full1Line(id string) string {
	return fmt.Sprintf(`{"id":%q,"request":"x","summary":"y","reply":"z","at":"2026-10-01T00:00:00Z"}`+"\n", id)
}

func full1Check(t *testing.T, s Store, want ...string) {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(s.Dir, "records", "feedback", projectName+".jsonl"))
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		var f Feedback
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			t.Errorf("B2: a broken line in the fixed name: %q", sc.Text())
			continue
		}
		seen[f.ID] = true
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("B1: a line the older shikii finished writing was lost: %s", id)
		}
	}
}

func full1Append(t *testing.T, path, b string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(b)
	f.Close()
}

// (1) the older shikii stops in the middle of a line and never finishes it (killed); the next older process appends a whole line after it.
func TestFull1TornLineThenNewLine(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	dir := filepath.Join(s.Dir, "records", "feedback")
	os.MkdirAll(dir, 0o700)
	from := filepath.Join(dir, scopeKey(scope)+".jsonl")
	full1Append(t, from, full1Line("0000000000000001"))
	full1Append(t, from, full1Line("0000000000000002")[:20]) // killed mid-line
	full1Append(t, from, full1Line("0000000000000003"))      // the next process
	if _, err := s.RecentFeedback(scope); err != nil {
		t.Fatal(err)
	}
	full1Check(t, s, "0000000000000001", "0000000000000003")
}

// (2) a mark of the number only (the 9b77d07 form, taken as it is) that points into the middle of a line.
func TestFull1NumberOnlyMarkMidLine(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	dir := filepath.Join(s.Dir, "records", "feedback")
	os.MkdirAll(dir, 0o700)
	from := filepath.Join(dir, scopeKey(scope)+".jsonl")
	full1Append(t, from, full1Line("0000000000000001"))
	os.WriteFile(from+".merged", []byte("12"), 0o600)
	if _, err := s.RecentFeedback(scope); err != nil {
		t.Fatal(err)
	}
	full1Check(t, s, "0000000000000001")
}

// (3) the only hash-named file (written under another path's hash) is merged once; what the older shikii appends to it later never arrives.
func TestFull1OnlyHashNamedIsMergedOnce(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	dir := filepath.Join(s.Dir, "records", "feedback")
	os.MkdirAll(dir, 0o700)
	from := filepath.Join(dir, scopeKey("/work/old")+".jsonl")
	full1Append(t, from, full1Line("0000000000000001"))
	if _, err := s.RecentFeedback("/work/new"); err != nil {
		t.Fatal(err)
	}
	full1Append(t, from, full1Line("0000000000000002")) // the older shikii, still running with the path it started at
	if _, err := s.RecentFeedback("/work/new"); err != nil {
		t.Fatal(err)
	}
	full1Check(t, s, "0000000000000001", "0000000000000002")
}

// (4) the same for memory-reviews: the folder is adopted, and a review the older shikii writes after that under its hash never arrives.
func TestFull1OnlyHashNamedReviewFolderAdoptedOnce(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	dir := filepath.Join(s.Dir, "records", "memory-reviews")
	old := filepath.Join(dir, scopeKey("/work/old"))
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "a.json"), []byte("{}"), 0o600)
	s.projectPath("memory-reviews", "", "/work/new")
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "b.json"), []byte("{}"), 0o600)
	fixed := s.projectPath("memory-reviews", "", "/work/new")
	for _, n := range []string{"a.json", "b.json"} {
		if _, err := os.Stat(filepath.Join(fixed, n)); err != nil {
			t.Errorf("B1: %s written by the older shikii does not reach the fixed folder", n)
		}
	}
}
