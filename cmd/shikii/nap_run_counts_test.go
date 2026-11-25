package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// What `shikii nap` counts, once, and the places it keeps. These are the findings of the audit of 0.42.1 (the first, third and fourth of
// them came with a failing test of the auditor, kept here under a name that says what must hold).

// A line of the record that is in it more than once, byte for byte, with the last one the end of what was read: the window says which
// copy it is, so the conversations before the last copy are not shown again for ever. (shikii itself never writes the same bytes twice:
// a line holds the time to the nanosecond and the id of its turn. A copy comes from a file put back by hand or merged from a backup.)
func TestNapDoesNotShowAgainWhatWasReadWhenTheLastLineIsInTheRecordTwice(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	path := st.FeedbackFile(scope)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := raw[:bytes.IndexByte(raw, '\n')+1]
	appendRawFeedbackLines(t, st, scope, string(first)) // [L, A, L]: the last line is a second copy of the first
	for run := 0; run < 4; run++ {
		r := decodeNap(t)
		ids := conversationIDs(r)
		t.Logf("run %d: %v", run, ids)
		if run >= 2 && len(ids) > 0 {
			t.Fatalf("run %d: the agent sent every id of every run, yet the same conversations come again: %v", run, ids)
		}
		if len(ids) > 0 {
			napSendSources(t, st, scope, ids...)
		}
	}
	want := formatNapWindowEnd(store.FeedbackMark{Offset: int64(len(raw) + len(first)), Hash: hashOfFeedbackLine(first), Copy: 1})
	if got := readNapWindowEndFile(t, st); got != want {
		t.Fatalf("the window says it is after the second copy:\n got %q\nwant %q", got, want)
	}
}

// The same with a copy in the middle of what a run shows: [L, A, L, B]. The conversations are shown once the window is on the copy it
// ended at, and a conversation written after it still comes.
func TestNapGoesOnAfterTheCopyItReadUpToWhenALineIsInTheRecordTwice(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-2*time.Hour), "second")
	raw, err := os.ReadFile(st.FeedbackFile(scope))
	if err != nil {
		t.Fatal(err)
	}
	first := raw[:bytes.IndexByte(raw, '\n')+1]
	appendRawFeedbackLines(t, st, scope, string(first))
	got := conversationIDs(decodeNap(t))
	if fmt.Sprint(got) != "[0000000000000002 0000000000000001]" {
		t.Fatalf("premise: the record is [L, A, L] and the newest line of an id stands: %v", got)
	}
	napSendSources(t, st, scope, got...)
	saveConversation(t, st, scope, "0000000000000003", time.Now().Add(-time.Hour), "third")
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != "[0000000000000003]" {
		t.Fatalf("only the conversation written after the last copy comes: %v", got)
	}
}

// hashOfFeedbackLine is the hash a place holds for a line: SHA-256 of the line without its newline, in hex.
func hashOfFeedbackLine(line []byte) string {
	sum := sha256.Sum256(bytes.TrimSuffix(line, []byte("\n")))
	return hex.EncodeToString(sum[:])
}

// The record of what was shown holds the copy of each conversation's line, and the window file does too; a record from before the field
// (no copy) is the first copy, and anything that is not a copy as written is no record.
func TestNapShownRecordHoldsTheCopyOfALineAndReadsTheOlderFormsToo(t *testing.T) {
	hash := strings.Repeat("ab12", 16)
	for text, want := range map[string]struct {
		ok   bool
		mark store.FeedbackMark
	}{
		"\"\"\nc1 100 " + hash + " 1\n":                      {true, store.FeedbackMark{Offset: 100, Hash: hash}},
		"\"\"\nc1 100 " + hash + "\n":                        {true, store.FeedbackMark{Offset: 100, Hash: hash}}, // from before whether an article cited it was kept
		"\"\"\nc1 100 " + hash + " 0 2\n":                    {true, store.FeedbackMark{Offset: 100, Hash: hash, Copy: 2}},
		"\"\"\nc1 100 " + hash + " 1 12\n":                   {true, store.FeedbackMark{Offset: 100, Hash: hash, Copy: 12}},
		"\"\"\nc1 100 " + hash + " 1 0\n":                    {},
		"\"\"\nc1 100 " + hash + " 1 02\n":                   {},
		"\"\"\nc1 100 " + hash + " 1 -1\n":                   {},
		"\"\"\nc1 100 " + hash + " 1 x\n":                    {},
		"\"\"\nc1 100 " + hash + " 1 2 3\n":                  {},
		"\"\"\nc1 100 " + hash + " 1 +2\n":                   {},
		"\"\"\nc1 100 " + hash + " 1 2 \n":                   {},
		"\"\"\nc1 100 " + hash + " 3 2\n":                    {}, // the fourth field is 0 or 1
		"\"\"\nc1 100 " + hash + " 1  2\n":                   {},
		"\"\"\nc1 100 " + hash + " 1 2\r\n":                  {},
		"\"\"\nc1 100 " + hash + " 1 99999999999999999999\n": {},
	} {
		got, ok := parseNapShown(text)
		if ok != want.ok || (ok && (len(got.convs) != 1 || got.convs[0].mark != want.mark)) {
			t.Errorf("%q: %v %+v, want %v %+v", text, ok, got.convs, want.ok, want.mark)
		}
	}
}

