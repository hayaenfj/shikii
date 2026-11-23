package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The tests here hold the parts of shikii forget that the first tests of it did not hold (the 0.41.0 audit): each one gives a case where
// one condition of the code is the only thing that keeps or removes a record.

func (w *forgetWorld) fileExists(rel string) bool {
	_, err := os.Stat(filepath.Join(w.root, rel))
	return err == nil
}

func (w *forgetWorld) fileHas(rel, text string) bool {
	return bytes.Contains(forgetRead(w.t, filepath.Join(w.root, rel)), []byte(text))
}

func (w *forgetWorld) forgetA() string {
	w.t.Helper()
	out, err := forgetCommand(w.t, w.A, "--apply")
	if err != nil {
		w.t.Fatalf("%v\n%s", err, out)
	}
	return out
}

// The original text of a decision (records/text) goes only when no decision line of any question holds the trace. A decision of the
// session that another question holds too, where a person labeled it, stays there, so its text stays with it.
func TestForgetKeepsTheTextOfADecisionAnotherQuestionStillHolds(t *testing.T) {
	w := newForgetWorld(t)
	pend := w.tr["pend"] // a decision of the session's pending list, of the question needs-plan
	w.decision("lesson-applies", pend, "ui", false)
	w.forgetA()
	if w.fileHas("records/needs-plan.jsonl", pend) {
		t.Errorf("the session's decision was not removed from its question")
	}
	if !w.fileHas("records/lesson-applies.jsonl", pend) || !w.fileHas("labels/lesson-applies.jsonl", pend) {
		t.Errorf("the decision a person labeled in another question lost its line or its label")
	}
	if !w.fileExists("records/text/" + pend + ".json") {
		t.Errorf("the original text of a decision that a question still holds was removed")
	}
	if w.fileExists("records/text/" + w.tr["la1"] + ".json") {
		t.Errorf("the original text of a decision no question holds stayed")
	}
}

