package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
)

// 2026-11-28: in a project whose .shikii/handoff/ held 970,000 files (the output of tools, 14 GB), the SessionStart hook took 19 seconds
// where the agent gives it 15, and the agent dropped the output whole: not the brief, not the notice of the memory nap. A reading that can take
// the time of the project's size must cost the help it gives (the newest work record, the count behind the notice) and never the rules.
// The limits of those two readings are variables here made past, so that what a hook does when it runs out of time is shown without a
// project of that size.

func agentHookLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".shikii", "agent", "hook.log"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestSessionStartPastTheRecordLimitStillBringsTheRulesAndSaysSoInTheLog(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Scene, always.AppliesTo = "", "Visible text is Japanese", "Every visible sentence is written in Japanese.", "\u3044\u3064\u3082", "every request"
	page, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (handoff.Book{Store: st.Dir}).Write("plan/scope.md", "# Scope\n\n## Where the work stands\nA and B are in; C is next.\n", false); err != nil {
		t.Fatal(err)
	}
	in := map[string]any{"session_id": "s", "cwd": scope, "hook_event_name": "SessionStart", "source": "startup"}

	sessionRecordLimit = -time.Second // the limit has passed before the first folder is read (the test restores it: hookEnv's patientGit)
	slow := noticeContext(t, runHook(t, "session-start", in))
	for _, want := range []string{"Every visible sentence is written in Japanese.", "lesson:" + page.Page.ID} {
		if !strings.Contains(slow, want) {
			t.Fatalf("past the limit the brief still carries the rules (missing %q):\n%s", want, slow)
		}
	}
	if !strings.Contains(slow, sessionRecordUnread) {
		t.Fatalf("a search cut before it found a record says so, not nothing:\n%s", slow)
	}
	if strings.Contains(slow, "plan/scope.md") || strings.Contains(slow, "A and B are in") {
		t.Fatalf("no folder was read in time, so no record is named:\n%s", slow)
	}
	if log := agentHookLog(t); !strings.Contains(log, "session start: the folders of .shikii/handoff/ were not all read within") {
		t.Fatalf("hook.log says the record was not looked for to the end:\n%s", log)
	}

	sessionRecordLimit = time.Minute
	whole := noticeContext(t, runHook(t, "session-start", in))
	if !strings.Contains(whole, "plan/scope.md") || !strings.Contains(whole, "A and B are in; C is next.") || !strings.Contains(whole, "Every visible sentence is written in Japanese.") {
		t.Fatalf("with the time the record comes with the rules:\n%s", whole)
	}
}

// The notice that cannot be counted in time is left for the next session start: nothing is written (so the next one counts again), the
// brief of the same output still comes, and hook.log says why. Then with the time the notice comes and is written.
func TestNapNoticePastTheCountLimitIsLeftForTheNextSessionAndTheBriefStillComes(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Scene, always.AppliesTo = "", "Visible text is Japanese", "Every visible sentence is written in Japanese.", "\u3044\u3064\u3082", "every request"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always); err != nil {
		t.Fatal(err)
	}
	saveWorkResults(t, st, scope, 0, 6, time.Now())

	napCountLimit = -time.Second
	slow := noticeContext(t, startSession(t, scope, nil))
	if !strings.Contains(slow, "Every visible sentence is written in Japanese.") {
		t.Fatalf("the rules come though the notice could not be counted:\n%s", slow)
	}
	if strings.Contains(slow, "memory nap is due") {
		t.Fatalf("a notice that was not counted is not given:\n%s", slow)
	}
	if got := readNapStateFile(t, st); got != "" {
		t.Fatalf("nothing is written for a notice that was not given, so that the next session start counts again: %q", got)
	}
	if log := agentHookLog(t); !strings.Contains(log, "nap: the notice is left for the next session start") {
		t.Fatalf("hook.log says the notice was left for the next session start:\n%s", log)
	}

	napCountLimit = time.Minute
	next := noticeContext(t, startSession(t, scope, nil))
	if !strings.Contains(next, "shikii: a memory nap is due (6 work results so far).") || !strings.Contains(next, "Every visible sentence is written in Japanese.") {
		t.Fatalf("with the time the notice comes with the rules:\n%s", next)
	}
	if got := readNapStateFile(t, st); got == "" {
		t.Fatal("the notice that was given is written")
	}
}

// A search for the newest work record that the time limit cut names the record it found and says a newer one may exist: the agent would
// otherwise resume from a record that a newer one replaced, with only hook.log knowing (audit of 0.45.0).
func TestTheBriefSaysWhenTheWorkRecordIsTheNewestOfAnUnfinishedSearch(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	if _, err := (handoff.Book{Store: st.Dir}).Write("plan/scope.md", "# Scope\n\n## Where the work stands\nA and B are in; C is next.\n", false); err != nil {
		t.Fatal(err)
	}
	in := map[string]any{"session_id": "s", "cwd": scope, "hook_event_name": "SessionStart", "source": "startup"}
	old := sessionRecordsOf
	t.Cleanup(func() { sessionRecordsOf = old })
	sessionRecordsOf = func(book handoff.Book, deadline time.Time) ([]handoff.Entry, bool, error) {
		found, _, err := old(book, deadline)
		return found, false, err
	}
	cutShort := noticeContext(t, runHook(t, "session-start", in))
	if !strings.Contains(cutShort, "Work record plan/scope.md (the newest found within the time limit; a newer one may exist, see `shikii handoff list`), its last section:") || !strings.Contains(cutShort, "A and B are in; C is next.") {
		t.Fatalf("the brief of a search the limit cut says the record may not be the newest:\n%s", cutShort)
	}
	sessionRecordsOf = old
	if whole := noticeContext(t, runHook(t, "session-start", in)); !strings.Contains(whole, "Work record plan/scope.md, its last section:") {
		t.Fatalf("a finished search names the record as it is:\n%s", whole)
	}
}

// A notice that no `shikii nap` followed is given again whatever the count is, so a count that takes too long does not hold it back
// (audit of 0.45.0): the sentence then says it was asked before instead of a number.
func TestNapNoticeGivenAgainDoesNotWaitForACountPastTheLimit(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	saveWorkResults(t, st, scope, 0, 6, time.Now())
	if first := noticeContext(t, startSession(t, scope, nil)); !strings.Contains(first, "shikii: a memory nap is due (6 work results so far).") {
		t.Fatalf("the first notice counts:\n%s", first)
	}
	written := readNapStateFile(t, st)
	napCountLimit = -time.Second // restored by hookEnv's cleanup
	again := noticeContext(t, startSession(t, scope, nil))
	if !strings.Contains(again, "shikii: a memory nap is due (asked at an earlier session start and not run yet).") || !strings.Contains(again, "`shikii nap`") {
		t.Fatalf("the notice is given again though the count took too long:\n%s", again)
	}
	was, now := strings.Split(written, "\n"), strings.Split(readNapStateFile(t, st), "\n")
	if len(was) != 4 || len(now) != 4 || now[0] == "" || now[1] != was[1] || now[2] != "" {
		t.Fatalf("the notice given again is written as one that counted is: its own time, the same time to count from, and no run of the nap\nwas %q\nnow %q", was, now)
	}
	if log := agentHookLog(t); !strings.Contains(log, "nap: the notice is given again without a count") {
		t.Fatalf("hook.log says the notice went without a count:\n%s", log)
	}
}
