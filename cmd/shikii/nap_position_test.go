package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
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

// The 24,000 bytes bound the whole report a run prints, not only its conversations: next_action and the articles whose premise changed
// (up to 32, each with a title of up to 200 bytes that JSON spells out at up to six bytes a character) come before the conversations
// and count toward it. The articles are taken first while the report fits, then the conversations take the room that is left, and a run
// that fits neither still prints one item, so it always moves on.

// napPremiseListOf is n articles whose premise changed, in id order, with the title title(i).
func napPremiseListOf(n int, title func(i int) string) []napPremiseChanged {
	list := make([]napPremiseChanged, n)
	for i := range list {
		list[i] = napPremiseChanged{ID: fmt.Sprintf("%016x", i+1), Revision: "0123456789abcdef", Title: title(i)}
	}
	return list
}

// napConversationsOf is n conversations in the order they were written, whose three texts are made by text(limit) with the most bytes
// the field keeps.
func napConversationsOf(n int, text func(limit int) string) []napConversation {
	convs := make([]napConversation, n)
	for i := range convs {
		convs[i] = napConversation{ID: fmt.Sprintf("%016x", i+1), At: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC), Session: "s", Request: text(napRequestLimit), Summary: text(napSummaryLimit), Reply: text(napReplyLimit)}
	}
	return convs
}

var (
	napControlTitle = func(int) string { return strings.Repeat("\x01", 200) } // 200 bytes that print as 1,200
	napQuoteTitle   = func(int) string { return strings.Repeat("a\"", 100) }  // 200 bytes that print as 300
	napAsciiTitle   = func(int) string { return strings.Repeat("t", 200) }
	napNoTitle      = func(int) string { return "" }
	napSmallText    = func(int) string { return "small" }
	napAsciiText    = func(limit int) string { return strings.Repeat("x", limit) }
	napControlText  = func(limit int) string { return strings.Repeat("\x1b", limit) } // a terminal's colour codes pasted in
)

// napReportSize is the size of the report with the first j articles of premise and the first k conversations of convs.
func napReportSize(base napReport, premise []napPremiseChanged, convs []napConversation, j, k int) int {
	base.PremiseChanged, base.PremiseChangedMore = premise[:j], len(premise)-j
	base.Conversations, base.NewerLeft = convs[:k], len(convs)-k
	return napEncodedLen(base)
}

// Audit 0.42.0 (second confirmation), item A, first case: 32 articles with control characters in the title (200 bytes each is allowed)
// made the report 43,455 bytes with one small conversation. The articles that fit are listed, the rest counted.
func TestNapRunStaysWithin24000BytesWhen32ArticlesWithControlCharacterTitlesAndOneSmallConversationAreWaiting(t *testing.T) {
	premise := napPremiseListOf(32, napControlTitle)
	r := napReport{NextAction: napNextAction, PremiseChanged: premise}
	n := fitNapReport(&r, napConversationsOf(1, napSmallText), 24000, napMaxConvs)
	if got := napEncodedLen(r); got > 24000 {
		t.Fatalf("one small conversation, %d bytes printed, more than 24000 (%d articles, %d conversations)", got, len(r.PremiseChanged), n)
	}
	if len(r.PremiseChanged) < 1 || len(r.PremiseChanged) >= 32 || len(r.PremiseChanged)+r.PremiseChangedMore != 32 {
		t.Fatalf("the articles that fit are listed from the front and the rest counted: %d listed, %d more", len(r.PremiseChanged), r.PremiseChangedMore)
	}
	if got, want := r.PremiseChanged[len(r.PremiseChanged)-1].ID, premise[len(r.PremiseChanged)-1].ID; got != want {
		t.Fatalf("the articles are taken in their order: the last is %s, not %s", got, want)
	}
}

// Audit 0.42.0 (second confirmation), item A, second case: 32 ordinary titles with quotes and one conversation full of terminal escape
// codes made it 30,848 bytes. The articles fit; the conversation, that alone is 16,000 bytes, does not fit what is left.
func TestNapRunStaysWithin24000BytesWhen32ArticlesAndOneConversationOfEscapeCodesAreWaiting(t *testing.T) {
	premise := napPremiseListOf(32, napQuoteTitle)
	r := napReport{NextAction: napNextAction, PremiseChanged: premise}
	convs := napConversationsOf(1, napControlText)
	n := fitNapReport(&r, convs, 24000, napMaxConvs)
	if got := napEncodedLen(r); got > 24000 {
		t.Fatalf("one conversation of escape codes, %d bytes printed, more than 24000 (%d articles, %d conversations)", got, len(r.PremiseChanged), n)
	}
	if len(r.PremiseChanged) != 32 || r.PremiseChangedMore != 0 || n != 0 || len(r.Conversations) != 0 || r.NewerLeft != 1 {
		t.Fatalf("the 32 articles fit and come first, the conversation is left for a run that has the room: %d articles, %d more, %d conversations, %d left", len(r.PremiseChanged), r.PremiseChangedMore, n, r.NewerLeft)
	}
}

