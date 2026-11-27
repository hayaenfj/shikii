package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conflictMarkers are the lines git writes into a file when a merge or an apply -3 cannot join two changes (the merge style and
// the diff3 style). They are spelled in pieces so this file does not hold them at the start of a line itself.
var conflictMarkers = []string{"<<<<<<" + "< ", "||||||" + "| ", ">>>>>>" + "> "}

// A conflict left unresolved in a tracked file ships to every reader (0.44.0's candidate kept a "||||||| base" line and a second
// Tests badge in both READMEs after an apply -3, and check-readme still passed). Every tracked or new file is read as it is on disk.
func TestNoConflictMarkerIsLeftInTheRepository(t *testing.T) {
	root := strings.TrimSpace(run(t, "git", "rev-parse", "--show-toplevel"))
	out := run(t, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	var hits []string
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		if info, err := os.Lstat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range conflictMarkers {
				if strings.HasPrefix(line, m) {
					hits = append(hits, name+":"+itoa(i+1))
				}
			}
		}
	}
	if len(hits) > 0 {
		t.Errorf("a merge conflict is left unresolved at %s", strings.Join(hits, ", "))
	}
}
