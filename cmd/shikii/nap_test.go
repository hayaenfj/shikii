package main

import (
	"encoding/json"
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

// Memory nap: SessionStart of the person's own session tells the agent once a nap is due (24 hours and 5 saved work
// results since the last notice), rewrites .shikii/nap as the mark, and reopens a completed memory upgrade. No test starts an
// agent or a server; the clock is the real one, so each test sets the times of the file and of the results relative to now.

const napMarker = "it is time for a memory nap"

func napStatePath(st store.Store) string { return filepath.Join(st.Dir, "nap") }

// writeNapState writes .shikii/nap with one time per line (the last notice first).
func writeNapState(t *testing.T, st store.Store, times ...time.Time) string {
	t.Helper()
	var lines []string
	for _, at := range times {
		lines = append(lines, at.UTC().Format(time.RFC3339))
	}
	text := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(napStatePath(st), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return text
}

func readNapStateFile(t *testing.T, st store.Store) string {
	t.Helper()
	b, err := os.ReadFile(napStatePath(st))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// saveWorkResults saves n work results observed at the given time (the first one is numbered from, so two calls do not repeat one).
func saveWorkResults(t *testing.T, st store.Store, scope string, from, n int, observed time.Time) {
	t.Helper()
	book := notebook.Book{Root: st.Dir, Scope: scope}
	for i := from; i < from+n; i++ {
		if _, err := book.CaptureWork(notebook.WorkResult(), fmt.Sprintf(`{"changed":"result %d"}`, i), "work", "test", observed); err != nil {
			t.Fatal(err)
		}
	}
}

func startSession(t *testing.T, scope string, extra map[string]any) string {
	t.Helper()
	in := map[string]any{"session_id": "s-" + t.Name(), "cwd": scope, "hook_event_name": "SessionStart", "source": "startup"}
	for k, v := range extra {
		in[k] = v
	}
	return runHook(t, "session-start", in)
}

// less than 24 hours after the last notice: nothing is said and the file is not touched, however many results there are.
func TestNapNoticeDoesNotComeBefore24HoursHavePassed(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	written := writeNapState(t, st, now.Add(-23*time.Hour))
	saveWorkResults(t, st, scope, 0, 8, now.Add(-time.Hour))
	if out := startSession(t, scope, nil); out != "" {
		t.Fatalf("23 hours after the last notice, nothing is said:\n%s", out)
	}
	if got := readNapStateFile(t, st); got != written {
		t.Fatalf("the file is not rewritten when nothing is said:\n%q\n%q", written, got)
	}
}

// 24 hours have passed but only 4 results were saved since: nothing is said. Results observed before the last notice do not count.
func TestNapNoticeDoesNotComeWithFourWorkResults(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	written := writeNapState(t, st, now.Add(-25*time.Hour))
	saveWorkResults(t, st, scope, 0, 4, now.Add(-time.Hour))
	saveWorkResults(t, st, scope, 100, 6, now.Add(-30*time.Hour)) // before the last notice
	if out := startSession(t, scope, nil); out != "" {
		t.Fatalf("four results since the last notice, nothing is said:\n%s", out)
	}
	if got := readNapStateFile(t, st); got != written {
		t.Fatalf("the file is not rewritten when nothing is said:\n%q\n%q", written, got)
	}
}

// 5 results since the last notice, 24 hours or more after it: the agent is told in the one paragraph, which stands alone in a project with
// nothing else to say, and the file becomes "now, then the last time".
func TestNapNoticeComesWithFiveWorkResultsAndRewritesTheFile(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	last := now.Add(-25 * time.Hour)
	writeNapState(t, st, last, now.Add(-80*time.Hour))
	saveWorkResults(t, st, scope, 0, 5, now.Add(-time.Hour))
	out := startSession(t, scope, nil)
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	spec, _ := v["hookSpecificOutput"].(map[string]any)
	ctx, _ := spec["additionalContext"].(string)
	for _, want := range []string{napMarker, "5 work results since " + last.UTC().Format(time.RFC3339), "`shikii nap`", "next_action", "background subagent"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("the paragraph names %q:\n%s", want, ctx)
		}
	}
	if spec["hookEventName"] != "SessionStart" || len(v) != 1 {
		t.Fatalf("the output only adds context to SessionStart:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(readNapStateFile(t, st)), "\n")
	if len(lines) != 2 || lines[1] != last.UTC().Format(time.RFC3339) {
		t.Fatalf("the file holds now, then the last time:\n%q", lines)
	}
	if at, err := time.Parse(time.RFC3339, lines[0]); err != nil || time.Since(at) > time.Minute || at.After(time.Now().Add(time.Second)) {
		t.Fatalf("the first line is the time of this notice: %q %v", lines[0], err)
	}
}

// The first time there is no file: every result counts, and the file gets one line.
func TestNapNoticeCountsFromTheStartWhenThereIsNoFile(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveWorkResults(t, st, scope, 0, 5, time.Now().Add(-90*24*time.Hour))
	out := startSession(t, scope, nil)
	if !strings.Contains(out, napMarker) || !strings.Contains(out, "5 work results so far") {
		t.Fatalf("with no file the results count from the start:\n%s", out)
	}
	if lines := strings.Split(strings.TrimSpace(readNapStateFile(t, st)), "\n"); len(lines) != 1 {
		t.Fatalf("the first notice writes one line: %q", lines)
	}
}

// The paragraph is added after the brief the project already gives (the always rules), not instead of it.
func TestNapNoticeFollowsTheBriefOfTheProject(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Scene, always.AppliesTo = "", "Visible text is Japanese", "Every visible sentence is written in Japanese.", "\u3044\u3064\u3082", "every request"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always); err != nil {
		t.Fatal(err)
	}
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	out := startSession(t, scope, nil)
	rule, notice := strings.Index(out, "Every visible sentence is written in Japanese."), strings.Index(out, napMarker)
	if rule < 0 || notice < 0 || rule > notice {
		t.Fatalf("the rules of the brief come first, then the paragraph (%d, %d):\n%s", rule, notice, out)
	}
}

// A second session opened right after the first is not told again: the mark was taken, and the 24 hours start from it.
func TestNapNoticeIsNotRepeatedToTheNextSession(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	if out := startSession(t, scope, map[string]any{"session_id": "first"}); !strings.Contains(out, napMarker) {
		t.Fatalf("the first session is told:\n%s", out)
	}
	taken := readNapStateFile(t, st)
	saveWorkResults(t, st, scope, 50, 6, time.Now()) // even with more results since
	if out := startSession(t, scope, map[string]any{"session_id": "second"}); out != "" {
		t.Fatalf("the second session is not told again:\n%s", out)
	}
	if got := readNapStateFile(t, st); got != taken {
		t.Fatalf("the second session leaves the mark as the first took it:\n%q\n%q", taken, got)
	}
}

// A subagent's SessionStart carries an agent_id: it is told nothing and takes no mark, so the person's own session still is.
func TestNapNoticeIsNotGivenToASubagent(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	written := writeNapState(t, st, time.Now().Add(-25*time.Hour))
	saveWorkResults(t, st, scope, 0, 5, time.Now().Add(-time.Hour))
	if out := startSession(t, scope, map[string]any{"agent_id": "sub-1", "agent_type": "general-purpose"}); out != "" {
		t.Fatalf("a subagent is told nothing:\n%s", out)
	}
	if got := readNapStateFile(t, st); got != written {
		t.Fatalf("a subagent takes no mark:\n%q\n%q", written, got)
	}
	if out := startSession(t, scope, nil); !strings.Contains(out, napMarker) {
		t.Fatalf("the person's own session is still told after the subagent's:\n%s", out)
	}
}

// The mark is taken under the file's lock and only while the file holds what the notice read: a session that finds the lock held, or the
// file already rewritten, says nothing and writes nothing.
func TestNapMarkIsNotTakenWhileLockedOrAfterAnotherSessionTookIt(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	read := writeNapState(t, st, time.Now().Add(-25*time.Hour))
	f, err := os.OpenFile(napStatePath(st), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if ok, err := takeNapMark(st.Dir, read, "now\n"); ok || err != nil {
		t.Fatalf("with the lock held by another session nothing is taken: %v %v", ok, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if ok, err := takeNapMark(st.Dir, "another time\n", "now\n"); ok || err != nil {
		t.Fatalf("a file that no longer holds what was read has been rewritten by another session: %v %v", ok, err)
	}
	if got := readNapStateFile(t, st); got != read {
		t.Fatalf("neither refusal wrote anything:\n%q\n%q", read, got)
	}
	if ok, err := takeNapMark(st.Dir, read, "now\n"); !ok || err != nil || readNapStateFile(t, st) != "now\n" {
		t.Fatalf("with the lock free and the file as it was read the mark is taken: %v %v", ok, err)
	}
}

// The notice reopens a completed memory upgrade: the article added after it completed comes back for review, the one reviewed does not.
func TestNapNoticeReopensACompletedMemoryUpgradeForNewArticles(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	u, err := book.MemoryUpgrade()
	if err != nil || len(u.Tasks) != 1 {
		t.Fatal(u, err)
	}
	task := u.Tasks[0]
	if _, err := book.Maintain(notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Reason: "Checked current policy and original evidence."}); err != nil {
		t.Fatal(err)
	}
	if u, err = book.MemoryUpgrade(); err != nil || u.Status != "complete" {
		t.Fatal(u, err)
	}
	added := draft
	added.ID, added.Title, added.Lesson = "", "A rule written after the upgrade", "Check the guide as well when the README changes."
	page, err := book.Ingest(added)
	if err != nil {
		t.Fatal(err)
	}
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	if out := startSession(t, scope, nil); !strings.Contains(out, napMarker) {
		t.Fatalf("the notice comes:\n%s", out)
	}
	u, err = book.MemoryUpgrade()
	if err != nil || u.Status != "upgrading" || u.Total != 2 || u.Reviewed != 1 || len(u.Tasks) != 1 || u.Tasks[0].PageID != page.Page.ID {
		t.Fatalf("only the new article is back for review: %+v %v", u, err)
	}
}

func decodeNap(t *testing.T) napReport {
	t.Helper()
	out := capture(t, func() error { return cmdNap(nil) })
	var r napReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	return r
}

func saveConversation(t *testing.T, st store.Store, scope, id string, at time.Time, reply string) {
	t.Helper()
	if err := st.AppendFeedback(scope, store.Feedback{ID: id, Request: "request " + id, Summary: "summary " + id, Reply: reply, At: at, Session: "session-" + id}); err != nil {
		t.Fatal(err)
	}
}

func conversationIDs(r napReport) []string {
	ids := []string{}
	for _, c := range r.Conversations {
		ids = append(ids, c.ID)
	}
	return ids
}

func napWindowEndFilePath(st store.Store) string { return filepath.Join(st.Dir, "nap-window-end") }

// writeNapWindowEnd writes .shikii/nap-window-end as `shikii nap` does: the end of the last window it printed, one time.
func writeNapWindowEnd(t *testing.T, st store.Store, at time.Time) string {
	t.Helper()
	text := at.UTC().Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(napWindowEndFilePath(st), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return text
}

func readNapWindowEndFile(t *testing.T, st store.Store) string {
	t.Helper()
	b, err := os.ReadFile(napWindowEndFilePath(st))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// napRunAt is what `shikii nap` does with the clock set: the window is built at now, and its end is recorded.
func napRunAt(t *testing.T, st store.Store, scope string, now time.Time) napReport {
	t.Helper()
	r, err := buildNapReport(st.Dir, scope, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordNapWindowEnd(st.Dir, r); err != nil {
		t.Fatal(err)
	}
	return r
}

func parseNapTime(t *testing.T, text string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(text))
	if err != nil {
		t.Fatalf("not a time: %q %v", text, err)
	}
	return at
}

func hexIDs(from, to int) string {
	ids := []string{}
	for i := from; i <= to; i++ {
		ids = append(ids, fmt.Sprintf("%016x", i))
	}
	return fmt.Sprint(ids)
}

// shikii nap lists the conversations after the end of the last window it printed, up to now, oldest first, and nothing outside them. The
// notice's file (.shikii/nap) does not decide the window and is not touched by the command.
func TestNapListsOnlyTheConversationsAfterTheEndOfTheLastWindow(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	end := now.Add(-72 * time.Hour)
	saveConversation(t, st, scope, "0000000000000001", now.Add(-100*time.Hour), "before the last window ended")
	saveConversation(t, st, scope, "0000000000000002", now.Add(-60*time.Hour), "after it, older")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-30*time.Hour), "after it, newer")
	saveConversation(t, st, scope, "0000000000000004", now.Add(-2*time.Hour), "after it, the newest")
	saveConversation(t, st, scope, "0000000000000005", now.Add(2*time.Hour), "not yet: later than now")
	writeNapWindowEnd(t, st, end)
	notices := writeNapState(t, st, now.Add(-24*time.Hour), now.Add(-80*time.Hour))
	r := decodeNap(t)
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(2, 4) {
		t.Fatalf("the three after the end of the last window, oldest first, none from the future: %v", got)
	}
	if r.Window.From != end.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("the window starts where the last one ended: %+v", r.Window)
	}
	if to := parseNapTime(t, r.Window.To); to.Before(now) || time.Since(to) > time.Minute {
		t.Fatalf("the window runs up to now: %+v", r.Window)
	}
	if got := readNapWindowEndFile(t, st); got != r.Window.To+"\n" {
		t.Fatalf("the end of the window is recorded once it is printed:\n%q\n%q", r.Window.To, got)
	}
	if got := readNapStateFile(t, st); got != notices {
		t.Fatalf("the notice's file is left as it was:\n%q\n%q", notices, got)
	}
	c := r.Conversations[0]
	if c.Request != "request 0000000000000002" || c.Summary != "summary 0000000000000002" || c.Reply != "after it, older" || c.Session != "session-0000000000000002" || c.At.IsZero() {
		t.Fatalf("each conversation carries its id, time, session and its three texts: %+v", c)
	}
	if r.NextAction != napNextAction || !strings.Contains(r.NextAction, "memory-upgrade") || !strings.Contains(r.NextAction, "Never delete") || !strings.Contains(r.NextAction, "newer_left") {
		t.Fatalf("the next action is given: %q", r.NextAction)
	}
	if r.MemoryUpgrade.Status == "" || r.MemoryUpgrade.Total != 1 {
		t.Fatalf("the progress of the memory upgrade is given: %+v", r.MemoryUpgrade)
	}
	// With no file, or a file that holds no time, the window starts at the beginning (and the file then holds a time).
	for name, prepare := range map[string]func(){
		"no file":             func() { _ = os.Remove(napWindowEndFilePath(st)) },
		"a file with no time": func() { _ = os.WriteFile(napWindowEndFilePath(st), []byte("yesterday\n"), 0o600) },
	} {
		prepare()
		r = decodeNap(t)
		if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(1, 4) || r.Window.From != "" {
			t.Fatalf("%s: every conversation up to now is in the window: %v %+v", name, got, r.Window)
		}
		if got := readNapWindowEndFile(t, st); got != r.Window.To+"\n" {
			t.Fatalf("%s: the end of the window is recorded:\n%q\n%q", name, r.Window.To, got)
		}
	}
}

// The reported failure: the agent was told, did not run shikii nap, and was told again. The next shikii nap shows the conversations of the
// window nobody read as well as the later ones, and the notice does not move where the window starts.
func TestNapShowsTheConversationsOfANoticeThatWasNotActedOn(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	windowEnd := writeNapWindowEnd(t, st, now.Add(-90*time.Hour)) // the last shikii nap showed what came before this
	saveConversation(t, st, scope, "0000000000000001", now.Add(-95*time.Hour), "shown by the last shikii nap")
	saveConversation(t, st, scope, "0000000000000002", now.Add(-70*time.Hour), "before the notice nobody acted on")
	writeNapState(t, st, now.Add(-50*time.Hour), now.Add(-120*time.Hour)) // that notice, and the one before it
	saveConversation(t, st, scope, "0000000000000003", now.Add(-30*time.Hour), "after that notice")
	saveWorkResults(t, st, scope, 0, 5, now.Add(-time.Hour))
	if out := startSession(t, scope, nil); !strings.Contains(out, napMarker) {
		t.Fatalf("the next notice comes:\n%s", out)
	}
	if got := readNapWindowEndFile(t, st); got != windowEnd {
		t.Fatalf("a notice does not move where the window starts:\n%q\n%q", windowEnd, got)
	}
	r := decodeNap(t)
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(2, 3) {
		t.Fatalf("the conversation of the window nobody read is listed with the later one: %v", got)
	}
}

// Run twice in a row, the second shikii nap shows only what was saved after the first one's window, and a third shows nothing.
func TestNapRunTwiceInARowShowsTheSecondOnlyWhatCameAfterTheFirst(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000001", now.Add(-3*time.Hour), "first")
	saveConversation(t, st, scope, "0000000000000002", now.Add(-2*time.Hour), "second")
	first := decodeNap(t) // through the command: it prints, then records
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 2) || first.Window.From != "" {
		t.Fatalf("the first run lists everything: %v %+v", got, first.Window)
	}
	end := parseNapTime(t, readNapWindowEndFile(t, st))
	if !end.Equal(parseNapTime(t, first.Window.To)) {
		t.Fatalf("the recorded end is the printed one: %v %+v", end, first.Window)
	}
	saveConversation(t, st, scope, "0000000000000003", end.Add(time.Minute), "third, after the first window")
	second := napRunAt(t, st, scope, end.Add(time.Hour))
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(3, 3) || second.Window.From != first.Window.To {
		t.Fatalf("the second run lists only what came after the first window: %v %+v", got, second.Window)
	}
	third := napRunAt(t, st, scope, end.Add(2*time.Hour))
	if got := conversationIDs(third); len(got) != 0 || third.Window.From != second.Window.To {
		t.Fatalf("a third run with nothing new lists nothing: %v %+v", got, third.Window)
	}
}

// At most 64 conversations are listed: the oldest of the window, oldest first, the window ends at the 64th and the count of the rest is said,
// so the next shikii nap lists the rest and then nothing.
func TestNapListsTheOldest64AndTheRestComesInTheNextRun(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	at := func(i int) time.Time { return now.Add(-time.Duration(100-i) * time.Minute) }
	for i := 1; i <= 70; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), at(i), "reply")
	}
	first := decodeNap(t)
	if got := conversationIDs(first); len(got) != 64 || fmt.Sprint(got) != hexIDs(1, 64) || first.NewerLeft != 6 {
		t.Fatalf("the oldest 64, oldest first, and 6 left for the next run: %d %v %v", len(got), got, first.NewerLeft)
	}
	if first.Window.To != at(64).UTC().Format(time.RFC3339Nano) || readNapWindowEndFile(t, st) != first.Window.To+"\n" {
		t.Fatalf("the window ends at the 64th conversation and that is recorded: %+v %q", first.Window, readNapWindowEndFile(t, st))
	}
	second := decodeNap(t)
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(65, 70) || second.NewerLeft != 0 || second.Window.From != first.Window.To {
		t.Fatalf("the next run lists the 6 that were left, none twice: %v %+v left %d", got, second.Window, second.NewerLeft)
	}
	if got := conversationIDs(decodeNap(t)); len(got) != 0 {
		t.Fatalf("then nothing is left: %v", got)
	}
}