// A save accepts the id of an old conversation that the last run printed (it is listed in .shikii/nap-shown) although no save has kept it:
// a line of the record of what was shown that names a copy (a fifth field) lists the conversation as well as a line without one.
func TestNapAcceptsAsSourcesTheOldConversationsWhoseShownLineNamesACopy(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	const total = 300 // the oldest 44 are older than the newest 256
	for i := 2; i <= total; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(total+10-i)*time.Minute), "reply")
	}
	first := decodeNap(t)
	n := len(first.Conversations)
	if n < 44 {
		t.Fatalf("premise: the first run prints the oldest conversations: %d", n)
	}
	path := napShownPath(st.Dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] += " 1" // the fifth field: the line is the second copy of its bytes (the place is found by the hash and falls back to the first copy)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	napSendSources(t, st, scope, conversationIDs(first)...)
	second := decodeNap(t)
	if len(second.Conversations) == 0 || second.Conversations[0].ID != fmt.Sprintf("%016x", n+1) {
		t.Fatalf("the next run goes on after the conversations the agent sent: %v", conversationIDs(second))
	}
}

// (2) The window could not be written (another run holds its lock): the conversations are not counted by this run. They are printed again,
// and counted once, by the run that moves the window over them.
func TestNapCountsAConversationOnceWhenTheWindowCouldNotBeMovedAtFirst(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	decodeNap(t)
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	f, err := os.OpenFile(filepath.Join(st.Dir, "nap-window-end"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	held, heldOut := decodeNapOutput(t) // another nap holds the window's lock while this run goes
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
	if fmt.Sprint(conversationIDs(held)) != hexIDs(1, 2) {
		t.Fatalf("premise: the window did not move, so 1 and 2 are printed again: %v", conversationIDs(held))
	}
	if napLastNapOf(t, heldOut) != nil || len(napLogRows(t, st)) != 0 {
		t.Fatalf("a run that did not move the window counts nothing: %s %v", heldOut, napLogRows(t, st))
	}
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1) // 2 was printed uncited the first time, and an article cites it now
	shown := 0
	for _, row := range napLogRows(t, st) {
		shown += int(row["shown"].(float64))
	}
	if shown != 2 {
		t.Fatalf("2 conversations were read once each, but the log counts %d shown: %v", shown, napLogRows(t, st))
	}
}

// Two runs that start from the same state (two sessions that ran `shikii nap` at the same time): the one that writes the window counts the
// conversations read, and the other, which finds the window moved, counts none. Whichever of them finishes first, the conversations are
// counted once, in what is printed and in the log: a count that waited for the record of what was shown to be replaced would be lost
// when the run that moved nothing replaced it first.
func TestNapCountsReadConversationsOnceWhenTwoRunsOverlap(t *testing.T) {
	for _, moverFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("the run that moved the window finishes first=%v", moverFirst), func(t *testing.T) {
			st, scope, _, draft, _ := memoryEffectFixture(t)
			book := notebook.Book{Root: st.Dir, Scope: scope}
			saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
			decodeNap(t) // prints 1 and 2
			saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
			napSendSources(t, st, scope, hexIDList(1, 2)...)
			mover, err := runNapReport(st.Dir, scope)
			if err != nil {
				t.Fatal(err)
			}
			other, err := runNapReport(st.Dir, scope) // started from the same record, and finds the window already moved
			if err != nil {
				t.Fatal(err)
			}
			if mover.LastNap == nil || other.LastNap != nil {
				t.Fatalf("the run that wrote the window counts the conversations read, and the other, which found the window moved, counts none: %v %v", mover.LastNap, other.LastNap)
			}
			order := []napReport{mover, other}
			if !moverFirst {
				order = []napReport{other, mover}
			}
			counted := 0
			for _, r := range order {
				out := capture(t, func() error { return finishNap(st.Dir, r) })
				if last := napLastNapOf(t, out); last != nil {
					counted++
					requireLastNap(t, out, 2, 1, 1)
				}
			}
			if counted != 1 {
				t.Fatalf("the 2 conversations are counted in the output of %d runs, once is right", counted)
			}
			rows := napLogRows(t, st)
			if len(rows) != 1 {
				t.Fatalf("the log has one row for the 2 conversations: %v", rows)
			}
			requireNapLogRow(t, rows[0], 2, 1, 1, "0000000000000002")
			if got, want := napStatusLine(t, st), "nap: conversations read while no article cited them: 1, of which an article cites now: 1 (runs: 1)"; got != want {
				t.Fatalf("status line:\n got %q\nwant %q", got, want)
			}
		})
	}
}

