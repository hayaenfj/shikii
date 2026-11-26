package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The notice of the memory nap (nap.go, napNotice): what it says, and when it comes again. A real Claude Code was told at the start of a
// session, did not run `shikii nap`, and was never told again for 24 hours, because the notice wrote its time at once. The notice now
// comes again at the next session start while no `shikii nap` has run since it; once one has, the usual 24 hours and 5 results apply.

// noticeContext is the text the SessionStart output adds to the context ("" when the hook printed nothing).
func noticeContext(t *testing.T, out string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	spec, _ := v["hookSpecificOutput"].(map[string]any)
	ctx, _ := spec["additionalContext"].(string)
	return ctx
}

var sourceFileName = regexp.MustCompile(`^[0-9a-f]{16}\.json$`)

// napSourceFiles are the files of the sources the project's wiki holds now.
func napSourceFiles(t *testing.T, st store.Store) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range files {
		if sourceFileName.MatchString(filepath.Base(f)) {
			out[f] = true
		}
	}
	return out
}

// The sentence asks for the nap now and says what to do when the agent cannot start a subagent: no "if your agent can" that an agent can
// read as leave it, and no "at a break" that can be put off for ever.
func TestNapNoticeAsksToStartTheNapNowAndSaysWhatToDoWithoutASubagent(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveWorkResults(t, st, scope, 0, 5, time.Now())
	ctx := noticeContext(t, startSession(t, scope, nil))
	for _, want := range []string{
		"shikii: a memory nap is due (5 work results so far).",
		"Start it now: launch one background subagent that runs `shikii nap` in this project and follows its next_action, then go on with the person's request without waiting for it.",
		"If your agent cannot run a background subagent, run `shikii nap` yourself after you answer this request, before you end.",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("the notice lacks %q:\n%s", want, ctx)
		}
	}
	for _, old := range []string{"If your agent can,", "otherwise do it at a break", "it is time for a memory nap"} {
		if strings.Contains(ctx, old) {
			t.Errorf("the notice still says %q:\n%s", old, ctx)
		}
	}
	if len(ctx) > 360 {
		t.Errorf("the notice stays about as long as it was (a fixed instruction for the agent): %d bytes", len(ctx))
	}
}

// The agent did not run the nap: the next session start tells it again, counting from where the first notice counted (the time before
// it, not the notice), and does not fall silent when the count has shrunk below 5 since (a cleanup of the sources).
func TestNapNoticeComesAgainWhileNoNapRanSinceItAndCountsFromTheSamePlace(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	last := now.Add(-80 * time.Hour)
	writeNapState(t, st, last) // the notice before this one
	was := napSourceFiles(t, st)
	saveWorkResults(t, st, scope, 0, 5, now.Add(-time.Hour))
	var results []string
	for f := range napSourceFiles(t, st) {
		if !was[f] {
			results = append(results, f)
		}
	}
	if len(results) != 5 {
		t.Fatalf("premise: five results were saved: %v", results)
	}
	since := last.UTC().Format(time.RFC3339)
	if ctx := noticeContext(t, startSession(t, scope, map[string]any{"session_id": "first"})); !strings.Contains(ctx, "5 work results since "+since) {
		t.Fatalf("the first session is told, counting from the notice before:\n%s", ctx)
	}
	for _, f := range results[:2] {
		if err := os.Remove(f); err != nil { // fewer than 5 are left
			t.Fatal(err)
		}
	}
	ctx := noticeContext(t, startSession(t, scope, map[string]any{"session_id": "second"}))
	if !strings.Contains(ctx, napMarker) || !strings.Contains(ctx, "3 work results since "+since) {
		t.Fatalf("no nap ran, so the next session is told again from the same place, and not silenced by the smaller count:\n%s", ctx)
	}
	lines := strings.Split(readNapStateFile(t, st), "\n")
	if len(lines) != 4 || lines[1] != since || lines[2] != "" || lines[3] != "" {
		t.Fatalf("the file holds this notice, the notice before it kept as it was, and an empty line for the nap that has not run: %q", lines)
	}
	if at, err := time.Parse(time.RFC3339, lines[0]); err != nil || time.Since(at) > time.Minute {
		t.Fatalf("the first line is the time of this notice: %q %v", lines[0], err)
	}
}

