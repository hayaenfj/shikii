package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The lines a forbidden-words guard stopped are kept once per repository (records/stopped-lines/), not per session, and every
// way the Stop works out the added lines of a file applies them: the content at the request read from the repository (a staged
// file, a stash), the kept table of the uncommitted lines, and the request's HEAD. A record kept per session and applied only
// when the next request's table was built would let each of these three through.

// The stopped line is staged (git add), by the turn or by the person between the turns. The next request's listing
// names a blob the repository has, so the Stop compares with that blob; the stopped line still counts as added.
func TestStagedStoppedLineStopsTheNextTurn(t *testing.T) {
	for name, stageInTurn := range map[string]bool{"the turn stages it": true, "the person stages it between the turns": false} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := uncommittedLinesRepo(t)
			organizedRequest(t, repo, root, "s")
			writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
			if stageInTurn {
				gitIn(t, repo, "add", "README.md")
			}
			if r := wordStop(t, repo, root, "s", false); !stoppedForTheLine(r) {
				t.Fatalf("premise: the turn's own forbidden line is refused: %+v", r)
			}
			wordStop(t, repo, root, "s", true)
			if !stageInTurn {
				gitIn(t, repo, "add", "README.md")
			}
			nextRequest(t, repo, root, "s")
			writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
			if r := wordStop(t, repo, root, "s", false); !stoppedForTheLine(r) {
				t.Fatalf("the stopped line, staged and not committed, passes at the next turn: %+v", r)
			}
		})
	}
}

// Session x is stopped for its line and leaves it; another session y in the same repository (after /clear, say)
// changes the same file. The line is still a stopped line there.
func TestStoppedLineStopsATurnOfAnotherSession(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "x")
	nextRequest(t, repo, root, "y")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "y", false); !stoppedForTheLine(r) {
		t.Fatalf("a line stopped in another session and never fixed passes: %+v", r)
	}
}

// One Stop stops so many lines that they do not all fit in the record (stoppedLinesMax, 4 MB). Only what does not fit is dropped:
// the small file's stopped line still stops the next turn.
func TestManyStoppedLinesKeepTheSmallFilesLine(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	organizedRequest(t, repo, root, "g")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	n := 0
	for i := 0; i < 12; i++ {
		var b strings.Builder
		for b.Len() < guardFileBytesMax-64 {
			fmt.Fprintf(&b, "FORBIDDENWORD %09d\n", n)
			n++
		}
		writeFile(t, filepath.Join(repo, fmt.Sprintf("data/g%02d.txt", i)), b.String())
	}
	if r := wordStop(t, repo, root, "g", false); r.Pass {
		t.Fatalf("premise: %+v", r)
	}
	wordStop(t, repo, root, "g", true)
	nextRequest(t, repo, root, "g")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "g", false); !stoppedForTheLine(r) {
		t.Fatalf("after a Stop that stopped %d lines, the small file's stopped line passes at the next turn: %+v", n, r)
	}
}
