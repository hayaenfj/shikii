package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// forgetRun is applied to a project that holds the records of two sessions (forget_fixture_test.go). The session to forget (A) is only
// found through what its own records name: a field with its id, the turns, deliveries, decisions and sources they carry. B's id starts
// with A's, so a substring match on A's id takes B's records too. A cited source, a source another record still names and a decision a
// person labeled stay.
func TestForgetRemovesWhatTheSessionLeftAndNothingElse(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	out, err := forgetCommand(t, w.A, "--apply")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if problems := w.problems(before); len(problems) > 0 {
		t.Fatalf("forget left the project wrong:\n  %s\n%s", strings.Join(problems, "\n  "), out)
	}
	for _, want := range []string{"Removed:", "records/guards.jsonl: 2 lines", "records/memories: 4 lines", "records/memory-reviews: 2 files", "records/coverage: 2 lines",
		"a person labeled it (source ui)", "cited by the article", "named by the work state of another session (" + w.B + ")", "named by the wiki's search records",
		"Candidates", "source " + w.src["mention"] + ": its text names " + w.A, "plan.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
}

// What was removed can be read back: each file is in the folder under its own path, each removed line is appended to the file of its
// path as it was, and plan.json says what was removed, what stayed and why.
func TestForgetKeepsWhatItRemovedWhereItCanBeReadBack(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	after := w.snapshot()
	runs := map[string]bool{}
	for path := range after {
		if rest, ok := strings.CutPrefix(path, "store/records/forgotten/"); ok {
			runs[strings.SplitN(rest, "/", 2)[0]] = true
		}
	}
	if len(runs) != 1 {
		t.Fatalf("one run's folder is expected: %v", runs)
	}
	var run string
	for r := range runs {
		run = r
	}
	if !strings.HasPrefix(run, "20") || !strings.HasSuffix(run, "Z-"+w.A[:9]) {
		t.Errorf("the folder is named <UTC time>-<short id>: %s", run)
	}
	kept := func(key string) []byte {
		if rest, ok := strings.CutPrefix(key, "home/"); ok {
			return after["store/records/forgotten/"+run+"/home/.shikii/"+rest]
		}
		return after["store/records/forgotten/"+run+"/"+strings.TrimPrefix(key, "store/")]
	}
	files, lines := 0, 0
	for path, b := range before {
		if strings.HasSuffix(path, ".lock") || strings.HasPrefix(path, "store/records/forgotten/") {
			continue
		}
		now, exists := after[path]
		switch {
		case !exists:
			files++
			if got := kept(path); !bytes.Equal(got, b) {
				t.Errorf("the removed file %s is not kept as it was (%d bytes kept, %d before)", path, len(got), len(b))
			}
		case strings.HasSuffix(path, ".jsonl") && !bytes.Equal(b, now):
			var want []byte
			for _, line := range bytes.Split(b, []byte("\n")) {
				if len(bytes.TrimSpace(line)) > 0 && w.namesA(string(line)) {
					want = append(append(want, line...), '\n')
					lines++
				}
			}
			if got := kept(path); !bytes.Equal(got, want) {
				t.Errorf("the removed lines of %s are not kept as they were:\n%s\nwant\n%s", path, got, want)
			}
		}
	}
	if files < 15 || lines < 12 {
		t.Errorf("the check looked at too little: %d files and %d lines were removed", files, lines)
	}
	var plan forgetPlan
	if err := json.Unmarshal(kept("store/plan.json"), &plan); err != nil {
		t.Fatalf("plan.json: %v", err)
	}
	if !plan.Done || !reflect.DeepEqual(plan.Sessions, []string{w.A}) || len(plan.Remove) < 10 || len(plan.Keep) < 4 || len(plan.Candidates) == 0 || !reflect.DeepEqual(plan.Children, []string{w.childA, "agent_old"}) && !reflect.DeepEqual(plan.Children, []string{w.childA}) {
		t.Errorf("plan.json does not say what was removed, what stayed and why: %+v", plan)
	}
	for _, item := range plan.Remove {
		if item.Why == "" || item.Count == 0 {
			t.Errorf("an item of the plan says no why: %+v", item)
		}
	}
	for _, k := range plan.Keep {
		if k.Why == "" {
			t.Errorf("a kept item says no why: %+v", k)
		}
	}
}

// Without --apply nothing changes, not even a lock file, and the list says what would go, what stays and what only might be the
// session's.
func TestForgetWithoutApplyOnlyListsAndChangesNothing(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	out, err := forgetCommand(t, w.A)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("a listing changed the project: %v", forgetDiff(before, after))
	}
	removed, rest, _ := strings.Cut(out, "Kept:")
	for _, label := range []string{"a1", "a3", "a5", "conv"} {
		if !strings.Contains(removed, w.src[label]) {
			t.Errorf("the list does not name the source %s it would remove:\n%s", label, out)
		}
	}
	for _, label := range []string{"a2", "a4", "a6", "read", "b1", "mention"} {
		if strings.Contains(removed, w.src[label]) {
			t.Errorf("the list would remove the source %s, which stays:\n%s", label, out)
		}
	}
	for _, want := range []string{"Would remove:", "Dry run: nothing was changed. Re-run with --apply", "Candidates", w.src["a2"], w.src["a4"], w.src["a6"], w.tr["la2"]} {
		if !strings.Contains(out, want) {
			t.Errorf("the list lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(rest, w.src["a1"]) {
		t.Errorf("the source %s is listed as kept though it goes", w.src["a1"])
	}
}

func forgetDiff(before, after map[string][]byte) []string {
	var out []string
	for path, b := range before {
		if now, ok := after[path]; !ok || !bytes.Equal(b, now) {
			out = append(out, "changed or gone: "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			out = append(out, "new: "+path)
		}
	}
	return out
}

// A refusal changes nothing: not a record, not a lock file.
func TestForgetRefusesWithoutChangingAnything(t *testing.T) {
	for _, c := range []struct {
		name   string
		setup  func(t *testing.T, w *forgetWorld) func()
		listed bool // a listing, which takes no lock, says it would be refused too
		expect string
	}{
		{"the session this command runs in (Claude Code)", func(t *testing.T, w *forgetWorld) func() { t.Setenv("CLAUDE_CODE_SESSION_ID", w.A); return nil }, true, "is the session this command runs in (CLAUDE_CODE_SESSION_ID)"},
		{"the session this command runs in (Codex)", func(t *testing.T, w *forgetWorld) func() { t.Setenv("CODEX_THREAD_ID", w.A); return nil }, true, "(CODEX_THREAD_ID)"},
		{"the session this command runs in (OpenCode)", func(t *testing.T, w *forgetWorld) func() { t.Setenv("OPENCODE_SESSION_ID", w.A); return nil }, true, "(OPENCODE_SESSION_ID)"},
		{"a session whose agent process is alive", func(t *testing.T, w *forgetWorld) func() { w.runs(w.A); return nil }, true, "is still running: its agent process (claude, pid 910001) is alive"},
		{"a session with no agent process noted that was written to a moment ago", func(t *testing.T, w *forgetWorld) func() {
			w.noProcess(w.A)
			now := time.Now()
			if err := os.Chtimes(filepath.Join(w.root, "sessions", w.A+".json"), now, now); err != nil {
				t.Fatal(err)
			}
			return nil
		}, true, "shikii cannot tell whether the session is still running (no agent process is noted for it"},
		{"a session whose work lock is held", func(t *testing.T, w *forgetWorld) func() {
			unlock, ok := lockPathWithin(filepath.Join(w.root, "sessions", w.A+".json.work.lock"), 0, nil)
			if !ok {
				t.Fatal("precondition: the test takes the lock")
			}
			return unlock
		}, false, "is locked (the session may still be running)"},
		{"two ids that share a file name", func(t *testing.T, w *forgetWorld) func() {
			path := filepath.Join(w.root, "sessions", w.A+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte(`"session_id":"`+w.A+`"`), []byte(`"session_id":"someone.else"`), 1)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, w.old, w.old); err != nil {
				t.Fatal(err)
			}
			return nil
		}, true, "two ids share one file name"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newForgetWorld(t)
			if cleanup := c.setup(t, w); cleanup != nil {
				defer cleanup()
			}
			before := w.snapshot()
			out, err := forgetCommand(t, w.A, "--apply")
			if err == nil || !strings.Contains(err.Error(), c.expect) {
				t.Fatalf("forget was not refused for %q: %v\n%s", c.expect, err, out)
			}
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
			}
			// a listing still shows the plan and says it would be refused
			if listed, err := forgetCommand(t, w.A); c.listed && (err != nil || !strings.Contains(listed, "Would be refused with --apply: ")) {
				t.Errorf("the listing does not say it would be refused: %v\n%s", err, listed)
			}
		})
	}
}

