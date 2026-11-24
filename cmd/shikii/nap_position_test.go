package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The place `shikii nap` stopped is a position in the record of conversations, which is only ever appended to under its lock: whatever is
// written after a run read the record is after the position, whatever time it carries. These tests run the command the way the agent does
// (build, print, record) and look at which conversations each run shows.

// napPrintedRun is one `shikii nap` through the command: what it printed (decoded and as text) and the error it returned.
func napPrintedRun(t *testing.T, st store.Store) (napReport, string, error) {
	t.Helper()
	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	err := cmdNap(nil)
	stdout = old
	var r napReport
	if buf.Len() > 0 {
		if jerr := json.Unmarshal(buf.Bytes(), &r); jerr != nil {
			t.Fatalf("one JSON object: %v\n%s", jerr, buf.String())
		}
	}
	return r, buf.String(), err
}

// napRunsUntilDone runs the command until a run shows nothing and has nothing left, and returns how many times each conversation was
// shown. It fails when the runs do not end (a position that never moves repeats the same conversations).
func napRunsUntilDone(t *testing.T, st store.Store, maxRuns int) map[string]int {
	t.Helper()
	shown := map[string]int{}
	for run := 0; ; run++ {
		if run == maxRuns {
			t.Fatalf("%d runs did not reach the end: shown so far %d ids, end file %q", maxRuns, len(shown), readNapWindowEndFile(t, st))
		}
		r, out, err := napPrintedRun(t, st)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if len(out) > 24000 && len(r.Conversations) > 1 {
			t.Fatalf("run %d printed %d bytes with %d conversations", run, len(out), len(r.Conversations))
		}
		for _, id := range conversationIDs(r) {
			shown[id]++
		}
		if len(r.Conversations) == 0 && r.NewerLeft == 0 {
			return shown
		}
	}
}

// sortedConversationIDs is the ids of the report in id order, for a check that does not depend on the order of the conversations.
func sortedConversationIDs(r napReport) []string {
	ids := conversationIDs(r)
	sort.Strings(ids)
	return ids
}

// appendRawFeedbackLines appends the lines to the record of conversations exactly as given.
func appendRawFeedbackLines(t *testing.T, st store.Store, scope string, text string) {
	t.Helper()
	f, err := os.OpenFile(st.FeedbackFile(scope), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// F1 of the audit, made deterministic: two sessions end together, the one that took its time first writes second. The run in between
// showed the one written first, whose time is later. The one written after the run is after the position, so the next run shows it.
func TestNapShowsAConversationWrittenAfterTheRunEvenWithAnEarlierTime(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-10*time.Minute), "written first, with the later time")
	first := napRun(t, st, scope)
	if got := sortedConversationIDs(first); fmt.Sprint(got) != hexIDs(1, 2) {
		t.Fatalf("premise: the first run shows what is there: %v", got)
	}
	saveConversation(t, st, scope, "0000000000000003", now.Add(-3*time.Hour), "written after the run, with an earlier time")
	second := napRun(t, st, scope)
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(3, 3) {
		t.Fatalf("the conversation written after the run is shown by the next one: %v", got)
	}
	if got := conversationIDs(napRun(t, st, scope)); len(got) != 0 {
		t.Fatalf("and then nothing is left: %v", got)
	}
}

// The conversations come in the order they were written, not in the order of the times they carry.
func TestNapListsConversationsInTheOrderTheyWereWritten(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-1*time.Hour), "written second")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-2*time.Hour), "written third, an earlier time")
	saveConversation(t, st, scope, "0000000000000004", now.Add(-3*time.Hour), "written fourth, an even earlier time")
	if got := conversationIDs(napRun(t, st, scope)); fmt.Sprint(got) != hexIDs(1, 4) {
		t.Fatalf("file order, oldest written first: %v", got)
	}
}

// F2 of the audit: records without `at` are each shown once and not repeated, however many there are.
func TestNapShowsRecordsWithoutATimeOnceAndDoesNotRepeatThem(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	var raw strings.Builder
	for i := 2; i <= 71; i++ {
		fmt.Fprintf(&raw, `{"id":"%016x","request":"r","summary":"s","reply":"x"}`+"\n", i)
	}
	appendRawFeedbackLines(t, st, scope, raw.String())
	saveConversation(t, st, scope, "00000000000000ff", time.Now().Add(-time.Minute), "a real one after them")
	shown := napRunsUntilDone(t, st, 40)
	if len(shown) != 72 { // the fixture's, the 70 without a time, and the real one
		t.Fatalf("every conversation is shown: %d of 72", len(shown))
	}
	for id, n := range shown {
		if n != 1 {
			t.Fatalf("%s is shown %d times, not once", id, n)
		}
	}
}

