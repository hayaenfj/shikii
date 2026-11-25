package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// What a conversation was when it was first printed lives in .shikii/nap-first, a file of its own (nap_first.go): the state is not carried
// from one record of what was shown to the next, which a run that prints nothing, or does not print the conversation, would lose.

// readNapFirstFile is the content of .shikii/nap-first, "" when it is not there.
func readNapFirstFile(t *testing.T, st store.Store) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(st.Dir, napFirstName))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// The first run writes a line for each conversation it prints (the id and whether an article cited it), the record of what was shown holds
// no state, and the run that moves the window over the conversations takes their lines out when it counts them.
func TestNapKeepsTheFirstStateOfAConversationInItsOwnFileUntilItIsCounted(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	decodeNap(t) // 1 is cited by the fixture's article, 2 is not
	if got, want := readNapFirstFile(t, st), "0000000000000001 1\n0000000000000002 0\n"; got != want {
		t.Fatalf("nap-first after the first run:\n got %q\nwant %q", got, want)
	}
	shown, err := os.ReadFile(napShownPath(st.Dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(shown), "\n"), "\n")[1:] {
		if n := len(strings.Fields(line)); n != 3 {
			t.Fatalf("a line of the record of what was shown holds the id, the offset and the hash, and no state: %q", line)
		}
	}
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
	if got := readNapFirstFile(t, st); got != "" {
		t.Fatalf("the conversations were counted, so their lines are out of nap-first: %q", got)
	}
}

// A conversation of a record of what was shown from before nap-first (a fourth field of 0 or 1) takes that state: the lines of the file
// are what the run before it wrote, and it counts them.
func TestNapReadsTheStateAnOlderRecordOfWhatWasShownCarried(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	decodeNap(t)
	if err := os.Remove(filepath.Join(st.Dir, napFirstName)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(napShownPath(st.Dir))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	lines[1] += " 1" // the older form: whether an article cited it when it was first printed
	lines[2] += " 0"
	if err := os.WriteFile(napShownPath(st.Dir), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
}

// The record of what was shown is written without a state, and a copy is the fifth field behind a "-" where the state used to be; the
// older forms are read as before, and a "-" there says nothing about the state.
func TestNapShownRecordIsWrittenWithoutTheStateAndStillReadsTheOlderForms(t *testing.T) {
	window := "w"
	convs := []napConversation{{ID: "c1"}, {ID: "c2"}}
	ends := []store.FeedbackMark{{Offset: 10, Hash: strings.Repeat("ab", 32)}, {Offset: 20, Hash: strings.Repeat("cd", 32), Copy: 2}}
	got := formatNapShown(window, convs, ends)
	want := "\"w\"\nc1 10 " + strings.Repeat("ab", 32) + "\nc2 20 " + strings.Repeat("cd", 32) + " - 2\n"
	if got != want {
		t.Fatalf("the record of what was shown:\n got %q\nwant %q", got, want)
	}
	back, ok := parseNapShown(got)
	if !ok || len(back.convs) != 2 || back.convs[0].mark != ends[0] || back.convs[1].mark != ends[1] || back.convs[0].cited != napCitedUnknown {
		t.Fatalf("what is written is read back: %v %+v", ok, back)
	}
	hash := strings.Repeat("ab12", 16)
	for text, ok := range map[string]bool{
		"\"\"\nc1 100 " + hash + " -\n":     true,
		"\"\"\nc1 100 " + hash + " - 2\n":   true,
		"\"\"\nc1 100 " + hash + " - 0\n":   false,
		"\"\"\nc1 100 " + hash + " - x\n":   false,
		"\"\"\nc1 100 " + hash + " - 1 2\n": false,
		"\"\"\nc1 100 " + hash + " -- 2\n":  false,
	} {
		if _, got := parseNapShown(text); got != ok {
			t.Errorf("%q: read as %v, want %v", text, got, ok)
		}
	}
}

// The file is read and written only when it is an ordinary file with one name: a link is a file of the person's and is never written
// through, the report is printed all the same, and the error says to remove the file.
func TestNapSaysToRemoveAnUnusableFirstFileAndNeverWritesThroughIt(t *testing.T) {
	for _, kind := range []string{"a hard link", "a symbolic link"} {
		t.Run(kind, func(t *testing.T) {
			st, _, _, _, _ := memoryEffectFixture(t)
			path := filepath.Join(st.Dir, napFirstName)
			var other string
			if kind == "a hard link" {
				other = linkToAPersonsFile(t, path, napPersonsFile)
			} else {
				other = filepath.Join(t.TempDir(), "persons-file")
				if err := os.WriteFile(other, []byte(napPersonsFile), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			}
			r, _, err := napPrintedRun(t, st)
			if len(r.Conversations) != 1 {
				t.Fatalf("the conversation is printed all the same: %v", conversationIDs(r))
			}
			if err == nil || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), napFirstName) {
				t.Fatalf("the error tells the person to remove .shikii/nap-first: %v", err)
			}
			requireFileHolds(t, other, napPersonsFile)
		})
	}
}

// The file keeps the newest napFirstKeep lines: a conversation that left the record without the window moving over it leaves its line
// behind, and the file does not grow for ever.
func TestNapFirstFileKeepsOnlyTheNewestLines(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	var old strings.Builder
	extra := 10
	for i := 0; i < napFirstKeep-1+extra; i++ { // one more line is added by the run for each of the two conversations
		fmt.Fprintf(&old, "%016x 0\n", 1000+i)
	}
	if err := os.WriteFile(filepath.Join(st.Dir, napFirstName), []byte(old.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	decodeNap(t) // prints 1 and 2
	lines := strings.Split(strings.TrimSuffix(readNapFirstFile(t, st), "\n"), "\n")
	if len(lines) != napFirstKeep {
		t.Fatalf("the file keeps %d lines: %d", napFirstKeep, len(lines))
	}
	if lines[len(lines)-2] != "0000000000000001 1" || lines[len(lines)-1] != "0000000000000002 0" {
		t.Fatalf("the newest lines are the ones just added: %q", lines[len(lines)-2:])
	}
	if want := fmt.Sprintf("%016x 0", 1000+extra+1); lines[0] != want {
		t.Fatalf("the oldest lines went: first line %q, want %q", lines[0], want)
	}
}

// Two runs that start from the same record both print the same conversations for the first time: the file has one line for each.
func TestNapFirstFileHasOneLineForAConversationTwoRunsPrintedForTheFirstTime(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	a, err := runNapReport(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runNapReport(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []napReport{a, b} {
		if err := recordNapShown(st.Dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := readNapFirstFile(t, st), "0000000000000001 1\n0000000000000002 0\n"; got != want {
		t.Fatalf("one line for each conversation:\n got %q\nwant %q", got, want)
	}
}