// Conversations saved at the very time of the 64th go with it: the window ends at that time, so the next one would not list them.
func TestNapKeepsConversationsOfTheSameTimeTogetherAtTheLimit(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	for i := 1; i <= 63; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(100-i)*time.Minute), "reply")
	}
	same := now.Add(-30 * time.Minute)
	saveConversation(t, st, scope, fmt.Sprintf("%016x", 64), same, "reply")
	saveConversation(t, st, scope, fmt.Sprintf("%016x", 65), same, "reply")
	saveConversation(t, st, scope, fmt.Sprintf("%016x", 66), now.Add(-20*time.Minute), "reply")
	first := decodeNap(t)
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 65) || first.NewerLeft != 1 {
		t.Fatalf("the 65th, saved at the time of the 64th, is listed with it: %d %v", len(got), first.NewerLeft)
	}
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(66, 66) {
		t.Fatalf("the next run lists the one that was left: %v", got)
	}
}

// The window is cut by the time of the conversations, not by their place in the file: one saved later in the file but with an earlier time
// (the clock was set back) is listed with the oldest, and is not lost to the cut at 64.
func TestNapCutsAtTheLimitByTimeNotByOrderInTheFile(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	for i := 1; i <= 64; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(100-i)*time.Minute), "reply")
	}
	saveConversation(t, st, scope, fmt.Sprintf("%016x", 65), now.Add(-200*time.Minute), "reply") // the last in the file, the earliest in time
	first := decodeNap(t)
	if got := conversationIDs(first); len(got) != 64 || got[0] != fmt.Sprintf("%016x", 65) || got[63] != fmt.Sprintf("%016x", 63) || first.NewerLeft != 1 {
		t.Fatalf("the earliest in time comes first, and the one left is the latest in time: %d %v %v", len(got), got, first.NewerLeft)
	}
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(64, 64) {
		t.Fatalf("the next run lists the one that was left: %v", got)
	}
}

