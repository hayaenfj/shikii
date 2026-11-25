package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// `shikii forget` takes the lines of the conversations it forgets out of .shikii/nap-first (a line names a conversation, and only the
// conversation's record says which session it is of), keeps them with what else it removed, and leaves the other lines. A plan only counts
// them.
func TestForgetTakesTheLinesOfTheConversationsItForgetsOutOfNapFirst(t *testing.T) {
	w := newForgetWorld(t)
	other := "ffffffffffffffff"
	start := w.tA + " 0\n" + w.tB + " 1\n" + other + " 0\n"
	if err := os.WriteFile(napFirstPath(w.root), []byte(start), 0o600); err != nil {
		t.Fatal(err)
	}
	before := w.snapshot()
	out, err := forgetCommand(t, w.A)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("a listing changed the project: %v", forgetDiff(before, after))
	}
	if !strings.Contains(out, "nap-first: 1 lines") {
		t.Fatalf("the plan counts the line it would remove:\n%s", out)
	}
	if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, want := readFile(t, napFirstPath(w.root)), w.tB+" 1\n"+other+" 0\n"; got != want {
		t.Fatalf("the line of the forgotten conversation is out, the others stay:\n got %q\nwant %q", got, want)
	}
	var kept string
	for path, data := range w.snapshot() {
		if strings.HasPrefix(path, "store/records/forgotten/") && strings.HasSuffix(path, "/"+napFirstName) {
			kept += string(data)
		}
	}
	if kept != w.tA+" 0\n" {
		t.Fatalf("the removed line is kept with the rest of what forget removed: %q", kept)
	}
	if _, err := os.Lstat(filepath.Join(w.root, napFirstTmpName)); !os.IsNotExist(err) {
		t.Fatalf("no temp file is left: %v", err)
	}
}

// A run of forget that cannot get the lock of .shikii/nap-first in time stops at that step with an error and removes nothing of it: the
// same call again, once the other run is done, removes the rest.
func TestForgetStopsAtTheStepOfNapFirstWhenAnotherRunHoldsItsLock(t *testing.T) {
	w := newForgetWorld(t)
	start := w.tA + " 0\n" + w.tB + " 1\n"
	if err := os.WriteFile(napFirstPath(w.root), []byte(start), 0o600); err != nil {
		t.Fatal(err)
	}
	release := holdNapFirstLock(t, w.root)
	useNapFirstWait(t, 0, nil)
	if out, err := forgetCommand(t, w.A, "--apply"); err == nil && !strings.Contains(out, "steps_left") && !strings.Contains(out, "not removed") {
		t.Fatalf("forget says it did not finish while the lock is held:\n%s", out)
	}
	if got := readFile(t, napFirstPath(w.root)); got != start {
		t.Fatalf("nothing of nap-first changed: %q", got)
	}
	release()
	if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readFile(t, napFirstPath(w.root)); got != w.tB+" 1\n" {
		t.Fatalf("the same call again removes the rest: %q", got)
	}
}
