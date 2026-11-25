package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

const napMarker = "a memory nap is due"

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

// Once a nap has run, a second session opened right after the first is not told again: the 24 hours start from the notice. (While no nap
// has run since the notice, the next session is told again: see nap_notice_test.go.)
func TestNapNoticeIsNotRepeatedToTheNextSessionOnceANapRan(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	if out := startSession(t, scope, map[string]any{"session_id": "first"}); !strings.Contains(out, napMarker) {
		t.Fatalf("the first session is told:\n%s", out)
	}
	decodeNap(t) // the agent runs the nap
	taken := readNapStateFile(t, st)
	saveWorkResults(t, st, scope, 50, 6, time.Now()) // even with more results since
	if out := startSession(t, scope, map[string]any{"session_id": "second"}); out != "" {
		t.Fatalf("the second session is not told again:\n%s", out)
	}
	if got := readNapStateFile(t, st); got != taken {
		t.Fatalf("the second session leaves the mark as it was:\n%q\n%q", taken, got)
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

func readNapWindowEndFile(t *testing.T, st store.Store) string {
	t.Helper()
	b, err := os.ReadFile(napWindowEndFilePath(st))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// napRunUnread is what `shikii nap` does without printing, for an agent that read none of it: the window is settled, the report is built
// and what was printed is recorded (.shikii/nap-shown), as the command does it.
func napRunUnread(t *testing.T, st store.Store, scope string) napReport {
	t.Helper()
	r, err := runNapReport(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordNapShown(st.Dir, r); err != nil {
		t.Fatal(err)
	}
	if r.settleErr != nil {
		t.Fatal(r.settleErr)
	}
	return r
}

// napReadAndSettle is what an agent that read everything a run printed leaves behind: the id of every conversation in the report is named
// as a source in a save (recorded here the way a save records it, so that more than the newest 256 conversations can be named), and the
// window is moved over them, which the next run does first anyway (doing it twice changes nothing: the second move finds the window
// already moved).
func napReadAndSettle(t *testing.T, st store.Store, r napReport) {
	t.Helper()
	notebook.Book{Root: st.Dir}.RecordNapRead(conversationIDs(r))
	if _, _, err := settleNapWindow(st.Dir); err != nil {
		t.Fatal(err)
	}
}

// napRun is a run of `shikii nap` by an agent that read all it printed, with the window already moved over it: the report is built, what
// was printed is recorded, and the place after the last conversation printed is in .shikii/nap-window-end.
func napRun(t *testing.T, st store.Store, scope string) napReport {
	t.Helper()
	r := napRunUnread(t, st, scope)
	napReadAndSettle(t, st, r)
	return r
}

// decodeNapRead is decodeNap for an agent that read all it printed: the window is moved over it.
func decodeNapRead(t *testing.T, st store.Store) napReport {
	t.Helper()
	r := decodeNap(t)
	napReadAndSettle(t, st, r)
	return r
}

// feedbackPlaceAfter is the place just after the n-th complete line of the record of conversations, as .shikii/nap-window-end holds it:
// the offset after its newline and the SHA-256 of the line without the newline. It is computed here from the file, not by the command.
func feedbackPlaceAfter(t *testing.T, st store.Store, scope string, n int) string {
	t.Helper()
	raw, err := os.ReadFile(st.FeedbackFile(scope))
	if err != nil {
		t.Fatal(err)
	}
	offset := 0
	var line string
	for i := 0; i < n; i++ {
		end := strings.IndexByte(string(raw[offset:]), '\n')
		if end < 0 {
			t.Fatalf("the record has fewer than %d complete lines", n)
		}
		line = string(raw[offset : offset+end])
		offset += end + 1
	}
	sum := sha256.Sum256([]byte(line))
	return fmt.Sprintf("%d\n%s\n", offset, hex.EncodeToString(sum[:]))
}

func hexIDs(from, to int) string {
	ids := []string{}
	for i := from; i <= to; i++ {
		ids = append(ids, fmt.Sprintf("%016x", i))
	}
	return fmt.Sprint(ids)
}

// shikii nap lists the conversations written after the place the last run recorded, in the order they were written, and nothing before
// it. The notice's file (.shikii/nap) does not decide the place; the command only adds the time it ran to it, as the third line.
func TestNapListsOnlyTheConversationsAfterThePlaceTheLastRunRecorded(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveConversation(t, st, scope, "0000000000000002", now.Add(-60*time.Hour), "second, older")
	saveConversation(t, st, scope, "0000000000000003", now.Add(-30*time.Hour), "third")
	notices := writeNapState(t, st, now.Add(-24*time.Hour), now.Add(-80*time.Hour))
	r := decodeNapRead(t, st)
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(1, 3) {
		t.Fatalf("the first run lists every conversation, in the order they were written: %v", got)
	}
	if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 3); got != want {
		t.Fatalf("the place after the last conversation printed is recorded once the agent has read them:\n%q\n%q", want, got)
	}
	// The notice's file keeps what the notices wrote; the run only adds the time it ran as the third line (see nap_notice_test.go).
	if got := readNapStateFile(t, st); !strings.HasPrefix(got, notices) || len(strings.Split(got, "\n")) != 4 {
		t.Fatalf("the notice's file keeps its two lines and gains the time of the run:\n%q\n%q", notices, got)
	}
	c := r.Conversations[1]
	if c.Request != "request 0000000000000002" || c.Summary != "summary 0000000000000002" || c.Reply != "second, older" || c.Session != "session-0000000000000002" || c.At.IsZero() {
		t.Fatalf("each conversation carries its id, time, session and its three texts: %+v", c)
	}
	if r.NextAction != napNextAction || !strings.Contains(r.NextAction, "memory-upgrade") || !strings.Contains(r.NextAction, "Never delete") || !strings.Contains(r.NextAction, "newer_left") {
		t.Fatalf("the next action is given: %q", r.NextAction)
	}
	if r.MemoryUpgrade.Status == "" || r.MemoryUpgrade.Total != 1 {
		t.Fatalf("the progress of the memory upgrade is given: %+v", r.MemoryUpgrade)
	}
	saveConversation(t, st, scope, "0000000000000004", now.Add(-2*time.Hour), "after the place")
	saveConversation(t, st, scope, "0000000000000005", now.Add(-time.Hour), "after the place, newer")
	r = decodeNapRead(t, st)
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(4, 5) {
		t.Fatalf("the next run lists the two written after the place, none before it: %v", got)
	}
	if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 5); got != want {
		t.Fatalf("the place moves to the end of the last conversation printed:\n%q\n%q", want, got)
	}
	// With no file, or a file that holds no place (the first lines of the old time-based file among them), the run starts at the
	// beginning, and the file then holds a place.
	for name, prepare := range map[string]func(){
		"no file":              func() { _ = os.Remove(napWindowEndFilePath(st)) },
		"a file with no place": func() { _ = os.WriteFile(napWindowEndFilePath(st), []byte("yesterday\n"), 0o600) },
		"a time": func() {
			_ = os.WriteFile(napWindowEndFilePath(st), []byte(now.UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
		},
		"an offset with no hash": func() { _ = os.WriteFile(napWindowEndFilePath(st), []byte("100\n"), 0o600) },
		"a hash that is not hex": func() {
			_ = os.WriteFile(napWindowEndFilePath(st), []byte("100\n"+strings.Repeat("z", 64)+"\n"), 0o600)
		},
		"an offset of 0": func() { _ = os.WriteFile(napWindowEndFilePath(st), []byte("0\n"+strings.Repeat("a", 64)+"\n"), 0o600) },
	} {
		prepare()
		r = decodeNapRead(t, st)
		if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(1, 5) {
			t.Fatalf("%s: every conversation is listed: %v", name, got)
		}
		if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 5); got != want {
			t.Fatalf("%s: the place is recorded:\n%q\n%q", name, want, got)
		}
	}
}

// The reported failure: the agent was told, did not run shikii nap, and was told again. The next shikii nap shows the conversations of the
// notice nobody read as well as the later ones, and the notice does not move where the run starts.
func TestNapShowsTheConversationsOfANoticeThatWasNotActedOn(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	decodeNapRead(t, st) // the last shikii nap showed the fixture's conversation, and the agent read it
	place := readNapWindowEndFile(t, st)
	saveConversation(t, st, scope, "0000000000000002", now.Add(-70*time.Hour), "before the notice nobody acted on")
	writeNapState(t, st, now.Add(-50*time.Hour), now.Add(-120*time.Hour)) // that notice, and the one before it
	saveConversation(t, st, scope, "0000000000000003", now.Add(-30*time.Hour), "after that notice")
	saveWorkResults(t, st, scope, 0, 5, now.Add(-time.Hour))
	if out := startSession(t, scope, nil); !strings.Contains(out, napMarker) {
		t.Fatalf("the next notice comes:\n%s", out)
	}
	if got := readNapWindowEndFile(t, st); got != place {
		t.Fatalf("a notice does not move where the run starts:\n%q\n%q", place, got)
	}
	r := decodeNap(t)
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(2, 3) {
		t.Fatalf("the conversation of the notice nobody read is listed with the later one: %v", got)
	}
}

// Run twice in a row, the second shikii nap shows only what was written after the first one stopped, and a third shows nothing and leaves
// the place as it was.
func TestNapRunTwiceInARowShowsTheSecondOnlyWhatCameAfterTheFirst(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-2*time.Hour), "second")
	first := decodeNapRead(t, st) // through the command: it prints, then records what it printed; the agent read it
	if got := conversationIDs(first); fmt.Sprint(got) != hexIDs(1, 2) {
		t.Fatalf("the first run lists everything: %v", got)
	}
	if got, want := readNapWindowEndFile(t, st), feedbackPlaceAfter(t, st, scope, 2); got != want {
		t.Fatalf("the recorded place is the end of the last conversation printed:\n%q\n%q", want, got)
	}
	saveConversation(t, st, scope, "0000000000000003", time.Now().Add(-time.Hour), "third, after the first run")
	second := napRun(t, st, scope)
	if got := conversationIDs(second); fmt.Sprint(got) != hexIDs(3, 3) {
		t.Fatalf("the second run lists only what came after the first one: %v", got)
	}
	recorded := readNapWindowEndFile(t, st)
	third := napRun(t, st, scope)
	if got := conversationIDs(third); len(got) != 0 || third.NewerLeft != 0 {
		t.Fatalf("a third run with nothing new lists nothing: %v", got)
	}
	if got := readNapWindowEndFile(t, st); got != recorded {
		t.Fatalf("the place is left where it was when nothing is shown:\n%q\n%q", recorded, got)
	}
}

// fitNapReport takes from the front the conversations that keep the encoded report within the bound, at least one and at most the
// cap; newer_left counts the ones after them; and the bound is the size of what is printed.
func TestNapFitTakesWhatFitsTheOutputAtMostTheCapAndAtLeastOne(t *testing.T) {
	var convs []napConversation
	for i := 1; i <= 100; i++ {
		convs = append(convs, napConversation{ID: fmt.Sprintf("%016x", i), Session: "s", Request: "request", Summary: "summary", Reply: "reply"})
	}
	r := napReport{NextAction: napNextAction}
	if n := fitNapReport(&r, convs, 1<<30, napMaxConvs); n != 64 || len(r.Conversations) != 64 || r.NewerLeft != 36 || r.Conversations[0].ID != convs[0].ID || r.Conversations[63].ID != convs[63].ID {
		t.Fatalf("with room for all, the cap of 64 is the bound, the front ones are taken and 36 are left: %d %d left %d", n, len(r.Conversations), r.NewerLeft)
	}
	size := func(k int) int {
		r.Conversations, r.NewerLeft = convs[:k], len(convs)-k
		return napEncodedLen(r)
	}
	for _, k := range []int{2, 3, 17, 63, 64} {
		fit := size(k)
		if n := fitNapReport(&r, convs, fit, napMaxConvs); n != k || r.NewerLeft != 100-k {
			t.Fatalf("a bound of exactly the size of %d conversations takes %d: %d left %d", k, k, n, r.NewerLeft)
		}
		if n := fitNapReport(&r, convs, fit-1, napMaxConvs); n != k-1 || r.NewerLeft != 100-(k-1) {
			t.Fatalf("one byte less takes %d: %d left %d", k-1, n, r.NewerLeft)
		}
	}
	if n := fitNapReport(&r, convs, 1, napMaxConvs); n != 1 || len(r.Conversations) != 1 || r.NewerLeft != 99 {
		t.Fatalf("one conversation is printed even when it alone is larger than the bound: %d left %d", n, r.NewerLeft)
	}
	var buf bytes.Buffer
	fitNapReport(&r, convs, 1<<30, 10)
	if err := encodeNapReport(&buf, r); err != nil || buf.Len() != napEncodedLen(r) {
		t.Fatalf("the size counted is the size printed: %d %d %v", buf.Len(), napEncodedLen(r), err)
	}
	empty := napReport{NextAction: napNextAction}
	if n := fitNapReport(&empty, nil, napOutputMax, napMaxConvs); n != 0 || empty.Conversations == nil || empty.NewerLeft != 0 {
		t.Fatalf("with no conversation, an empty list (printed as [], not null) and nothing left: %d %v", n, empty.Conversations)
	}
	var out bytes.Buffer
	if err := encodeNapReport(&out, empty); err != nil || !strings.Contains(out.String(), `"conversations": []`) {
		t.Fatalf("no conversation is printed as []: %v\n%s", err, out.String())
	}
}

// The most one run prints is a number every supported agent shows whole; the guides and the help say it, so a change of it is a change of
// them (TestNapDocsDescribeThePlaceAndTheFileThatRecordsIt).
func TestNapOutputMaxIs24000(t *testing.T) {
	if napOutputMax != 24000 {
		t.Fatalf("napOutputMax is %d: the guides and the help say 24,000 bytes", napOutputMax)
	}
}

// The window is moved under the file's lock and only while the file still holds what the run that printed started from. A run that finds
// the lock held, or another run's place in the file, moves nothing; the conversations are then listed again, never lost.
func TestNapWindowEndIsMovedUnderTheLockAndOnlyWhileTheFileIsAsTheLastRunStartedFromIt(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
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
	decodeNap(t) // prints the fixture's conversation
	notebook.Book{Root: st.Dir}.RecordNapRead([]string{"0000000000000001"})
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(1, 1) {
		t.Fatalf("with the lock held the conversations are still printed, and the one that was read cannot be moved past: %v", got)
	}
	if got := readNapWindowEndFile(t, st); got != "" {
		t.Fatalf("with the lock held nothing is recorded: %q", got)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	notebook.Book{Root: st.Dir}.RecordNapRead([]string{"0000000000000001"}) // the agent read it again, in the output of the run above
	if got := conversationIDs(decodeNap(t)); len(got) != 0 {
		t.Fatalf("with the lock free the conversation read is moved past: %v", got)
	}
	if recorded := readNapWindowEndFile(t, st); recorded != feedbackPlaceAfter(t, st, scope, 1) {
		t.Fatalf("with the lock free the place is recorded: %q", recorded)
	}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-30*time.Minute), "a reply saved after that run")
	r := napRunUnread(t, st, scope)
	if fmt.Sprint(conversationIDs(r)) != hexIDs(2, 2) {
		t.Fatalf("premise: the next run shows the new conversation: %v", conversationIDs(r))
	}
	notebook.Book{Root: st.Dir}.RecordNapRead([]string{"0000000000000002"})
	other := formatNapWindowEnd(store.FeedbackMark{Offset: 99999, Hash: strings.Repeat("a", 64)}) // another run records its place between the print and the move
	if err := os.WriteFile(napWindowEndFilePath(st), []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := settleNapWindow(st.Dir); err != nil {
		t.Fatal(err)
	}
	if got := readNapWindowEndFile(t, st); got != other {
		t.Fatalf("a file that is no longer as the last run started from it is left to the run that wrote it:\n%q\n%q", other, got)
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
	if _, err := os.Lstat(filepath.Join(st.Dir, napShownName)); !os.IsNotExist(err) {
		t.Fatalf("nothing was shown, so what was shown is not recorded either: %v", err)
	}
	if got := conversationIDs(decodeNap(t)); fmt.Sprint(got) != hexIDs(1, 1) {
		t.Fatalf("the next run lists the conversation: %v", got)
	}
}

// The guides describe the conversations the command prints, the file that records where it stopped, the lessons of a conversation and the
// articles whose premise changed.
func TestNapDocsDescribeThePlaceAndTheFileThatRecordsIt(t *testing.T) {
	for _, d := range []struct {
		file, heading string
		words         []string
	}{
		{"../../docs/guide.md", "### Memory nap", []string{"`.shikii/nap-window-end`", "`newer_left`", "about 24,000 bytes", "at most 64", "starts where the agent last read up to", "`.shikii/nap-read`", "`.shikii/nap-shown`", "`shown_again`", "Printing is not reading", "names its id among the sources of a wiki entry", "SHA-256", "no time is compared", "remove `.shikii/nap-window-end`", "`lessons`", "`premise_changed`", "`premise_changed_more`", "up to 32 in id order", "at most about 24,000 bytes in all", "`next_action` and the articles whose premise changed (below) included", "that run prints no conversation"}},
		{"../../docs/guide.ja.md", "### \u8a18\u61b6\u306e\u3072\u3068\u4f11\u307f", []string{"`.shikii/nap-window-end`", "`newer_left`", "\u304a\u3088\u305d 24,000 \u30d0\u30a4\u30c8", "\u6700\u5927\u3067\u3082 64 \u4ef6", "\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u304c\u6700\u5f8c\u306b\u8aad\u307f\u7d42\u3048\u305f\u6240\u304b\u3089\u59cb\u307e\u308b", "`.shikii/nap-read`", "`.shikii/nap-shown`", "`shown_again`", "\u51fa\u3057\u305f\u3060\u3051\u3067\u306f\u3001\u8aad\u3093\u3060\u3053\u3068\u306b\u306a\u308a\u307e\u305b\u3093", "SHA-256", "\u6642\u523b\u306f\u6bd4\u3079\u307e\u305b\u3093", "`.shikii/nap-window-end` \u3092\u6d88\u3059\u3088\u3046\u4f1d\u3048\u307e\u3059", "`lessons`", "`premise_changed`", "`premise_changed_more`", "id \u306e\u9806\u306b 32 \u4ef6\u307e\u3067", "\u5168\u4f53\u3067\u304a\u3088\u305d 24,000 \u30d0\u30a4\u30c8\u307e\u3067", "`next_action` \u3068\u3001\u524d\u63d0\u306e\u5909\u308f\u3063\u305f\u8a18\u4e8b(\u4e0b\u306e `premise_changed`)\u3082\u3053\u306e\u5927\u304d\u3055\u306b\u6570\u3048\u307e\u3059", "\u8a18\u4e8b\u3067\u5927\u304d\u3055\u3092\u4f7f\u3044\u5207\u308b\u56de\u306f\u4f1a\u8a71\u3092\u51fa\u3055\u305a"}},
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
		for _, old := range []string{"the same time as the 64th", "window the last", "RFC 3339"} {
			if strings.Contains(section, old) {
				t.Errorf("%s: the section on the nap still describes the time window (%q)", d.file, old)
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
		for _, want := range []string{".shikii/nap-window-end", "newer_left", "shown_again", "reflect submit", "lessons", "premise_changed", "premise_changed_more", "24,000 bytes", "The whole output, those articles included, stays within about 24,000 bytes", "last_nap", ".shikii/nap-log", "shikii status"} {
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
	if got := conversationIDs(r); fmt.Sprint(got) != fmt.Sprint([]string{"0000000000000001", "0000000000000002", "0000000000000003"}) {
		t.Fatalf("every conversation is still listed, in the order they were written: %v", got)
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
	if got := conversationIDs(r); fmt.Sprint(got) != fmt.Sprint([]string{"0000000000000001", "0000000000000004", "0000000000000005", "0000000000000006", "0000000000000007"}) {
		t.Fatalf("every conversation is still listed, in the order they were written, whatever became of the articles that cite it: %v", got)
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
	if !strings.Contains(napNextAction, "save it again unchanged with its id and its watch") || !strings.Contains(napNextAction, "a maintenance review alone leaves it listed") || !strings.Contains(napNextAction, "a watch that leaves that file out") {
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
	text := time.Now().UTC().Format(napTimeLayout) + "\n"
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
	if raw, place := readNapWindowEnd(st.Dir); raw != "" || place != (store.FeedbackMark{}) {
		t.Fatalf("the run then starts at the beginning: %q %v", raw, place)
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
	r := napRunUnread(t, st, scope)
	if len(r.Conversations) == 0 {
		t.Fatalf("premise: the window is built and shows a conversation: %v", conversationIDs(r))
	}
	notebook.Book{Root: st.Dir}.RecordNapRead(conversationIDs(r))
	_, _, err := settleNapWindow(st.Dir)
	requireFileHolds(t, other, napPersonsFile)
	if err == nil {
		t.Fatal("moving the window is refused with an error: the next run then prints the conversations again")
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
	always.ID, always.Title, always.Lesson, always.Scene, always.AppliesTo = "", "Visible text is Japanese", "Every visible sentence is written in Japanese.", "\u3044\u3064\u3082", "every request"
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

// When a run shows nothing, the place stays where it was: no file is made or rewritten.
func TestNapKeepsThePlaceWhereItWasWhenNothingIsShown(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	if err := os.Remove(st.FeedbackFile(scope)); err != nil {
		t.Fatal(err)
	}
	if none := napRun(t, st, scope); len(none.Conversations) != 0 || none.NewerLeft != 0 {
		t.Fatalf("premise: nothing is shown: %+v", none)
	}
	if _, err := os.Lstat(napWindowEndFilePath(st)); !os.IsNotExist(err) {
		t.Fatalf("no place is recorded when nothing was shown and none was there: %v", err)
	}
	saveConversation(t, st, scope, "0000000000000001", time.Now().Add(-2*time.Hour), "shown once")
	shown := napRun(t, st, scope)
	recorded := readNapWindowEndFile(t, st)
	if len(shown.Conversations) != 1 || recorded != feedbackPlaceAfter(t, st, scope, 1) {
		t.Fatalf("premise: the place is recorded once a conversation is shown: %q", recorded)
	}
	// A line with a reply of only blanks is not shown, and a place is only ever the end of a conversation that was shown.
	appendRawFeedbackLines(t, st, scope, `{"id":"00000000000000aa","request":"r","summary":"s","reply":"  "}`+"\n")
	later := napRun(t, st, scope)
	if len(later.Conversations) != 0 || later.NewerLeft != 0 {
		t.Fatalf("a run with nothing new lists nothing: %+v", later)
	}
	if got := readNapWindowEndFile(t, st); got != recorded {
		t.Fatalf("the place is left where it was:\n%q\n%q", recorded, got)
	}
}

// A conversation older than the newest 256 is in the first run: the ones written first come first.
func TestNapShowsAConversationOlderThanTheNewest256OnTheFirstRun(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	for i := 2; i <= 300; i++ {
		saveConversation(t, st, scope, fmt.Sprintf("%016x", i), now.Add(-time.Duration(310-i)*time.Minute), "reply")
	}
	first := napRun(t, st, scope)
	got := conversationIDs(first)
	if len(got) == 0 || len(got)+first.NewerLeft != 300 || fmt.Sprint(got) != hexIDs(1, len(got)) {
		t.Fatalf("the first run starts with the conversation written first (44 of the 300 are older than the newest 256) and goes on in file order: %d %v left %d", len(got), got, first.NewerLeft)
	}
}
