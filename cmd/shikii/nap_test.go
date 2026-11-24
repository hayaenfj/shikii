package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	r, _ := decodeNapOutput(t)
	return r
}

// decodeNapOutput is decodeNap with the text that was printed, for a test of which keys the JSON carries.
func decodeNapOutput(t *testing.T) (napReport, string) {
	t.Helper()
	out := capture(t, func() error { return cmdNap(nil) })
	var r napReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	return r, out
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
	if to := parseNapTime(t, r.Window.To); !to.Equal(now.Add(-2 * time.Hour)) {
		t.Fatalf("the window ends at the time of the last conversation it showed, not at now: %+v", r.Window)
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
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-30*time.Minute), "a reply saved after that window")
	r, err := buildNapReport(st.Dir, scope, time.Now())
	if err != nil || fmt.Sprint(conversationIDs(r)) != hexIDs(2, 2) {
		t.Fatalf("premise: the next window shows the new conversation: %v %v", conversationIDs(r), err)
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

// The guides describe the window the command prints, the file that records its end, the lessons of a conversation and the articles whose
// premise changed.
func TestNapDocsDescribeTheWindowAndTheFileThatRecordsItsEnd(t *testing.T) {
	for _, d := range []struct {
		file, heading string
		words         []string
	}{
		{"../../docs/guide.md", "### Memory nap", []string{"`.shikii/nap-window-end`", "`newer_left`", "oldest first, 64 at a time", "starts where the last one stopped", "`lessons`", "`premise_changed`", "`premise_changed_more`", "up to 32 in id order"}},
		{"../../docs/guide.ja.md", "### \u8a18\u61b6\u306e\u3072\u3068\u4f11\u307f", []string{"`.shikii/nap-window-end`", "`newer_left`", "\u53e4\u3044\u65b9\u304b\u3089 64 \u4ef6", "\u524d\u56de\u304c\u7d42\u308f\u3063\u305f\u6240\u304b\u3089\u59cb\u307e\u308b", "`lessons`", "`premise_changed`", "`premise_changed_more`", "id \u306e\u9806\u306b 32 \u4ef6\u307e\u3067"}},
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
		for _, want := range []string{".shikii/nap-window-end", "newer_left", "lessons", "premise_changed"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s names the file that records the end of the window, newer_left, lessons and premise_changed; it lacks %q: %q", name, want, text)
			}
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

// napDraft is a draft of the fixture's shape under a title of its own (so an id of its own), citing the given conversations.
func napDraft(draft notebook.Draft, title string, sources ...string) notebook.Draft {
	draft.ID, draft.Title, draft.Lesson, draft.Sources = "", title, "Lesson of "+title+".", sources
	return draft
}

func saveNapArticle(t *testing.T, book notebook.Book, d notebook.Draft) *notebook.Page {
	t.Helper()
	saved, err := book.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	return saved.Page
}

// watchedNapArticle saves an article of the fixture's shape that cites the fixture's conversation and watches one project file, which it
// writes with content first.
func watchedNapArticle(t *testing.T, book notebook.Book, draft notebook.Draft, title, file, content string) *notebook.Page {
	t.Helper()
	if err := os.WriteFile(filepath.Join(book.Scope, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	d := napDraft(draft, title, "0000000000000001")
	d.Watch = &[]string{file}
	return saveNapArticle(t, book, d)
}

// lessonsOf is the lessons of each conversation in the report by conversation id (a conversation with none is not in the map).
func lessonsOf(r napReport) map[string][]string {
	by := map[string][]string{}
	for _, c := range r.Conversations {
		if len(c.Lessons) > 0 {
			by[c.ID] = c.Lessons
		}
	}
	return by
}

func sortedIDs(ids ...string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
	return out
}

// Every conversation is still listed; one that an article cites as a source carries that article's id in lessons, so the agent checks the
// article instead of adding the same lesson again, and one nobody cites carries no lessons (the key is not in the JSON at all).
func TestNapNamesTheArticlesThatCiteAConversation(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-3*time.Hour), "cited by two articles")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-2*time.Hour), "cited by no article")
	b := saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	c := saveNapArticle(t, book, napDraft(draft, "Third article", "0000000000000002", "0000000000000001"))
	r, out := decodeNapOutput(t)
	if got := conversationIDs(r); fmt.Sprint(got) != fmt.Sprint([]string{"0000000000000002", "0000000000000003", "0000000000000001"}) {
		t.Fatalf("every conversation is still listed, oldest first (the fixture's is the newest): %v", got)
	}
	want := map[string][]string{
		"0000000000000001": sortedIDs(draft.ID, c.ID),
		"0000000000000002": sortedIDs(b.ID, c.ID),
	}
	if got := lessonsOf(r); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("each conversation names the live articles that cite it, by id, and the one nobody cites names none:\n got %v\nwant %v", got, want)
	}
	var raw struct {
		Conversations []map[string]any `json:"conversations"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	for _, c := range raw.Conversations {
		_, has := c["lessons"]
		if cited := c["id"] != "0000000000000003"; has != cited {
			t.Fatalf("the lessons key is there exactly when an article cites the conversation: %v", c)
		}
	}
}

// An article that was withdrawn, deleted or replaced is out of the wiki's working set, so it does not stand for the conversation.
func TestNapLeavesOutArticlesThatWereWithdrawnDeletedOrReplaced(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	now := time.Now()
	for i := 4; i <= 7; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(i)*time.Hour), "a reply")
	}
	withdrawn := saveNapArticle(t, book, napDraft(draft, "To be withdrawn", "0000000000000004"))
	if _, err := book.Withdraw(notebook.Withdrawal{ID: withdrawn.ID, Revision: withdrawn.Revision, Sources: []string{"0000000000000004"}, Reason: "The work that made it took it back."}); err != nil {
		t.Fatal(err)
	}
	deleted := saveNapArticle(t, book, napDraft(draft, "To be deleted", "0000000000000005"))
	if _, err := book.Manage(notebook.Change{ID: deleted.ID, Revision: deleted.Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	replaced := saveNapArticle(t, book, napDraft(draft, "To be replaced", "0000000000000006"))
	replacement := napDraft(draft, "The replacement", "0000000000000007")
	replacement.Replaces = []string{replaced.ID}
	kept := saveNapArticle(t, book, replacement)
	r := decodeNap(t)
	if got := conversationIDs(r); fmt.Sprint(got) != fmt.Sprint([]string{"0000000000000007", "0000000000000006", "0000000000000005", "0000000000000004", "0000000000000001"}) {
		t.Fatalf("every conversation is still listed, oldest first, whatever became of the articles that cite it: %v", got)
	}
	want := map[string][]string{"0000000000000001": {draft.ID}, "0000000000000007": {kept.ID}}
	if got := lessonsOf(r); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("a withdrawn, a deleted and a replaced article stand for nothing; the replacement does:\n got %v\nwant %v", got, want)
	}
}

// The person's correction counts as a citation too, even when the conversation is not among the article's sources, and an article is named
// once however many ways it cites the conversation.
func TestNapCitationsNameEachArticleOnceFromItsSourcesAndItsCorrection(t *testing.T) {
	const a, b, c = "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"
	pages := []notebook.Page{
		{ID: c, Sources: []notebook.Source{{ID: "0000000000000009"}}, Correction: &store.Correction{SourceID: "0000000000000003"}},
		{ID: b, Sources: []notebook.Source{{ID: "0000000000000001"}, {ID: "0000000000000002"}}},
		{ID: a, Sources: []notebook.Source{{ID: "0000000000000001"}}, Correction: &store.Correction{SourceID: "0000000000000001"}},
	}
	want := map[string][]string{
		"0000000000000001": {a, b}, // a cites it twice, once as a source and once in its correction, and is named once
		"0000000000000002": {b},
		"0000000000000003": {c}, // only the correction
		"0000000000000009": {c},
	}
	if got := napCitations(pages); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("each article once, in id order:\n got %v\nwant %v", got, want)
	}
}

// An article that watches project files and whose files changed (or can no longer be read) is listed first, whatever the memory upgrade
// says; an article whose files are as they were, and one that watches none, are not.
func TestNapListsTheArticlesWhoseWatchedFilesChangedOrAreGone(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	watchedNapArticle(t, book, draft, "Watches a file that stays", "stays.txt", "same")
	changed := watchedNapArticle(t, book, draft, "Watches a file that changes", "changes.txt", "before")
	gone := watchedNapArticle(t, book, draft, "Watches a file that goes", "goes.txt", "before")
	if err := os.WriteFile(filepath.Join(scope, "changes.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(scope, "goes.txt")); err != nil {
		t.Fatal(err)
	}
	r := decodeNap(t)
	want := []napPremiseChanged{{changed.ID, changed.Revision, changed.Title}, {gone.ID, gone.Revision, gone.Title}}
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	if fmt.Sprint(r.PremiseChanged) != fmt.Sprint(want) || r.PremiseChangedMore != 0 {
		t.Fatalf("only the articles whose watched file changed or went, by id, with revision and title (the fixture article and the one whose file stays are not there):\n got %+v\nwant %+v", r.PremiseChanged, want)
	}
}

// Nothing changed: the key is not in the JSON, as the lessons of a conversation nobody cites are not.
func TestNapLeavesOutPremiseChangedWhenNoWatchedFileChanged(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	watchedNapArticle(t, notebook.Book{Root: st.Dir, Scope: scope}, draft, "Watches a file that stays", "stays.txt", "same")
	_, out := decodeNapOutput(t)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"premise_changed", "premise_changed_more"} {
		if _, has := keys[key]; has {
			t.Fatalf("no premise changed, so no %s key:\n%s", key, out)
		}
	}
	if _, has := keys["conversations"]; !has {
		t.Fatalf("premise: the keys of the report are read: %v", keys)
	}
}

// What the memory upgrade has reviewed does not matter: a review keeps the files the article was saved against, so the premise stays
// changed until the article is saved again, and the nap names it every time.
func TestNapListsAChangedPremiseEvenWhenTheMemoryUpgradeReviewedTheArticle(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	changed := watchedNapArticle(t, book, draft, "Watches a file that changes", "changes.txt", "before")
	if err := os.WriteFile(filepath.Join(scope, "changes.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := book.MemoryUpgrade()
	if err != nil || len(u.Tasks) != 2 {
		t.Fatalf("the fixture article and the changed one are up for review: %+v %v", u, err)
	}
	for _, task := range u.Tasks {
		if _, err := book.Maintain(notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Reason: "Checked the file and the article."}); err != nil {
			t.Fatal(err)
		}
	}
	r := decodeNap(t)
	if r.MemoryUpgrade.Status != "complete" {
		t.Fatalf("premise: the memory upgrade has reviewed both: %+v", r.MemoryUpgrade)
	}
	if len(r.PremiseChanged) != 1 || r.PremiseChanged[0].ID != changed.ID {
		t.Fatalf("the reviewed article is named all the same: %+v", r.PremiseChanged)
	}
}

// At most 32 are named, in id order, and the number left out is counted, so a wiki where a large file changed does not fill the output.
func TestNapListsAtMost32ChangedPremisesAndCountsTheRest(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	ids := []string{}
	for i := 0; i < 33; i++ {
		ids = append(ids, watchedNapArticle(t, book, draft, fmt.Sprintf("Watches the shared file %d", i), "shared.txt", "before").ID)
	}
	if err := os.WriteFile(filepath.Join(scope, "shared.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, out := decodeNapOutput(t)
	got := []string{}
	for _, p := range r.PremiseChanged {
		got = append(got, p.ID)
	}
	if want := sortedIDs(ids...)[:32]; fmt.Sprint(got) != fmt.Sprint(want) || r.PremiseChangedMore != 1 || !strings.Contains(out, `"premise_changed_more": 1`) {
		t.Fatalf("the first 32 by id and one left out:\n got %v more %d\nwant %v", got, r.PremiseChangedMore, want)
	}
}

// The agent is told to review the articles whose premise changed before the memory upgrade, and what to do with a conversation that has
// lessons (check those articles, do not add the same lesson again) and with one that has none (read it most closely).
func TestNapNextActionReviewsChangedPremisesFirstAndSortsConversationsByLessons(t *testing.T) {
	a := napNextAction
	if !strings.HasPrefix(a, "1) If premise_changed lists articles, review them before anything else") || strings.Index(a, "premise_changed") > strings.Index(a, "memory-upgrade") {
		t.Fatalf("the changed premises come first, ahead of the memory upgrade: %q", a)
	}
	for _, want := range []string{"premise_changed_more", "maintenance", "A conversation with lessons is already cited by those articles", "do not add the same content again", "without lessons most closely"} {
		if !strings.Contains(a, want) {
			t.Errorf("the next action does not say %q: %q", want, a)
		}
	}
}

// The next action tells the agent that an article needing no change is saved again unchanged with its watch: that save takes the files as
// they are now, so the article leaves premise_changed, while the article whose file changed and was not saved again stays.
func TestNapTakesAnArticleOffPremiseChangedWhenItIsSavedAgainUnchangedWithWatch(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	resaved := watchedNapArticle(t, book, draft, "Saved again after its file changed", "resaved.txt", "before")
	left := watchedNapArticle(t, book, draft, "Left as it was after its file changed", "left.txt", "before")
	for _, f := range []string{"resaved.txt", "left.txt"} {
		if err := os.WriteFile(filepath.Join(scope, f), []byte("after"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if r := decodeNap(t); len(r.PremiseChanged) != 2 {
		t.Fatalf("premise: both articles are listed before the save: %+v", r.PremiseChanged)
	}
	d := napDraft(draft, resaved.Title, "0000000000000001")
	d.ID, d.Watch = resaved.ID, &[]string{"resaved.txt"}
	saveNapArticle(t, book, d)
	r := decodeNap(t)
	if len(r.PremiseChanged) != 1 || r.PremiseChanged[0].ID != left.ID {
		t.Fatalf("the article saved again unchanged with its watch leaves the list, the other stays: %+v", r.PremiseChanged)
	}
	if !strings.Contains(napNextAction, "save it again unchanged with its id and its watch") || !strings.Contains(napNextAction, "a maintenance review alone leaves it listed") {
		t.Fatalf("the next action tells the agent how an article that needs no change leaves the list: %q", napNextAction)
	}
}

// A file of the person's that has a second name at .shikii/nap or .shikii/nap-window-end (a hard link) is neither read nor written through:
// the file is rewritten in place, so a write would change the person's file as well.
const napPersonsFile = "do not touch\n"

// linkToAPersonsFile gives path a second name of a file of the person's, which holds content, and returns the other name.
func linkToAPersonsFile(t *testing.T, path, content string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), "persons-file")
	if err := os.WriteFile(other, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(other, path); err != nil {
		t.Fatal(err)
	}
	return other
}

func requireFileHolds(t *testing.T, path, want string) {
	t.Helper()
	if got, err := os.ReadFile(path); err != nil || string(got) != want {
		t.Fatalf("%s must be left as it was:\n%q\n%q %v", path, want, got, err)
	}
}

func TestNapStateFilesWithASecondNameAreReadAsNotThere(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	text := time.Now().UTC().Format(napWindowLayout) + "\n"
	for _, path := range []string{napStatePath(st), napWindowEndFilePath(st)} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := readNapStateAt(path); got != text {
			t.Fatalf("premise: a file with one name is read: %q", got)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		linkToAPersonsFile(t, path, text)
		if got := readNapStateAt(path); got != "" {
			t.Fatalf("%s: a file with a second name is read as not there: %q", filepath.Base(path), got)
		}
	}
	if raw, end := readNapWindowEnd(st.Dir); raw != "" || !end.IsZero() {
		t.Fatalf("the window then starts at the beginning: %q %v", raw, end)
	}
}

func TestNapNoticeDoesNotWriteThroughAHardLink(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	other := linkToAPersonsFile(t, napStatePath(st), napPersonsFile)
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	out := startSession(t, scope, nil)
	requireFileHolds(t, other, napPersonsFile)
	if strings.Contains(out, napMarker) {
		t.Fatalf("no mark could be taken, so the agent is not told:\n%s", out)
	}
	if ok, err := takeNapMark(st.Dir, "", "now\n"); ok || err == nil {
		t.Fatalf("the mark is refused with an error: %v %v", ok, err)
	}
	requireFileHolds(t, other, napPersonsFile)
}

func TestNapWindowEndDoesNotWriteThroughAHardLink(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "a reply")
	other := linkToAPersonsFile(t, napWindowEndFilePath(st), napPersonsFile)
	r, err := buildNapReport(st.Dir, scope, time.Now())
	if err != nil || len(r.Conversations) == 0 {
		t.Fatalf("premise: the window is built and shows a conversation: %v %v", conversationIDs(r), err)
	}
	err = recordNapWindowEnd(st.Dir, r)
	requireFileHolds(t, other, napPersonsFile)
	if err == nil {
		t.Fatal("the end of the window is refused with an error: the next run then prints the window again")
	}
}

// .shikii/nap and .shikii/nap-window-end are read only when they are small ordinary files, and are never written through a symbolic link,
// a named pipe or a folder. The written text is what the caller read, so a write through the link would go through if it were not refused.
func TestNapStateThatIsNotAnOrdinaryFileIsReadAsNotThereAndNotWrittenThrough(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	text := time.Now().UTC().Format(napTimeLayout) + "\n"
	for _, path := range []string{napStatePath(st), napWindowEndFilePath(st)} {
		for _, kind := range []string{"a symbolic link", "a named pipe", "a folder"} {
			t.Run(filepath.Base(path)+" is "+kind, func(t *testing.T) {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				other := ""
				switch kind {
				case "a symbolic link":
					other = filepath.Join(t.TempDir(), "persons-file")
					if err := os.WriteFile(other, []byte(text), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(other, path); err != nil {
						t.Fatal(err)
					}
				case "a named pipe":
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Fatal(err)
					}
					pipe, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0) // holds the pipe open, with the text waiting in it
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { pipe.Close() })
					if _, err := pipe.WriteString(text); err != nil {
						t.Fatal(err)
					}
				case "a folder":
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if got := readNapStateAt(path); got != "" {
					t.Fatalf("it is read as not there: %q", got)
				}
				if ok, err := rewriteNapFile(path, text, "rewritten\n"); ok || err == nil {
					t.Fatalf("it is not written, and the refusal is an error: %v %v", ok, err)
				}
				if other != "" {
					requireFileHolds(t, other, text)
				}
			})
		}
	}
}

// A file larger than 256 bytes is read only up to its first 256 bytes, so a long file is never read whole.
func TestNapStateIsReadOnlyUpToTheLimit(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	text := time.Now().UTC().Format(napTimeLayout) + "\n" + strings.Repeat("x", 1024)
	for _, path := range []string{napStatePath(st), napWindowEndFilePath(st)} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := readNapStateAt(path); got != text[:256] {
			t.Fatalf("%s: the first 256 bytes are read and no more: %d bytes", filepath.Base(path), len(got))
		}
	}
}

// A notice that cannot be given (here its mark cannot be taken: .shikii/nap is a symbolic link) never stops the brief of the project.
func TestNapNoticeFailureDoesNotStopTheBriefOfTheProject(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Scene, always.AppliesTo = "", "Visible text is Japanese", "Every visible sentence is written in Japanese.", "いつも", "every request"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "persons-file")
	if err := os.WriteFile(sentinel, []byte(napPersonsFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, napStatePath(st)); err != nil {
		t.Fatal(err)
	}
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	out := startSession(t, scope, nil)
	if !strings.Contains(out, "Every visible sentence is written in Japanese.") {
		t.Fatalf("the brief is printed although the notice failed:\n%q", out)
	}
	if strings.Contains(out, napMarker) {
		t.Fatalf("no notice is given when its mark could not be taken:\n%s", out)
	}
	requireFileHolds(t, sentinel, napPersonsFile)
	home, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	if log, _ := os.ReadFile(filepath.Join(home, "hook.log")); !strings.Contains(string(log), "nap: ") {
		t.Fatalf("the reason is left in hook.log: %q", log)
	}
}

// A time in the future in the first line (the clock was set far ahead once) is no earlier notice: it must not keep the notice silent until
// that time. Every work result then counts, as when there is no file, and the file is written again with this notice alone.
func TestNapNoticeTreatsAFutureTimeInTheFileAsNoEarlierNotice(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	written := writeNapState(t, st, now.Add(48*time.Hour), now.Add(-80*time.Hour))
	saveWorkResults(t, st, scope, 0, 2, now.Add(-time.Hour))
	saveWorkResults(t, st, scope, 10, 2, now.Add(-90*24*time.Hour)) // before any real notice: counted only when the file holds no earlier time
	if out := startSession(t, scope, nil); out != "" {
		t.Fatalf("four results in all, nothing is said:\n%s", out)
	}
	if got := readNapStateFile(t, st); got != written {
		t.Fatalf("the file is not rewritten when nothing is said:\n%q\n%q", written, got)
	}
	saveWorkResults(t, st, scope, 20, 1, now.Add(-90*24*time.Hour))
	out := startSession(t, scope, nil)
	if !strings.Contains(out, napMarker) || !strings.Contains(out, "5 work results so far") {
		t.Fatalf("every result counts, and the notice comes:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(readNapStateFile(t, st)), "\n")
	if at, err := time.Parse(time.RFC3339, lines[0]); len(lines) != 1 || err != nil || at.After(time.Now().Add(time.Second)) {
		t.Fatalf("the file holds this notice alone, not the future time: %q %v", lines, err)
	}
}

// The window ends at the time of the last conversation it showed, not at now. A conversation whose time is before the moment a run read
// the records, but which was written after it (its time was set before the write, or the clock was set back), is then shown by the next run.
func TestNapShowsAConversationSavedAfterTheLastRunWithATimeBeforeThatRun(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000001", now.Add(-3*time.Hour), "first")
	first := napRunAt(t, st, scope, now)
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 1) {
		t.Fatalf("premise: the first run shows the first conversation: %v", got)
	}
	if end := parseNapTime(t, first.Window.To); !end.Equal(now.Add(-3 * time.Hour)) {
		t.Fatalf("the first window ends at the conversation it showed: %+v", first.Window)
	}
	saveConversation(t, st, scope, "0000000000000002", now.Add(-time.Minute), "written after that run read the records")
	second := napRunAt(t, st, scope, now.Add(time.Second))
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(2, 2) {
		t.Fatalf("the conversation written after the first run is shown by the next one: %v", got)
	}
}

// When a run shows nothing, the end stays where it was: it is not moved to now, and no file is made or rewritten.
func TestNapKeepsTheEndOfTheWindowWhereItWasWhenNothingIsShown(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	early := napRunAt(t, st, scope, now.Add(-24*time.Hour)) // before the conversation of the fixture
	if len(early.Conversations) != 0 || early.Window.From != "" || early.Window.To != "" {
		t.Fatalf("premise: nothing is shown and the window has no end: %+v", early)
	}
	if _, err := os.Lstat(napWindowEndFilePath(st)); !os.IsNotExist(err) {
		t.Fatalf("no end is recorded when nothing was shown and none was there: %v", err)
	}
	saveConversation(t, st, scope, "0000000000000001", now.Add(-2*time.Hour), "shown once")
	shown := napRunAt(t, st, scope, now)
	recorded := readNapWindowEndFile(t, st)
	if len(shown.Conversations) != 1 || recorded != shown.Window.To+"\n" {
		t.Fatalf("premise: the end is recorded once a conversation is shown: %+v %q", shown.Window, recorded)
	}
	later := napRunAt(t, st, scope, now.Add(time.Hour))
	if len(later.Conversations) != 0 || later.Window.From != shown.Window.To || later.Window.To != shown.Window.To {
		t.Fatalf("a window with nothing in it ends where it began: %+v", later.Window)
	}
	if got := readNapWindowEndFile(t, st); got != recorded {
		t.Fatalf("the end is left where it was:\n%q\n%q", recorded, got)
	}
}

// The nap is not limited by a count of the conversations saved: the history RecentFeedback reads (the newest 256 within 4 MiB) was made for
// recalling similar past answers. With 300 waiting, the first run shows the oldest 64, the others follow, and running it until
// newer_left is 0 shows every conversation exactly once.
func TestNapShowsEveryConversationExactlyOnceHoweverManyAreWaiting(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	const total = 300
	for i := 1; i <= total; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(total+10-i)*time.Minute), "reply")
	}
	shown := map[string]int{}
	left := []int{}
	for run := 0; run < 10; run++ {
		r := napRunAt(t, st, scope, now)
		for _, id := range conversationIDs(r) {
			shown[id]++
		}
		left = append(left, r.NewerLeft)
		if r.NewerLeft == 0 {
			break
		}
	}
	if fmt.Sprint(left) != "[236 172 108 44 0]" {
		t.Fatalf("newer_left counts down by 64 to 0: %v", left)
	}
	if len(shown) != total {
		t.Fatalf("every conversation is shown: %d of %d", len(shown), total)
	}
	for id, n := range shown {
		if n != 1 {
			t.Fatalf("%s is shown %d times, not once", id, n)
		}
	}
}

// A conversation older than the newest 256 is in the first window: the oldest ones come first.
func TestNapShowsAConversationOlderThanTheNewest256OnTheFirstRun(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	for i := 1; i <= 300; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(310-i)*time.Minute), "reply")
	}
	first := napRunAt(t, st, scope, now)
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 64) || first.NewerLeft != 236 {
		t.Fatalf("the first run shows the oldest 64 of the 300, 44 of which are older than the newest 256: %d %v left %d", len(got), got, first.NewerLeft)
	}
}