// The articles are taken from the front while the report, with no conversation yet, stays within the bound; the conversations then take
// what room is left; what is left out is counted (the articles past the cap of 32 are counted already).
func TestNapFitTakesTheArticlesWhosePremiseChangedWhileTheReportFitsAndThenTheConversations(t *testing.T) {
	premise := napPremiseListOf(32, napControlTitle)
	big := napConversationsOf(5, napAsciiText)
	small := napConversationsOf(5, napSmallText)
	base := napReport{NextAction: napNextAction}
	fit := func(more int, convs []napConversation, bound int) (napReport, int) {
		r := base
		r.PremiseChanged, r.PremiseChangedMore = premise, more
		n := fitNapReport(&r, convs, bound, napMaxConvs)
		return r, n
	}
	// sizeWith is the size of the report with the first j articles and no conversation, when `more` articles were already left out.
	sizeWith := func(more, j int) int {
		r := base
		r.PremiseChanged, r.PremiseChangedMore = premise[:j], more+len(premise)-j
		r.Conversations, r.NewerLeft = big[:0], len(big)
		return napEncodedLen(r)
	}
	for _, more := range []int{0, 8} { // 8: the cap of 32 left eight out before
		for _, j := range []int{1, 7, 16, 31, 32} {
			bound := sizeWith(more, j)
			r, n := fit(more, big, bound)
			if len(r.PremiseChanged) != j || r.PremiseChangedMore != more+32-j || n != 0 || len(r.Conversations) != 0 || r.NewerLeft != 5 {
				t.Fatalf("more %d, room for exactly %d articles: %d listed, %d more, %d conversations, %d left", more, j, len(r.PremiseChanged), r.PremiseChangedMore, n, r.NewerLeft)
			}
			if r.PremiseChanged[j-1].ID != premise[j-1].ID {
				t.Fatalf("more %d, %d articles: the last listed is %s, not %s", more, j, r.PremiseChanged[j-1].ID, premise[j-1].ID)
			}
			if j > 1 {
				r, n = fit(more, big, bound-1)
				if len(r.PremiseChanged) != j-1 || r.PremiseChangedMore != more+32-(j-1) || n != 0 {
					t.Fatalf("more %d, one byte less than %d articles takes %d: %d listed, %d more, %d conversations", more, j, j-1, len(r.PremiseChanged), r.PremiseChangedMore, n)
				}
			}
		}
	}
	// The articles come first: with room for 7 of them and one small conversation, the 7 are listed and the conversation takes what is left.
	bound := napReportSize(base, premise, small, 7, 1)
	r, n := fit(0, small, bound)
	if len(r.PremiseChanged) != 7 || n != 1 || len(r.Conversations) != 1 || r.NewerLeft != 4 || napEncodedLen(r) != bound {
		t.Fatalf("7 articles and one conversation: %d articles, %d conversations, %d left, %d bytes of %d", len(r.PremiseChanged), n, r.NewerLeft, napEncodedLen(r), bound)
	}
}

// When not even one article or one conversation fits, one item is still printed (the first article, else the first conversation), so a
// run moves on; and with nothing to print the lists are empty.
func TestNapFitPrintsExactlyOneItemWhenNothingFits(t *testing.T) {
	premise := napPremiseListOf(3, napControlTitle)
	convs := napConversationsOf(4, napAsciiText)
	r := napReport{NextAction: napNextAction, PremiseChanged: premise, PremiseChangedMore: 5}
	if n := fitNapReport(&r, convs, 1, napMaxConvs); n != 0 || len(r.PremiseChanged) != 1 || r.PremiseChanged[0].ID != premise[0].ID || r.PremiseChangedMore != 7 || len(r.Conversations) != 0 || r.NewerLeft != 4 {
		t.Fatalf("with articles waiting, the first article only: %d conversations, %d articles, %d more, %d left", n, len(r.PremiseChanged), r.PremiseChangedMore, r.NewerLeft)
	}
	r = napReport{NextAction: napNextAction}
	if n := fitNapReport(&r, convs, 1, napMaxConvs); n != 1 || len(r.PremiseChanged) != 0 || len(r.Conversations) != 1 || r.Conversations[0].ID != convs[0].ID || r.NewerLeft != 3 {
		t.Fatalf("with no article, the first conversation only: %d, %d articles, %d conversations, %d left", n, len(r.PremiseChanged), len(r.Conversations), r.NewerLeft)
	}
	r = napReport{NextAction: napNextAction}
	if n := fitNapReport(&r, nil, 1, napMaxConvs); n != 0 || len(r.PremiseChanged) != 0 || r.Conversations == nil || len(r.Conversations) != 0 || r.NewerLeft != 0 {
		t.Fatalf("with nothing to print, nothing: %d, %d articles, %v", n, len(r.PremiseChanged), r.Conversations)
	}
}