func TestForgetRefusesAnIdThatNamesNothingAndOneThatIsNotAnId(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	for ids, want := range map[string]string{"ses_not_here": "nothing in this project's .shikii/ names ses_not_here", "../escape": "is not a session id", "a b": "is not a session id"} {
		for _, args := range [][]string{{ids}, {ids, "--apply"}, {w.A, ids, "--apply"}} {
			if out, err := forgetCommand(t, args...); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("forget %v: %v\n%s", args, err, out)
			}
		}
	}
	if _, err := forgetCommand(t); err == nil || !strings.Contains(err.Error(), "usage: shikii forget") {
		t.Errorf("no id: %v", err)
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("a refused call changed the project: %v", forgetDiff(before, after))
	}
}

// A session id is taken whole: the id of a session that starts with another's is not taken when the other is given, and the other's own
// sessions go when both are given. A source only the other session's work state or miss named can go with it.
func TestForgetTakesSeveralIdsAndWhatOnlyTheyNamed(t *testing.T) {
	w := newForgetWorld(t)
	out, err := forgetCommand(t, w.A, w.B, "--apply")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	after := w.snapshot()
	for _, path := range []string{"sessions/" + w.A + ".json", "sessions/" + w.B + ".json", "records/wiki-sources/" + w.name + "/" + w.src["a4"] + ".json", "records/wiki-sources/" + w.name + "/" + w.src["a6"] + ".json", "records/wiki-sources/" + w.name + "/" + w.src["b1"] + ".json"} {
		if _, ok := after["store/"+path]; ok {
			t.Errorf("%s is still there: both sessions went, and nothing else names it", path)
		}
	}
	// the source an article cites stays, whoever made it
	if _, ok := after["store/records/wiki-sources/"+w.name+"/"+w.src["a2"]+".json"]; !ok {
		t.Errorf("the cited source went")
	}
	open, err := w.st.OpenCoverage(w.scope)
	if err != nil || len(open) != 0 {
		t.Errorf("the ledger keeps rows of the two sessions: %v %v", open, err)
	}
}

