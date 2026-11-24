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

// shikii nap lists the conversations of the window between the last two notices (after the earlier one, up to the last one),
// oldest first, and nothing outside it.
func TestNapListsOnlyTheConversationsOfTheWindow(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	before, from, to := now.Add(-100*time.Hour), now.Add(-72*time.Hour), now.Add(-24*time.Hour)
	saveConversation(t, st, scope, "0000000000000001", before, "before the window")
	saveConversation(t, st, scope, "0000000000000002", now.Add(-60*time.Hour), "inside, older")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-30*time.Hour), "inside, newer")
	saveConversation(t, st, scope, "0000000000000004", now.Add(-2*time.Hour), "after the window")
	writeNapState(t, st, to, from)
	r := decodeNap(t)
	if got := conversationIDs(r); fmt.Sprint(got) != "[0000000000000002 0000000000000003]" {
		t.Fatalf("only the two inside the window, oldest first: %v", got)
	}
	if r.Window.From != from.UTC().Format(time.RFC3339) || r.Window.To != to.UTC().Format(time.RFC3339) {
		t.Fatalf("the window is the two times of the file: %+v", r.Window)
	}
	c := r.Conversations[0]
	if c.Request != "request 0000000000000002" || c.Summary != "summary 0000000000000002" || c.Reply != "inside, older" || c.Session != "session-0000000000000002" || c.At.IsZero() {
		t.Fatalf("each conversation carries its id, time, session and its three texts: %+v", c)
	}
	if r.NextAction != napNextAction || !strings.Contains(r.NextAction, "memory-upgrade") || !strings.Contains(r.NextAction, "Never delete") {
		t.Fatalf("the next action is given: %q", r.NextAction)
	}
	if r.MemoryUpgrade.Status == "" || r.MemoryUpgrade.Total != 1 {
		t.Fatalf("the progress of the memory upgrade is given: %+v", r.MemoryUpgrade)
	}
	// with one line the window starts at the beginning; with no file it runs up to now
	writeNapState(t, st, to)
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != "[0000000000000001 0000000000000002 0000000000000003]" {
		t.Fatalf("with one time the window starts at the beginning: %v", got)
	}
	if err := os.Remove(napStatePath(st)); err != nil {
		t.Fatal(err)
	}
	r = decodeNap(t)
	if len(r.Conversations) != 4 || r.Window.From != "" {
		t.Fatalf("with no file every conversation up to now is in the window: %v %+v", conversationIDs(r), r.Window)
	}
}

// At most 64 conversations are listed: the newest of the window, oldest first, and the count left out is said.
func TestNapListsAtMost64ConversationsAndSaysHowManyWereLeftOut(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	for i := 1; i <= 70; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(100-i)*time.Minute), "reply")
	}
	r := decodeNap(t)
	ids := conversationIDs(r)
	if len(ids) != 64 || ids[0] != fmt.Sprintf("%016x", 7) || ids[63] != fmt.Sprintf("%016x", 70) || r.OlderOmitted != 6 {
		t.Fatalf("the newest 64, oldest first, and 6 left out: %d %v %v", len(ids), ids[:1], r.OlderOmitted)
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