// A line of a decisions file that names no decision may be any decision, so no original text is taken as unused: the decisions of the
// session go, their texts stay, and the list says so.
func TestForgetKeepsTheTextsWhenADecisionLineNamesNoDecision(t *testing.T) {
	w := newForgetWorld(t)
	file, err := os.OpenFile(filepath.Join(w.root, "records", "lesson-applies.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"spec_id":"lesson-applies","note":"a line with no trace_id"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	out := w.forgetA()
	if w.fileHas("records/lesson-applies.jsonl", w.tr["la1"]) || w.fileHas("records/needs-plan.jsonl", w.tr["pend"]) {
		t.Errorf("the decisions of the session were not removed")
	}
	for _, trace := range []string{w.tr["la1"], w.tr["pend"]} {
		if !w.fileExists("records/text/" + trace + ".json") {
			t.Errorf("the original text of the decision %s was removed though a line names no decision", trace)
		}
	}
	if !strings.Contains(out, "a line in a decisions file names no decision") {
		t.Errorf("the list does not say why the texts stay:\n%s", out)
	}
}

// A provisional score of a conversation goes when the decision it scores is one the session's own records wait on, or one a delivery to
// the session quoted, even when the score's source names no turn or session of the session. Another score stays.
func TestForgetTakesTheScoresOfTheDecisionsOfTheSession(t *testing.T) {
	w := newForgetWorld(t)
	score := func(trace string) {
		_, err := w.st.SaveReview(store.Review{TraceID: trace, Spec: "done-as-asked", Scope: w.scope, InputsHash: "h", Truth: "yes", Reason: "fixture",
			Source: store.Feedback{ID: "e5e5e5e5e5e5e5e5", Request: "r", Summary: "s", Reply: "p"}})
		w.must(err)
	}
	other := "c9c9c9c9c9c9c9c9"
	score(w.tr["pend"]) // the decision A's own record waits on
	score(w.tr["la1"])  // the decision a delivery to A quoted
	score(other)        // nobody's of the session
	w.forgetA()
	scores := string(forgetRead(t, filepath.Join(w.root, "records", "reviews", "done-as-asked.jsonl")))
	for label, trace := range map[string]string{"waited on by the session's record": w.tr["pend"], "quoted by a delivery to the session": w.tr["la1"]} {
		if strings.Contains(scores, trace) {
			t.Errorf("the score of the decision %s stayed", label)
		}
	}
	if !strings.Contains(scores, other) || !strings.Contains(scores, w.tr["rvb"]) {
		t.Errorf("a score that is not the session's was removed:\n%s", scores)
	}
}

// The list of what a session was told about a folder (told.json) is shared: forgetting a session takes its element and leaves the
// elements of the others, which are kept beside the other things that went.
func TestForgetTakesOnlyTheElementOfTheSessionFromWhatWasTold(t *testing.T) {
	w := newForgetWorld(t)
	key := func(id string) string {
		sum := sha256.Sum256([]byte(id + "\n" + w.scope))
		return hex.EncodeToString(sum[:8])
	}
	told := func(files map[string][]byte, path string) []string {
		var out []string
		if err := json.Unmarshal(files[path], &out); err != nil {
			t.Fatalf("%s is not a list: %v", path, err)
		}
		return out
	}
	if got := told(w.snapshot(), "home/agent/told.json"); !reflect.DeepEqual(got, []string{key(w.A), key(w.B)}) {
		t.Fatalf("precondition: both sessions were told: %v", got)
	}
	w.forgetA()
	after := w.snapshot()
	if got := told(after, "home/agent/told.json"); !reflect.DeepEqual(got, []string{key(w.B)}) {
		t.Errorf("told.json holds %v, want only the element of the other session", got)
	}
	var kept []string
	for path := range after {
		if strings.HasPrefix(path, "store/records/forgotten/") && strings.HasSuffix(path, "/home/.shikii/agent/told.json") {
			kept = told(after, path)
		}
	}
	if !reflect.DeepEqual(kept, []string{key(w.A)}) {
		t.Errorf("the run keeps %v of told.json, want only the element that went", kept)
	}
}

// What a person was shown is what a yes removes: when the records changed while they decided, so that any one part of the plan (what
// goes, with the names of it, what stays, what only might belong) is not what was shown, nothing is removed. Each case changes one part
// and no other.
func TestMCPForgetStopsWhenAnyPartOfThePlanChangedWhileAsking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(w *forgetWorld)
	}{
		{"which sources go (the same number of them)", func(w *forgetWorld) {
			// the session's record now names another source in place of a1: one source goes in both plans, under another name
			swapped := w.observe("a7", "made by A: another note that nothing cites")
			path := filepath.Join(w.root, "sessions", w.A+".json")
			w.must(os.WriteFile(path, bytes.ReplaceAll(forgetRead(w.t, path), []byte(w.src["a1"]), []byte(swapped)), 0o600))
			w.must(os.Chtimes(path, w.old, w.old))
		}},
		{"why a decision stays", func(w *forgetWorld) {
			w.must(w.st.AppendLabel(store.Label{TraceID: w.tr["la2"], SpecID: "lesson-applies", SpecHash: "h", Type: "noul", Model: "m", Truth: "yes", Weight: 1, Source: "manual", At: time.Now().UTC()}))
		}},
		{"what only might belong", func(w *forgetWorld) {
			w.write(filepath.Join("wiki", "lessons", w.name, "00000000000000cc.md"), []byte("an article that says "+w.A+"\n"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newForgetWorld(t)
			c := newLiveMCP(t, w.mcpHandler())
			c.initialize(map[string]any{"elicitation": map[string]any{}})
			c.callForget(2, forgetArgs(w, nil))
			q := c.question()
			tc.change(w)
			before := w.snapshot()
			c.send(answerTo(q, accept(true)))
			failed, body := c.reply(2)
			if !failed || body["status"] != "changed" || body["changed"] != false || !strings.Contains(body["message"].(string), "list it again") {
				t.Fatalf("a yes to a plan that is no longer the one shown was taken (%v): %v", body["status"], body["message"])
			}
			c.finish()
			if after := w.snapshot(); len(withoutLockFiles(forgetDiff(before, after))) > 0 {
				t.Errorf("the project changed: %v", forgetDiff(before, after))
			}
		})
	}
}

// A late answer to a question that was withdrawn is no answer to the next one: the next question has an id of its own, and an answer
// with the first id, which would be a yes, is passed over while the next question stays open.
func TestMCPForgetTakesNoLateAnswerOfAnEarlierQuestion(t *testing.T) {
	old := mcpAskLimit
	mcpAskLimit = 200 * time.Millisecond
	t.Cleanup(func() { mcpAskLimit = old })
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	first := c.question()
	if m := c.next(); m["method"] != "notifications/cancelled" {
		t.Fatalf("the first question was not withdrawn: %v", m)
	}
	if _, body := c.reply(2); body["status"] != "unanswered" {
		t.Fatalf("the first call did not end unanswered: %v", body)
	}
	mcpAskLimit = old // the second question waits as long as it is given
	c.callForget(3, forgetArgs(w, nil))
	second := c.question()
	if first["id"] == second["id"] {
		t.Fatalf("two questions carry the id %v", first["id"])
	}
	c.send(answerTo(first, accept(true))) // late: for the first question
	c.pinged(4)                           // nothing was written for it: the second question is still open
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("a late answer to the first question changed the project: %v", forgetDiff(before, after))
	}
	c.send(answerTo(second, map[string]any{"action": "decline"}))
	if _, body := c.reply(3); body["status"] != "declined" {
		t.Errorf("the second question did not take its own answer: %v", body)
	}
	c.finish()
}

// The client cancels a call by its id: a cancellation of another call, of the same number as text, or one that names no call leaves the
// question open.
func TestMCPForgetKeepsAskingWhenTheCancelledCallIsAnother(t *testing.T) {
	for name, params := range map[string]map[string]any{
		"another number":              {"requestId": 99},
		"the same number as a string": {"requestId": "2"},
		"no id":                       {"reason": "no call is named"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			c := newLiveMCP(t, w.mcpHandler())
			c.initialize(map[string]any{"elicitation": map[string]any{}})
			c.callForget(2, forgetArgs(w, nil))
			q := c.question()
			c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": params})
			c.pinged(3) // the question was not withdrawn: no notice and no answer to the call came first
			c.send(answerTo(q, map[string]any{"action": "decline"}))
			if _, body := c.reply(2); body["status"] != "declined" {
				t.Errorf("the question did not stay open: %v", body)
			}
			c.finish()
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Errorf("the project changed: %v", forgetDiff(before, after))
			}
		})
	}
}