// The end of the window is written under the file's lock and only while the file holds what the window was read from. A run that finds the
// lock held, or another run's window recorded, writes nothing; its window is then listed again by the next run, never lost.
func TestNapWindowEndIsRecordedUnderTheLockAndOnlyWhileTheFileIsAsRead(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000001", time.Now().Add(-time.Hour), "a reply")
	if err := os.WriteFile(napWindowEndFilePath(st), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(napWindowEndFilePath(st), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(1, 1) {
		t.Fatalf("with the lock held the window is still printed: %v", got)
	}
	if got := readNapWindowEndFile(t, st); got != "" {
		t.Fatalf("with the lock held nothing is recorded: %q", got)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(1, 1) {
		t.Fatalf("the window that was not recorded is listed again: %v", got)
	}
	recorded := readNapWindowEndFile(t, st)
	if recorded == "" {
		t.Fatalf("with the lock free the end is recorded")
	}
	r, err := buildNapReport(st.Dir, scope, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other := writeNapWindowEnd(t, st, time.Now().Add(time.Hour)) // another run records its window between the read and the write
	if err := recordNapWindowEnd(st.Dir, r); err != nil {
		t.Fatal(err)
	}
	if got := readNapWindowEndFile(t, st); got != other {
		t.Fatalf("a file that is no longer as it was read is left to the run that wrote it:\n%q\n%q", other, got)
	}
}

type napBrokenPipe struct{}

func (napBrokenPipe) Write([]byte) (int, error) { return 0, fmt.Errorf("broken pipe") }

// The end is recorded after the window is printed: when nothing could be printed, nothing is recorded and the next run lists it.
func TestNapRecordsNothingWhenTheWindowCouldNotBePrinted(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000001", time.Now().Add(-time.Hour), "a reply")
	old := stdout
	stdout = napBrokenPipe{}
	err := cmdNap(nil)
	stdout = old
	if err == nil || !strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("the failure to print is returned: %v", err)
	}
	if got := readNapWindowEndFile(t, st); got != "" {
		t.Fatalf("nothing was shown, so nothing is recorded: %q", got)
	}
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(1, 1) {
		t.Fatalf("the next run lists the conversation: %v", got)
	}
}

// The guides describe the window the command prints and the file that records its end.
func TestNapDocsDescribeTheWindowAndTheFileThatRecordsItsEnd(t *testing.T) {
	for _, d := range []struct {
		file, heading string
		words         []string
	}{
		{"../../docs/guide.md", "### Memory nap", []string{"`.shikii/nap-window-end`", "`newer_left`", "oldest first, 64 at a time", "starts where the last one stopped"}},
		{"../../docs/guide.ja.md", "### \u8a18\u61b6\u306e\u3072\u3068\u4f11\u307f", []string{"`.shikii/nap-window-end`", "`newer_left`", "\u53e4\u3044\u65b9\u304b\u3089 64 \u4ef6", "\u524d\u56de\u304c\u7d42\u308f\u3063\u305f\u6240\u304b\u3089\u59cb\u307e\u308b"}},
	} {
		text := string(mustRead(t, d.file))
		at := strings.Index(text, d.heading)
		if at < 0 {
			t.Fatalf("%s has no section %q", d.file, d.heading)
		}
		section := text[at+len(d.heading):]
		if end := strings.Index(section, "\n### "); end >= 0 {
			section = section[:end]
		}
		section = strings.Join(strings.Fields(section), " ")
		for _, want := range d.words {
			if !strings.Contains(section, want) {
				t.Errorf("%s: the section on the nap does not say %q", d.file, want)
			}
		}
	}
}

// shikii nap takes no argument and no standard input (an argument is refused with the usage; help prints it).
func TestNapTakesNoArguments(t *testing.T) {
	memoryEffectFixture(t)
	err := cmdNap([]string{"--window", "3"})
	if err == nil || !strings.Contains(err.Error(), "takes no arguments") || !strings.Contains(err.Error(), "usage: shikii nap") {
		t.Fatalf("an argument is refused with the usage: %v", err)
	}
	if out := capture(t, func() error { return cmdNap([]string{"--help"}) }); !strings.HasPrefix(out, "usage: shikii nap") {
		t.Fatalf("help prints the usage: %q", out)
	}
	if !strings.Contains(usageText(), "shikii nap") {
		t.Fatalf("the help of shikii lists the command")
	}
	// Both texts say what the command records and what it counts for the next run, so neither calls it a plain read.
	for name, text := range map[string]string{"the help of shikii": helpEntry(t, "shikii nap"), "the usage of the command": napUsage} {
		if !strings.Contains(text, ".shikii/nap-window-end") || !strings.Contains(text, "newer_left") {
			t.Fatalf("%s names the file that records the end of the window and newer_left: %q", name, text)
		}
	}
}

// The command is reached from the shikii binary's own entry (main), and prints the report for the project it is run in.
func TestNapIsReachedFromMain(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000001", time.Now().Add(-time.Hour), "a reply")
	old := os.Args
	os.Args = []string{"shikii", "nap"}
	out := capture(t, func() error { main(); return nil })
	os.Args = old
	var r napReport
	if err := json.Unmarshal([]byte(out), &r); err != nil || len(r.Conversations) != 1 || r.NextAction == "" {
		t.Fatalf("shikii nap prints the report: %v\n%s", err, out)
	}
}

// The agent reads the report in one tool output: the person's reply is kept longest, the request shorter and the agent's own
// summary shortest, so 64 long conversations stay near 180 KB instead of growing with three equal limits.
func TestNapCutsTheRequestAndSummaryShorterThanTheReply(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	long := strings.Repeat("x", store.FeedbackTextLimit)
	if err := st.AppendFeedback(scope, store.Feedback{ID: "0000000000000001", Request: long, Summary: long, Reply: long, At: time.Now().Add(-time.Hour), Session: "s"}); err != nil {
		t.Fatal(err)
	}
	r := decodeNap(t)
	if len(r.Conversations) != 1 {
		t.Fatalf("one conversation: %d", len(r.Conversations))
	}
	c := r.Conversations[0]
	if len(c.Reply) != 1500 || len(c.Request) != 800 || len(c.Summary) != 400 {
		t.Fatalf("reply 1500, request 800, summary 400 bytes: %d %d %d", len(c.Reply), len(c.Request), len(c.Summary))
	}
}