// Whatever the titles and the texts are, a run prints at most 24,000 bytes, except when the report with one item alone is larger (no
// title or text a record keeps is that large, so here none is); it prints something whenever something waits, takes the front ones in
// order, and counts what it left out.
func TestNapFitNeverPrintsMoreThan24000BytesWhateverTheTitlesAndTextsAreAndLosesNothing(t *testing.T) {
	titles := map[string]func(int) string{"no title": napNoTitle, "ascii": napAsciiTitle, "quotes": napQuoteTitle, "control characters": napControlTitle}
	texts := map[string]func(int) string{"small": napSmallText, "ascii": napAsciiText, "escape codes": napControlText}
	for titleName, title := range titles {
		for textName, text := range texts {
			for _, articles := range []int{0, 1, 10, 32} {
				for _, waiting := range []int{0, 1, 3, 64, 100} {
					premise := napPremiseListOf(articles, title)
					convs := napConversationsOf(waiting, text)
					r := napReport{NextAction: napNextAction, PremiseChanged: premise, PremiseChangedMore: 4}
					n := fitNapReport(&r, convs, 24000, napMaxConvs)
					name := fmt.Sprintf("%d articles with %s titles, %d conversations of %s text", articles, titleName, waiting, textName)
					items := len(r.PremiseChanged) + len(r.Conversations)
					if got := napEncodedLen(r); got > 24000 && items != 1 {
						t.Fatalf("%s: %d bytes with %d articles and %d conversations, more than 24000", name, got, len(r.PremiseChanged), len(r.Conversations))
					}
					if n != len(r.Conversations) || len(r.PremiseChanged)+r.PremiseChangedMore != articles+4 || len(r.Conversations)+r.NewerLeft != waiting {
						t.Fatalf("%s: what is left out is counted: %d articles + %d more, %d conversations + %d left", name, len(r.PremiseChanged), r.PremiseChangedMore, len(r.Conversations), r.NewerLeft)
					}
					if (articles > 0 || waiting > 0) && items == 0 {
						t.Fatalf("%s: something waits but nothing is printed, so the run would never move on", name)
					}
					for i, p := range r.PremiseChanged {
						if p.ID != premise[i].ID {
							t.Fatalf("%s: article %d is %s, not the one at that place in the order", name, i, p.ID)
						}
					}
					for i, c := range r.Conversations {
						if c.ID != convs[i].ID {
							t.Fatalf("%s: conversation %d is %s, not the one at that place in the order", name, i, c.ID)
						}
					}
				}
			}
		}
	}
}

// Through the command: 32 articles whose premise changed are more than a run prints, so it prints the articles that fit, no conversation
// (they take what the articles leave and nothing is left), counts them all in newer_left, and leaves the place where it was; once the
// articles are saved again (here: the file is as it was) the same conversations are printed.
func TestNapRunWithMoreChangedPremisesThanFitListsArticlesUpToTheLimitShowsNoConversationAndKeepsThePlace(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	for i := 0; i < 32; i++ {
		watchedNapArticle(t, book, draft, fmt.Sprintf("%s%010d", strings.Repeat("\x01", 190), i), "shared.txt", "before")
	}
	if r, _, err := napPrintedRun(t, st); err != nil || fmt.Sprint(conversationIDs(r)) != hexIDs(1, 1) || len(r.PremiseChanged) != 0 {
		t.Fatalf("premise: the first run shows the fixture's conversation, no premise has changed yet: %v %v %d", conversationIDs(r), err, len(r.PremiseChanged))
	}
	place := readNapWindowEndFile(t, st)
	for i := 2; i <= 4; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), time.Now().Add(-time.Hour), strings.Repeat("x", store.FeedbackTextLimit))
	}
	if err := os.WriteFile(filepath.Join(scope, "shared.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, out, err := napPrintedRun(t, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 24000 {
		t.Fatalf("the run printed %d bytes, more than 24000 (%d articles listed)", len(out), len(r.PremiseChanged))
	}
	if len(r.PremiseChanged) == 0 || len(r.PremiseChanged) >= 32 || len(r.PremiseChanged)+r.PremiseChangedMore != 32 {
		t.Fatalf("articles up to the limit are listed and the rest counted: %d listed, %d more", len(r.PremiseChanged), r.PremiseChangedMore)
	}
	if len(r.Conversations) != 0 || r.NewerLeft != 3 {
		t.Fatalf("no conversation fits, and all 3 waiting are counted in newer_left: %v, newer_left %d", conversationIDs(r), r.NewerLeft)
	}
	if got := readNapWindowEndFile(t, st); got != place {
		t.Fatalf("with no conversation shown the place does not move:\n%q\n%q", place, got)
	}
	if err := os.WriteFile(filepath.Join(scope, "shared.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _, err = napPrintedRun(t, st)
	if err != nil || len(r.PremiseChanged) != 0 || fmt.Sprint(conversationIDs(r)) != hexIDs(2, 4) || r.NewerLeft != 0 {
		t.Fatalf("with no premise changed the 3 waiting conversations are shown: %v, %d articles, newer_left %d, %v", conversationIDs(r), len(r.PremiseChanged), r.NewerLeft, err)
	}
	if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 4); got != want {
		t.Fatalf("the place is the end of the last conversation shown:\n%q\n%q", want, got)
	}
}