// A session may still be running when any record of it was written within ten minutes, not only its own record: its guard lines, the
// deliveries to it and to its subagents, its conversations, the reasoning history and the state of its last turn are written as it
// works. A session whose own record is gone leaves only those.
func TestForgetRefusesASessionAnyOfWhoseRecordsWasWrittenLately(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name  string
		id    func(w *forgetWorld) string // the id given to forget (the session's by default)
		touch func(w *forgetWorld)
	}{
		{"a guard line", nil, func(w *forgetWorld) {
			w.must(appendGuardRecords(w.root, []guardRecord{{At: now, Session: w.A, Turn: w.tA, Guard: "g", Pass: true}}))
		}},
		{"a delivery to the session", nil, func(w *forgetWorld) {
			w.delivery("fresh", "f2f2f2f2f2f2f2f2", w.tA, "", w.A, "", "")
		}},
		{"a delivery to its subagent, when only the subagent is given", func(w *forgetWorld) string { return w.childA }, func(w *forgetWorld) {
			w.delivery("fresh", "f3f3f3f3f3f3f3f3", w.tA, w.childA, w.A, "", "")
		}},
		{"a conversation", nil, func(w *forgetWorld) {
			w.must(w.st.AppendFeedback(w.scope, store.Feedback{ID: "f4f4f4f4f4f4f4f4", Request: "r", Summary: "s", Reply: "p", Session: w.A, At: now}))
		}},
		{"the reasoning history", nil, func(w *forgetWorld) {
			w.writeJSON("records/work-reasoning-"+strings.Repeat("a", 64)+".json", map[string]any{"store": w.name, "session": w.A, "turn": w.tA0, "entry": map[string]any{"key": "z", "at": now}})
		}},
		{"the state of its last turn", nil, func(w *forgetWorld) {
			home, _ := os.UserHomeDir()
			path := filepath.Join(home, ".shikii", "agent", "latest", w.A+".json")
			w.must(os.Chtimes(path, now, now))
		}},
		{"a guard line, when the session's own record is gone", nil, func(w *forgetWorld) {
			for _, suffix := range []string{".json", ".files", ".lines", ".json.lock", ".json.work.lock"} {
				w.must(os.Remove(filepath.Join(w.root, "sessions", w.A+suffix)))
			}
			w.must(appendGuardRecords(w.root, []guardRecord{{At: now, Session: w.A, Turn: w.tA, Guard: "g", Pass: true}}))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newForgetWorld(t)
			tc.touch(w)
			id := w.A
			if tc.id != nil {
				id = tc.id(w)
			}
			before := w.snapshot()
			out, err := forgetCommand(t, id, "--apply")
			if err == nil || !strings.Contains(err.Error(), "may still be running") {
				t.Fatalf("a session with a record written a moment ago was not refused: %v\n%s", err, out)
			}
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Errorf("a refusal changed the project: %v", forgetDiff(before, after))
			}
		})
	}
}