// With 300 waiting, runs go on until nothing is left, and every conversation is shown exactly once: the position moves to the end of
// the last conversation a run printed, never past one it left for the next run.
func TestNapShowsEveryConversationExactlyOnceHoweverManyAreWaiting(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	const total = 300
	for i := 2; i <= total; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(total+10-i)*time.Minute), "reply")
	}
	shown := napRunsUntilDone(t, st, 100)
	if len(shown) != total {
		t.Fatalf("every conversation is shown: %d of %d", len(shown), total)
	}
	for id, n := range shown {
		if n != 1 {
			t.Fatalf("%s is shown %d times, not once", id, n)
		}
	}
}

// One run prints at most 24000 bytes, unless the one conversation it must print is larger, and newer_left is exactly the number of
// conversations after the last one printed.
func TestNapRunPrintsAtMost24000BytesAndNewerLeftIsExact(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	long := strings.Repeat("x", store.FeedbackTextLimit)
	const total = 41 // the fixture's conversation and 40 with the most text a conversation keeps
	for i := 2; i <= total; i++ {
		if err := st.AppendFeedback(scope, store.Feedback{ID: fmt.Sprintf("%016x", i), Request: long, Summary: long, Reply: long, At: time.Now().Add(-time.Hour), Session: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	seen := 0
	runs := 0
	for ; seen < total; runs++ {
		if runs > total {
			t.Fatalf("the runs do not end: %d of %d shown", seen, total)
		}
		r, out, err := napPrintedRun(t, st)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > 24000 {
			t.Fatalf("run %d printed %d bytes, more than 24000", runs, len(out))
		}
		if len(r.Conversations) == 0 {
			t.Fatalf("run %d shows nothing with %d left", runs, total-seen)
		}
		seen += len(r.Conversations)
		if r.NewerLeft != total-seen {
			t.Fatalf("run %d: newer_left %d, but %d conversations come after the last one shown", runs, r.NewerLeft, total-seen)
		}
	}
	if runs < 2 {
		t.Fatalf("the 41 conversations do not fit one run: %d runs", runs)
	}
}

// A run that cannot record its place (the file has a second name: a hard link) reads from the beginning each time and refuses to write
// through the link; the error tells the person to remove the file, and the conversations are still printed.
func TestNapHardLinkedEndFileReadsFromTheStartAndTheErrorSaysWhatToDo(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	for i := 2; i <= 3; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), time.Now().Add(-time.Hour), "reply")
	}
	other := linkToAPersonsFile(t, napWindowEndFilePath(st), napPersonsFile)
	for run := 0; run < 2; run++ {
		r, _, err := napPrintedRun(t, st)
		if got := sortedConversationIDs(r); fmt.Sprint(got) != hexIDs(1, 3) {
			t.Fatalf("run %d: every conversation is printed from the start: %v", run, got)
		}
		if err == nil || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), "nap-window-end") {
			t.Fatalf("run %d: the error tells the person to remove .shikii/nap-window-end: %v", run, err)
		}
		requireFileHolds(t, other, napPersonsFile)
	}
}

// A conversation still being written (its record is there, its newline is not) is not taken, and the position does not move past it:
// when its writer ends the line, the next run shows it, even though its time is earlier than the ones already shown.
func TestNapDoesNotMovePastALineStillBeingWritten(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-10*time.Minute), "complete")
	line, err := json.Marshal(store.Feedback{ID: "0000000000000003", Request: "r", Summary: "s", Reply: "being written", At: now.Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	appendRawFeedbackLines(t, st, scope, string(line)) // the whole record, but its newline is not written yet
	if got := sortedConversationIDs(napRun(t, st, scope)); fmt.Sprint(got) != hexIDs(1, 2) {
		t.Fatalf("the complete lines are shown, the partly written one is not: %v", got)
	}
	if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 2); got != want {
		t.Fatalf("the place is the end of the last complete line, not past the line being written:\n%q\n%q", want, got)
	}
	appendRawFeedbackLines(t, st, scope, "\n")
	if got := conversationIDs(napRun(t, st, scope)); fmt.Sprint(got) != hexIDs(3, 3) {
		t.Fatalf("the line, once ended, is shown by the next run: %v", got)
	}
}

// removeFeedbackLines removes the lines of the conversations the way `shikii forget` does: the file is rewritten without them.
func removeFeedbackLines(t *testing.T, st store.Store, scope string, ids ...string) {
	t.Helper()
	drop := func(line []byte) bool {
		for _, id := range ids {
			if strings.Contains(string(line), `"id":"`+id+`"`) {
				return true
			}
		}
		return false
	}
	if n, err := st.RewriteJSONL(st.FeedbackFile(scope), drop, func([][]byte) error { return nil }); err != nil || n != len(ids) {
		t.Fatalf("removed %d lines: %v", n, err)
	}
}