// The window moves before the output is printed. When the output cannot be printed, the conversations the window moved over are counted
// all the same, once: the next run finds the window moved and counts nothing again.
func TestNapCountsWhatTheWindowMovedOverEvenWhenTheOutputCouldNotBePrinted(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	decodeNap(t)
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	old := stdout
	stdout = napBrokenPipe{}
	err := cmdNap(nil)
	stdout = old
	if err == nil {
		t.Fatal("premise: the output could not be printed")
	}
	if rows := napLogRows(t, st); len(rows) != 1 {
		t.Fatalf("the window moved over the 2 conversations, so they are counted: %v", rows)
	}
	_, out := decodeNapOutput(t)
	if napLastNapOf(t, out) != nil {
		t.Fatalf("the next run counts nothing again: %s", out)
	}
	if rows := napLogRows(t, st); len(rows) != 1 {
		t.Fatalf("still one row: %v", rows)
	}
}

// (3) A conversation printed again, because an earlier one was not read, keeps what it was when it was first printed: an article that was
// saved from it in between is what the run caught, and it is counted when the window moves over the conversation.
func TestNapCountsWhatAnArticleCaughtFromAConversationPrintedAgain(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-3*time.Hour), "second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-2*time.Hour), "third")
	decodeNap(t) // 1 (cited) 2 3 are printed
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, "0000000000000003") // 1 is not sent
	again := decodeNap(t)
	if fmt.Sprint(conversationIDs(again)) != hexIDs(1, 3) {
		t.Fatalf("premise: 1 was not read, so 1..3 are printed again: %v", conversationIDs(again))
	}
	if got := lessonsOf(again); len(got["0000000000000002"]) == 0 {
		t.Fatalf("premise: the article now cites 2: %v", got)
	}
	napSendSources(t, st, scope, hexIDList(1, 3)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 3, 2, 1) // 2 was printed with no article citing it, and an article cites it now
}

// A conversation first printed cited stays cited when it is printed again, whatever the articles do in between.
func TestNapKeepsACitedConversationCitedWhenItIsPrintedAgain(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	first := decodeNap(t)
	if got := lessonsOf(first); len(got["0000000000000001"]) == 0 {
		t.Fatalf("premise: 1 is cited when printed: %v", got)
	}
	decodeNap(t) // nobody read: printed again
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 0)
}