// The same notice comes at every session start until a nap runs; after one has, the 24 hours and 5 results of before apply again.
func TestNapNoticeStopsOnceANapRanAndThenFollowsTheUsualRule(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	now := time.Now()
	saveWorkResults(t, st, scope, 0, 5, now.Add(-time.Hour))
	for _, id := range []string{"first", "second", "third"} {
		if ctx := noticeContext(t, startSession(t, scope, map[string]any{"session_id": id})); !strings.Contains(ctx, "5 work results so far") {
			t.Fatalf("session %s is told while no nap ran:\n%s", id, ctx)
		}
	}
	decodeNap(t) // the nap runs
	lines := strings.Split(readNapStateFile(t, st), "\n")
	if len(lines) != 4 {
		t.Fatalf("the file holds three lines: %q", lines)
	}
	if at, err := time.Parse(time.RFC3339, lines[2]); err != nil || time.Since(at) > time.Minute {
		t.Fatalf("the third line is the time the nap ran: %q %v", lines[2], err)
	}
	if out := startSession(t, scope, map[string]any{"session_id": "fourth"}); out != "" {
		t.Fatalf("after a nap ran nothing is said within 24 hours:\n%s", out)
	}
	// 25 hours later (the times of the file are set back), with 4 results since: still nothing; with 5: told, counting from the notice
	notice := now.Add(-25 * time.Hour)
	ran := now.Add(-24 * time.Hour)
	text := notice.UTC().Format(time.RFC3339) + "\n\n" + ran.UTC().Format(time.RFC3339) + "\n"
	if err := os.WriteFile(napStatePath(st), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	st2, scope2, _, _, _ := memoryEffectFixture(t)
	if err := os.WriteFile(napStatePath(st2), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	saveWorkResults(t, st2, scope2, 0, 4, now.Add(-time.Hour))
	if out := startSession(t, scope2, nil); out != "" {
		t.Fatalf("four results since the notice, a nap ran: nothing is said:\n%s", out)
	}
	saveWorkResults(t, st2, scope2, 10, 1, now.Add(-30*time.Minute))
	if ctx := noticeContext(t, startSession(t, scope2, nil)); !strings.Contains(ctx, "5 work results since "+notice.UTC().Format(time.RFC3339)) {
		t.Fatalf("five results since the notice, 24 hours passed: told by the usual rule:\n%s", ctx)
	}
}

// A run of `shikii nap` marks the notice it follows, once: the file of a version before this (one or two lines) gets the third line, a
// later run leaves the first time as it was, and a project that was never told gets no file.
func TestNapRunMarksTheNoticeOnceAndMakesNoFileWhenThereWasNone(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	decodeNap(t)
	if _, err := os.Lstat(napStatePath(st)); !os.IsNotExist(err) {
		t.Fatalf("a nap that follows no notice makes no .shikii/nap: %v", err)
	}
	now := time.Now()
	notice, before := now.Add(-3*time.Hour), now.Add(-90*time.Hour)
	writeNapState(t, st, notice, before) // the form of a version before this one
	decodeNap(t)
	first := readNapStateFile(t, st)
	lines := strings.Split(first, "\n")
	if len(lines) != 4 || lines[0] != notice.UTC().Format(time.RFC3339) || lines[1] != before.UTC().Format(time.RFC3339) {
		t.Fatalf("the notice and the one before it are kept, and the run is added: %q", lines)
	}
	if at, err := time.Parse(time.RFC3339, lines[2]); err != nil || time.Since(at) > time.Minute {
		t.Fatalf("the third line is the time the nap ran: %q %v", lines[2], err)
	}
	decodeNap(t)
	if got := readNapStateFile(t, st); got != first {
		t.Fatalf("a later run keeps the first run after the notice:\n%q\n%q", first, got)
	}
}

// A notice that comes again takes the mark the way the first one does: under the file's lock, and only while the file holds what it
// read. A session that finds the lock held says nothing and writes nothing, so two sessions that start together are not both told.
func TestNapNoticeThatComesAgainTakesTheMarkUnderTheLockOfTheFile(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveWorkResults(t, st, scope, 0, 5, time.Now().Add(-time.Hour))
	if ctx := noticeContext(t, startSession(t, scope, nil)); !strings.Contains(ctx, napMarker) {
		t.Fatalf("premise: the first session is told:\n%s", ctx)
	}
	waiting := readNapStateFile(t, st)
	f, err := os.OpenFile(napStatePath(st), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	notice, err := napNotice(st.Dir, scope, time.Now())
	if notice != "" || err != nil {
		t.Fatalf("with the lock held by another session nothing is said: %q %v", notice, err)
	}
	if got := readNapStateFile(t, st); got != waiting {
		t.Fatalf("nothing was written:\n%q\n%q", waiting, got)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if notice, err := napNotice(st.Dir, scope, time.Now()); err != nil || !strings.Contains(notice, napMarker) {
		t.Fatalf("with the lock free it is said again: %q %v", notice, err)
	}
}

// While another session holds the lock of .shikii/nap (it is taking the mark of a notice), a run of `shikii nap` leaves the file as it is
// and still succeeds: the run is not held up, and writes nothing it was not sure of.
func TestNapRunLeavesTheMarkAloneWhileAnotherSessionHoldsItsLock(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	written := writeNapState(t, st, time.Now().Add(-3*time.Hour))
	f, err := os.OpenFile(napStatePath(st), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	decodeNap(t)
	if got := readNapStateFile(t, st); got != written {
		t.Fatalf("nothing was written while the lock was held:\n%q\n%q", written, got)
	}
}

// The guides describe the notice that comes again, the third line of the file, and the sentence's ask to start the nap now.
func TestNapGuidesDescribeTheNoticeThatComesAgain(t *testing.T) {
	for _, d := range []struct {
		file, heading string
		want, old     []string
	}{
		{"../../docs/guide.md", "### Memory nap", []string{"start the nap now", "launch one background subagent", "runs `shikii nap` itself after it answers that request, before it ends", "on the third the time of the first `shikii nap` that ran after this notice", "While the third line is empty", "the next session start tells the agent again", "so a count that has shrunk since does not silence it", "Once a nap has run, the 24 hours and the 5 results apply again", "is read as a notice that a `shikii nap` followed"}, []string{"to run `shikii nap` in a background subagent. The numbers", "(this notice on the first line, the previous one on the second), so another"}},
		{"../../docs/guide.ja.md", "### \u8a18\u61b6\u306e\u3072\u3068\u4f11\u307f", []string{"\u4eca\u3059\u3050\u898b\u76f4\u3057\u3092\u59cb\u3081\u308b\u3088\u3046\u4f1d\u3048\u307e\u3059", "\u88cf\u306e\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u3092 1 \u3064\u8d77\u3053\u3057\u3066", "\u81ea\u5206\u3067 `shikii nap` \u3092\u6d41\u3057\u307e\u3059", "3 \u884c\u76ee\u304c\u3001\u3053\u306e\u77e5\u3089\u305b\u306e\u5f8c\u306b\u521d\u3081\u3066\u6d41\u308c\u305f `shikii nap` \u306e\u6642\u523b\u3067\u3059", "3 \u884c\u76ee\u304c\u7a7a\u306e\u307e\u307e", "\u6b21\u306e\u30bb\u30c3\u30b7\u30e7\u30f3\u304c\u59cb\u307e\u308b\u6642\u306b", "\u4ef6\u6570\u304c\u6e1b\u3063\u3066\u3082\u77e5\u3089\u305b\u306f\u6d88\u3048\u307e\u305b\u3093", "\u307e\u305f 24 \u6642\u9593\u3068 5 \u4ef6\u306e\u6c7a\u307e\u308a\u306b\u623b\u308a\u307e\u3059", "\u77e5\u3089\u305b\u306e\u5f8c\u306b `shikii nap` \u304c\u6d41\u308c\u305f\u7269\u3068\u3057\u3066\u8aad\u307f\u307e\u3059"}, []string{"\u300c\u88cf\u306e\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u3067 `shikii nap` \u3092\u6d41\u3057\u3066\u300d\u3068\u4f1d\u3048\u307e\u3059", "(1 \u884c\u76ee\u304c\u4eca\u56de\u30012 \u884c\u76ee\u304c\u524d\u56de)\u3002\u540c\u6642\u306b\u958b\u3044\u305f"}},
	} {
		section := strings.Join(strings.Fields(napGuideSection(t, d.file, d.heading)), " ")
		for _, want := range d.want {
			if !strings.Contains(section, want) {
				t.Errorf("%s does not say %q", d.file, want)
			}
		}
		for _, old := range d.old {
			if strings.Contains(section, old) {
				t.Errorf("%s still says %q", d.file, old)
			}
		}
	}
}

// A .shikii/nap of a version before the third line (one or two lines) is a notice that a nap followed: it is not told again before 24 hours
// and 5 results, and it is told by that rule after them, counting from its first line.
func TestNapNoticeReadsTheFileOfAnEarlierVersionAsFollowed(t *testing.T) {
	now := time.Now()
	for name, times := range map[string][]time.Time{
		"one line":  {now.Add(-3 * time.Hour)},
		"two lines": {now.Add(-3 * time.Hour), now.Add(-90 * time.Hour)},
	} {
		st, scope, _, _, _ := memoryEffectFixture(t)
		written := writeNapState(t, st, times...)
		saveWorkResults(t, st, scope, 0, 5, now.Add(-time.Hour))
		if out := startSession(t, scope, nil); out != "" {
			t.Fatalf("%s: three hours after a notice nothing is said:\n%s", name, out)
		}
		if got := readNapStateFile(t, st); got != written {
			t.Fatalf("%s: the file is left as it was:\n%q\n%q", name, written, got)
		}
		old := now.Add(-30 * time.Hour)
		writeNapState(t, st, append([]time.Time{old}, times[1:]...)...)
		if ctx := noticeContext(t, startSession(t, scope, nil)); !strings.Contains(ctx, "5 work results since "+old.UTC().Format(time.RFC3339)) {
			t.Fatalf("%s: after 24 hours the usual rule tells it, counting from the first line:\n%s", name, ctx)
		}
	}
}

// Only the first run after a notice is kept: a later run, even an hour on, leaves the file byte for byte as it was. (The times of the
// file reach the second, so a test that runs the nap twice in a row cannot tell the two runs apart; the time is given here.)
func TestNapRunAfterTheFirstOneLeavesTheThirdLineAsItWas(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	notice, ran := now.Add(-5*time.Hour), now.Add(-4*time.Hour)
	if err := os.WriteFile(napStatePath(st), []byte(formatNapMark(notice, time.Time{}, time.Time{})), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordNapRan(st.Dir, ran); err != nil {
		t.Fatal(err)
	}
	want := notice.Format(time.RFC3339) + "\n\n" + ran.Format(time.RFC3339) + "\n"
	if got := readNapStateFile(t, st); got != want {
		t.Fatalf("the first run is written as the third line:\n%q\n%q", want, got)
	}
	if err := recordNapRan(st.Dir, now); err != nil {
		t.Fatal(err)
	}
	if got := readNapStateFile(t, st); got != want {
		t.Fatalf("a later run leaves the file as it was:\n%q\n%q", want, got)
	}
}