// shikii forget rewrites the record and removes the lines of a finished session. The offset the last run recorded then points somewhere
// else, but the line that ended at it is still in the file: the run goes on after it, so the conversations written since are shown, and
// not shown twice, and the lines removed after the place change nothing.
func TestNapFindsItsPlaceAgainWhenLinesBeforeItWereRemoved(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	for i := 2; i <= 5; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Hour), "before the place")
	}
	if got := conversationIDs(napRun(t, st, scope)); fmt.Sprint(got) != hexIDs(1, 5) {
		t.Fatalf("premise: the first run shows them all: %v", got)
	}
	saveConversation(t, st, scope, "0000000000000006", now.Add(-time.Minute), "after the place")
	saveConversation(t, st, scope, "0000000000000007", now.Add(-time.Minute), "after the place, and removed again")
	removeFeedbackLines(t, st, scope, "0000000000000001", "0000000000000002", "0000000000000003", "0000000000000007")
	r := napRun(t, st, scope)
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(6, 6) {
		t.Fatalf("the run goes on after the line it stopped at, though lines before it are gone: %v", got)
	}
	if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 3); got != want { // 4, 5 and 6 are left
		t.Fatalf("the place is recorded at its new offset:\n%q\n%q", want, got)
	}
	if got := conversationIDs(napRun(t, st, scope)); len(got) != 0 {
		t.Fatalf("and then nothing is left: %v", got)
	}
}

// When no line has the hash of the place any more (the line it stopped at was removed, the file was replaced, or it is shorter than the
// offset), the run starts from the beginning: a conversation may be shown again, none is lost.
func TestNapStartsFromTheBeginningWhenItsLineIsNotInTheRecordAnyMore(t *testing.T) {
	now := time.Now()
	for name, change := range map[string]func(t *testing.T, st store.Store, scope string){
		"the line it stopped at was removed": func(t *testing.T, st store.Store, scope string) {
			removeFeedbackLines(t, st, scope, "0000000000000003")
		},
		"the record was replaced by another": func(t *testing.T, st store.Store, scope string) {
			text := feedbackLineText(t, "00000000000000a1", now) + feedbackLineText(t, "00000000000000a2", now) + feedbackLineText(t, "00000000000000a3", now)
			if err := os.WriteFile(st.FeedbackFile(scope), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"the record is shorter than the offset": func(t *testing.T, st store.Store, scope string) {
			if err := os.WriteFile(st.FeedbackFile(scope), []byte(feedbackLineText(t, "00000000000000b1", now)), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			st, scope, _, _, _ := memoryEffectFixture(t)
			saveConversation(t, st, scope, "0000000000000002", now.Add(-time.Hour), "second")
			saveConversation(t, st, scope, "0000000000000003", now.Add(-time.Hour), "third")
			if got := conversationIDs(napRun(t, st, scope)); fmt.Sprint(got) != hexIDs(1, 3) {
				t.Fatalf("premise: the first run shows them all: %v", got)
			}
			change(t, st, scope)
			raw, err := os.ReadFile(st.FeedbackFile(scope))
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
				if line != "" {
					want++
				}
			}
			r := napRun(t, st, scope)
			if len(r.Conversations) != want || r.NewerLeft != 0 {
				t.Fatalf("every conversation left in the record is shown from the beginning: %d of %d %v", len(r.Conversations), want, conversationIDs(r))
			}
			if got, place := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, want); got != place {
				t.Fatalf("the place is recorded at the end of what was shown:\n%q\n%q", place, got)
			}
		})
	}
}

func feedbackLineText(t *testing.T, id string, at time.Time) string {
	t.Helper()
	raw, err := json.Marshal(store.Feedback{ID: id, Request: "r " + id, Summary: "s", Reply: "x", At: at})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

// The place is two lines, an offset above 0 and the hash of a line in lower-case hex; anything else is the beginning.
func TestNapWindowEndHoldsOneOffsetAndOneHash(t *testing.T) {
	hash := strings.Repeat("ab12", 16)
	for text, want := range map[string]store.FeedbackMark{
		"1234\n" + hash + "\n":                    {Offset: 1234, Hash: hash},
		"1\n" + hash + "\n":                       {Offset: 1, Hash: hash},
		"":                                        {},
		"1234\n" + hash:                           {}, // the last line is not ended
		"1234\r\n" + hash + "\r\n":                {},
		"1234\n" + hash + "\nmore\n":              {},
		"0\n" + hash + "\n":                       {},
		"-5\n" + hash + "\n":                      {},
		"+5\n" + hash + "\n":                      {},
		"05\n" + hash + "\n":                      {},
		"1234\n" + strings.ToUpper(hash) + "\n":   {},
		"1234\n" + hash[:62] + "\n":               {},
		"1234\n" + strings.Repeat("g", 64) + "\n": {},
		"2026-11-24T10:00:00Z\n":                  {},
	} {
		if got := parseNapWindowEnd(text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
		if want.Offset > 0 && formatNapWindowEnd(want) != text {
			t.Errorf("%v is written as %q, not %q", want, formatNapWindowEnd(want), text)
		}
	}
}
