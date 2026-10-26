package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Properties B1 and B2 of .shikii/handoff/audit-0.9.7/invariants.md over random sequences of what an older shikii (before 0.9.7, still
// running) and this one do to records/feedback (0.9.7 quality bar): every line the older shikii finished writing reaches the fixed name at
// least once, whatever happens in between, and no broken line ever does. The operations are what the two do: the older shikii appends and
// may stop in the middle of a line, the mark may be broken, the store may be copied, and this one reads. Nobody removes the older file or
// empties the fixed one, so those are not generated.
func TestOlderShikiisLinesArriveWhateverHappens(t *testing.T) {
	r := rand.New(rand.NewSource(97))
	runs := 300
	if testing.Short() {
		runs = 40
	}
	for run := 0; run < runs; run++ {
		s := Store{Dir: t.TempDir()}
		scope := "/work/p"
		upgrade := r.Intn(2) == 0 // the update moment: the older shikii wrote first and the fixed name does not exist yet
		if !upgrade {
			if err := s.AppendFeedback(scope, Feedback{ID: "f000000000000000", Request: "a", Summary: "b", Reply: "c"}); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(filepath.Join(s.Dir, "records", "feedback"), 0o700); err != nil {
			t.Fatal(err)
		}
		path := func() (string, string) {
			dir := filepath.Join(s.Dir, "records", "feedback")
			return filepath.Join(dir, scopeKey(scope)+".jsonl"), filepath.Join(dir, projectName+".jsonl")
		}
		written := map[string]bool{} // ids the older shikii finished writing
		partial := ""                // the line it is in the middle of writing
		next := 1
		line := func(id string) string {
			return fmt.Sprintf(`{"id":%q,"request":"x","summary":"y","reply":"z","at":"2026-10-01T00:00:00Z"}`+"\n", id)
		}
		appendRaw := func(b string) {
			from, _ := path()
			f, err := os.OpenFile(from, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			f.WriteString(b)
			f.Close()
		}
		var steps []string
		for step := 0; step < 30; step++ {
			from, _ := path()
			switch op := r.Intn(8); op {
			case 0, 1: // the older shikii writes a whole line
				if partial != "" {
					break
				}
				id := fmt.Sprintf("%016x", next)
				next++
				appendRaw(line(id))
				written[id] = true
				steps = append(steps, "append "+id)
			case 2: // it starts a line and stops in the middle
				if partial != "" {
					break
				}
				id := fmt.Sprintf("%016x", next)
				next++
				l := line(id)
				cut := 1 + r.Intn(len(l)-2)
				appendRaw(l[:cut])
				partial = id + l[cut:]
				steps = append(steps, "half "+id)
			case 3: // it finishes the line
				if partial == "" {
					break
				}
				id, rest := partial[:16], partial[16:]
				appendRaw(rest)
				written[id] = true
				partial = ""
				steps = append(steps, "finish "+id)
			case 4: // the merge mark is broken
				os.WriteFile(from+".merged", []byte([]string{"-1", "x", "99999999 abc", "", "1:1 5"}[r.Intn(5)]), 0o600)
				steps = append(steps, "mark broken")
			case 5: // the store is copied and work goes on in the copy
				dst := filepath.Join(t.TempDir(), "copy")
				if out, err := exec.Command("cp", "-Rp", s.Dir, dst).CombinedOutput(); err != nil {
					t.Fatal(err, string(out))
				}
				s = Store{Dir: dst}
				steps = append(steps, "copied")
			default: // this shikii reads (and merges)
				if _, err := s.RecentFeedback(scope); err != nil {
					t.Fatal(err)
				}
				steps = append(steps, "merge")
			}
		}
		if _, err := s.RecentFeedback(scope); err != nil {
			t.Fatal(err)
		}
		_, fixed := path()
		data, _ := os.ReadFile(fixed)
		seen := map[string]bool{}
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			var f Feedback
			if json.Unmarshal(sc.Bytes(), &f) != nil {
				t.Fatalf("run %d: a broken line in the fixed name: %q\nsteps: %v", run, sc.Text(), steps)
			}
			seen[f.ID] = true
		}
		for id := range written {
			if !seen[id] {
				t.Fatalf("run %d: a line the older shikii finished writing was lost: %s\nsteps: %v", run, id, steps)
			}
		}
	}
}