// A record from before whether an article cited a conversation was kept says nothing of the first print: the state of the print that
// replaces it is what counts.
func TestNapTakesTheStateOfTheNewPrintWhenTheOldRecordSaysNone(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	decodeNap(t)
	path := napShownPath(st.Dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var old []string
	for i, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if i > 0 {
			line = line[:strings.LastIndex(line, " ")] // "id offset hash", the form before the state was kept
		}
		old = append(old, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(old, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decodeNap(t) // nobody read: printed again, with a record of its own
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 0)
}

// (4) .shikii/nap-log holds one line a run. A last line that was not ended (a run stopped in the middle of writing it) is not joined to the
// line of the next run: the broken line is the only one lost.
func TestNapLogLineAfterAnUnfinishedLineIsKept(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	if err := os.WriteFile(filepath.Join(st.Dir, "nap-log"), []byte(`{"at":"2026-11-25T00:00:00Z","shown":1`), 0o600); err != nil {
		t.Fatal(err)
	}
	decodeNap(t)
	napSendSources(t, st, scope, "0000000000000001")
	decodeNap(t)
	if got := napStatusLine(t, st); !strings.Contains(got, "(runs: 1)") {
		t.Fatalf("the run that moved the window is counted in status: %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(st.Dir, "nap-log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"); len(lines) != 2 || !strings.HasPrefix(lines[0], `{"at":"2026-11-25T00:00:00Z","shown":1`) || !strings.HasPrefix(lines[1], `{"at":`) || !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("the broken line stays as it was, the new line is on its own:\n%q", raw)
	}
}

// A last line that is a whole row but has no newline yet is a row: it is kept, and counted with the row after it.
func TestNapLogCountsAWholeRowThatHadNoNewlineAndTheRowAfterIt(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	if err := os.WriteFile(filepath.Join(st.Dir, "nap-log"), []byte(`{"at":"2026-11-25T00:00:00Z","shown":2,"uncited":1,"picked_up":1,"picked_up_ids":["x"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	decodeNap(t)
	napSendSources(t, st, scope, "0000000000000001")
	decodeNap(t)
	if got, want := napStatusLine(t, st), "nap: conversations read while no article cited them: 1, of which an article cites now: 1 (runs: 2)"; got != want {
		t.Fatalf("both rows are counted:\n got %q\nwant %q", got, want)
	}
}

// The status line sums the lines it can read: a line that is broken, in the middle of the log, is skipped and the lines around it count.
func TestNapStatusSkipsOnlyTheBrokenLinesOfTheLog(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	good := `{"at":"2026-11-25T00:00:00Z","shown":2,"uncited":1,"picked_up":1,"picked_up_ids":["x"]}`
	text := good + "\n" + `{"at":"2026-11-25T00:00:00Z","shown":1` + "\n" + "not json at all\n" + "\n" + `{"shown":0}` + "\n" + good + "\n"
	if err := os.WriteFile(filepath.Join(st.Dir, "nap-log"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := napStatusLine(t, st), "nap: conversations read while no article cited them: 2, of which an article cites now: 2 (runs: 2)"; got != want {
		t.Fatalf("the two good lines are counted, the others skipped:\n got %q\nwant %q", got, want)
	}
}

// (6) A run that prints no conversation still says so when .shikii/nap-shown cannot be used (a link, a pipe, a folder): the guide says the
// error tells the person to remove the file, whatever the run printed. The file is never written through.
func TestNapSaysToRemoveAnUnusableShownRecordEvenWhenNoConversationIsPrinted(t *testing.T) {
	for _, kind := range []string{"a hard link", "a symbolic link", "a named pipe", "a folder"} {
		t.Run(kind, func(t *testing.T) {
			st, scope, _, _, _ := memoryEffectFixture(t)
			if err := os.Remove(st.FeedbackFile(scope)); err != nil {
				t.Fatal(err)
			}
			path := napShownPath(st.Dir)
			other := ""
			switch kind {
			case "a hard link":
				other = linkToAPersonsFile(t, path, napPersonsFile)
			case "a symbolic link":
				other = filepath.Join(t.TempDir(), "persons-file")
				if err := os.WriteFile(other, []byte(napPersonsFile), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			case "a named pipe":
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "a folder":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			r, _, err := napPrintedRun(t, st)
			if len(r.Conversations) != 0 {
				t.Fatalf("premise: no conversation is printed: %v", conversationIDs(r))
			}
			if err == nil || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), napShownName) {
				t.Fatalf("the error tells the person to remove .shikii/nap-shown: %v", err)
			}
			if other != "" {
				requireFileHolds(t, other, napPersonsFile)
			}
		})
	}
}

// A shown record that is not there, or is an ordinary file, is no error when nothing is printed.
func TestNapSaysNothingWhenNoConversationIsPrintedAndTheShownRecordIsUsable(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	if err := os.Remove(st.FeedbackFile(scope)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := napPrintedRun(t, st); err != nil {
		t.Fatalf("no record of what was shown: %v", err)
	}
	if err := os.WriteFile(napShownPath(st.Dir), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := napPrintedRun(t, st); err != nil {
		t.Fatalf("an empty ordinary file: %v", err)
	}
}