// An id that was only a subagent's takes its deliveries and what was said of them, and nothing of the session it ran under; an id that
// several sessions' subagents used is not taken alone.
func TestForgetTakesASubagentIdAlone(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	out, err := forgetCommand(t, w.childA, "--apply")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	after := w.snapshot()
	gone := forgetDiff(before, after)
	memories := string(after["store/records/memories/"+w.name+".jsonl"])
	if strings.Contains(memories, `"id":"d2d2d2d2d2d2d2d2"`) || !strings.Contains(memories, `"id":"`+w.tA+`"`) || !strings.Contains(memories, `"id":"d1d1d1d1d1d1d1d1"`) {
		t.Errorf("only the subagent's delivery goes, the session's own deliveries stay:\n%s", memories)
	}
	if _, ok := after["store/sessions/"+w.A+".json"]; !ok {
		t.Errorf("the session's own record went with its subagent")
	}
	assessments := 0
	for path := range before {
		if _, ok := after[path]; !ok && strings.Contains(path, "memory-reviews") {
			assessments++
		}
	}
	if assessments != 1 {
		t.Errorf("one assessment (the subagent's) goes: %d\n%v", assessments, gone)
	}

	// the same agent id under another session as well
	w2 := newForgetWorld(t)
	w2.delivery("Bshared", "d5d5d5d5d5d5d5d5", w2.tB, w2.childA, w2.B, "", "")
	w2.ageRecords() // the delivery is as old as the others: a delivery written a moment ago says its agent may still work
	before = w2.snapshot()
	if out, err := forgetCommand(t, w2.childA, "--apply"); err == nil || !strings.Contains(err.Error(), "ran under several sessions") {
		t.Errorf("an agent id of two sessions is taken alone: %v\n%s", err, out)
	}
	if after := w2.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
	}
	// with the session it ran under, the pair is clear
	if out, err := forgetCommand(t, w2.A, w2.childA, "--apply"); err != nil {
		t.Errorf("the session and its subagent: %v\n%s", err, out)
	}
}