// The session that asks is the one forget must never remove, and only the call can say which it is (the server's own environment is the
// one of whoever started it). A call that does not say is refused, with how to say it, before any question is sent.
func TestMCPForgetNeedsTheSessionThatAsks(t *testing.T) {
	for name, work := range map[string]any{
		"no work":                    nil,
		"an empty work":              map[string]any{},
		"work without a session id":  map[string]any{"turn_id": "t"},
		"an empty session id":        map[string]any{"session_id": ""},
		"a session id that is no id": map[string]any{"session_id": "a b"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			c := newLiveMCP(t, w.mcpHandler())
			c.initialize(map[string]any{"elicitation": map[string]any{}})
			args := map[string]any{"operation": "forget", "sessions": []string{w.A}}
			if work != nil {
				args["work"] = work
			}
			c.callForget(2, args)
			failed, body := c.reply(2) // the next message is the answer: no question was sent
			message, _ := body["error"].(string)
			if !failed || !strings.Contains(message, "work.session_id") || !strings.Contains(message, "your own session") || !strings.Contains(message, "nothing was changed") {
				t.Errorf("a call that does not name its session was not refused with how to name it: %v", body)
			}
			c.finish()
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Errorf("the project changed: %v", forgetDiff(before, after))
			}
		})
	}
}

// Other sessions' hooks give up on the wiki's locks and the memory's after two seconds, so forget holds each only while the step that
// needs it runs. After every step a hook of another session must be able to take both.
func TestForgetHoldsTheWikiAndMemoryLocksOnlyInTheStepsThatNeedThem(t *testing.T) {
	w := newForgetWorld(t)
	var free []string
	old := forgetAfterStep
	forgetAfterStep = func(step string) error {
		unlockWiki, err := w.book.LockForForget()
		if err != nil {
			return fmt.Errorf("a hook cannot take the wiki's locks after the step %s: %w", step, err)
		}
		unlockWiki()
		unlockMemory, err := w.st.LockMemory(w.scope)
		if err != nil {
			return fmt.Errorf("a hook cannot take the memory's lock after the step %s: %w", step, err)
		}
		unlockMemory()
		free = append(free, step)
		return nil
	}
	defer func() { forgetAfterStep = old }()
	before := w.snapshot()
	out, err := forgetCommand(t, w.A, "--apply")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	steps := []string{}
	for _, s := range (&forgetRun{}).steps() {
		steps = append(steps, s.name)
	}
	if !reflect.DeepEqual(free, steps) {
		t.Errorf("the locks were taken after the steps %v, want all of %v", free, steps)
	}
	if problems := w.problems(before); len(problems) > 0 {
		t.Errorf("forget left the project wrong:\n  %s", strings.Join(problems, "\n  "))
	}
}