// The state the agents' common folder keeps of a session says where its last turn was. A session that was last in another project has
// its records here removed and that state kept: the other project still uses it.
func TestForgetLeavesTheStateOfASessionThatWorkedElsewhere(t *testing.T) {
	w := newForgetWorld(t)
	const c = "ses_fx_c"
	noteSessionRequest(c, "/somewhere/else/.shikii", w.tA)
	w.must(appendGuardRecords(w.root, []guardRecord{{At: w.old.UTC(), Session: c, Turn: "c0c0c0c0c0c0c0c0", Guard: "g", Pass: true}}))
	home, _ := os.UserHomeDir()
	latest := filepath.Join(home, ".shikii", "agent", "latest", c+".json")
	if out, err := forgetCommand(t, c, "--apply"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(latest); err != nil {
		t.Errorf("the state of a session of another project was removed: %v", err)
	}
	if bytes.Contains(forgetRead(t, filepath.Join(w.root, "records", "guards.jsonl")), []byte(c)) {
		t.Errorf("its guard line stayed")
	}
}

func forgetRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A run that stops in the middle is run again and finds the rest: the evidence a step goes by is removed after it.
func TestForgetRunAgainAfterAStopRemovesTheRest(t *testing.T) {
	for _, stop := range []string{"decisions", "wiki", "feedback", "guards", "agent"} {
		t.Run(stop, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			old := forgetAfterStep
			forgetAfterStep = func(step string) error {
				if step == stop {
					return errors.New("the disk went away")
				}
				return nil
			}
			out, err := forgetCommand(t, w.A, "--apply")
			forgetAfterStep = old
			if err == nil || !strings.Contains(err.Error(), "running the same command again removes the rest") {
				t.Fatalf("the run was not stopped at %s: %v\n%s", stop, err, out)
			}
			if _, ok := w.snapshot()["store/sessions/"+w.A+".json"]; !ok {
				t.Fatalf("the session's own record went before the step that removes it")
			}
			if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
				t.Fatalf("the second run: %v\n%s", err, out)
			}
			if problems := w.problems(before); len(problems) > 0 {
				t.Fatalf("after a stop at %s and a second run the project is wrong:\n  %s", stop, strings.Join(problems, "\n  "))
			}
		})
	}
}

// An implementation that goes by the session id alone (the id as text, or a field that holds it or its hash) leaves what only the
// session's turns, deliveries and sources name, takes what merely says the id or starts with it, and closes nothing it opened: the
// checks above name each of those. If they stopped seeing it, a forget as careless as this one would pass them.
func TestTheChecksSeeAForgetThatGoesByTheSessionIdAlone(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	covA := store.CoverageSession(w.A)
	for path, b := range before {
		if strings.HasPrefix(path, "store/records/forgotten/") {
			continue
		}
		full := filepath.Join(w.root, strings.TrimPrefix(path, "store/"))
		if home, ok := strings.CutPrefix(path, "home/"); ok {
			h, _ := os.UserHomeDir()
			full = filepath.Join(h, ".shikii", home)
		}
		switch {
		case strings.HasSuffix(path, ".jsonl"):
			var kept []byte
			for _, line := range bytes.Split(b, []byte("\n")) {
				if len(bytes.TrimSpace(line)) > 0 && !bytes.Contains(line, []byte(w.A)) && !bytes.Contains(line, []byte(covA)) {
					kept = append(append(kept, line...), '\n')
				}
			}
			w.must(os.WriteFile(full, kept, 0o600))
		case strings.Contains(path, w.A) || bytes.Contains(b, []byte(w.A)):
			w.must(os.Remove(full))
		}
	}
	got := strings.Join(w.problems(before), "\n")
	for _, kind := range []string{"a: a record of the session is left", "b: a file that is not the session's is gone", "c: the source a2 was removed", "d: the open rows are"} {
		if !strings.Contains(got, kind) {
			t.Errorf("the checks do not see %q in a forget that goes by the id alone:\n%s", kind, got)
		}
	}
}

// guards.jsonl is written under guards.jsonl.lock like every other records file, so forget can rewrite it without losing a line a Stop
// appends meanwhile.
func TestGuardRecordsAreAppendedUnderTheFilesLock(t *testing.T) {
	root := t.TempDir()
	st := store.Store{Dir: root}
	path := guardRecordsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	held := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = st.WithAppendLock(path, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	go func() {
		done <- appendGuardRecords(root, []guardRecord{{At: time.Now().UTC(), Session: "s", Turn: "t", Guard: "g", Pass: true}})
	}()
	select {
	case err := <-done:
		t.Fatalf("a line was appended while the file's lock was held (%v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the append did not go on after the lock was released")
	}
	if got := forgetRead(t, path); !bytes.Contains(got, []byte(`"session_id":"s"`)) {
		t.Errorf("the line is not there: %s", got)
	}
}

func TestUsageListsForget(t *testing.T) {
	entry := helpEntry(t, "shikii forget")
	for _, want := range []string{"<session id>...", "--apply", ".shikii/records/forgotten/", "an agent does not remove records on its own"} {
		if !strings.Contains(entry, want) {
			t.Errorf("shikii --help does not say %q of forget:\n%s", want, entry)
		}
	}
}