// A step that needs a lock takes it: while another holder has it the step is not run, what the steps before it removed stays removed,
// and the same command run again after the holder is gone removes the rest.
func TestForgetStepsTakeTheLocksTheyNeed(t *testing.T) {
	for _, tc := range []struct {
		name, step, busy string
		hold             func(w *forgetWorld) (func(), error)
	}{
		{"the wiki's", "wiki", "wiki is busy", func(w *forgetWorld) (func(), error) { return w.book.LockForForget() }},
		{"the memory's", "assessments", "the memory record is busy", func(w *forgetWorld) (func(), error) { return w.st.LockMemory(w.scope) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			release, err := tc.hold(w)
			if err != nil {
				t.Fatal(err)
			}
			out, err := forgetCommand(t, w.A, "--apply")
			if err == nil || !strings.Contains(err.Error(), tc.step+": ") || !strings.Contains(err.Error(), tc.busy) || !strings.Contains(err.Error(), "running the same command again removes the rest") {
				t.Fatalf("a step run while its lock is held by another: %v\n%s", err, out)
			}
			if !w.fileHas("records/lesson-applies.jsonl", w.tr["lb"]) || w.fileHas("records/needs-plan.jsonl", w.tr["pend"]) {
				t.Errorf("the step before the one that stopped did not run")
			}
			release()
			if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
				t.Fatalf("the second run: %v\n%s", err, out)
			}
			if problems := w.problems(before); len(problems) > 0 {
				t.Errorf("after the stop and a second run the project is wrong:\n  %s", strings.Join(problems, "\n  "))
			}
		})
	}
}

// The plan was listed before the step took its lock, and the records the lock protects may change in between: a step that finds its part
// of the plan changed is not run, and says so. Here an article starts to cite a source the plan removes.
func TestForgetStopsBeforeAStepWhoseRecordsChangedAfterThePlan(t *testing.T) {
	w := newForgetWorld(t)
	old := forgetAfterStep
	forgetAfterStep = func(step string) error {
		if step == "memories" { // the step before the wiki's
			w.write(filepath.Join("wiki", "lessons", w.name, "00000000000000dd.md"), []byte("an article that cites "+w.src["a1"]+"\n"))
		}
		return nil
	}
	defer func() { forgetAfterStep = old }()
	out, err := forgetCommand(t, w.A, "--apply")
	if err == nil || !strings.Contains(err.Error(), "wiki: what this step would remove changed after the plan was made") || !strings.Contains(err.Error(), "running the same command again") {
		t.Fatalf("a step whose records changed was run: %v\n%s", err, out)
	}
	for _, label := range []string{"a1", "a3", "a5"} {
		if !w.fileExists("records/wiki-sources/" + w.name + "/" + w.src[label] + ".json") {
			t.Errorf("the source %s went though the wiki step was not run", label)
		}
	}
	forgetAfterStep = old
	if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
		t.Fatalf("the second run: %v\n%s", err, out)
	}
	if !w.fileExists("records/wiki-sources/"+w.name+"/"+w.src["a1"]+".json") || w.fileExists("records/wiki-sources/"+w.name+"/"+w.src["a3"]+".json") {
		t.Errorf("the second run did not go by the records as they were then: the cited source goes or the others stay")
	}
}
