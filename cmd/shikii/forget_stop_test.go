package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The second audit of 0.41.0 (audit-2.md): what an application that stops part-way says over MCP, a step with nothing to remove, and
// the two conditions of the code that the tests did not hold (the memory lock of the memories step, the plan that an application writes).

// holdAfterStep makes the step hold a lock that another agent's hook could hold: after the step before it has run, take it, and give it
// up at the end of the test or when release is called.
func holdAfterStep(t *testing.T, after string, take func() (func(), error)) (release func()) {
	t.Helper()
	var unlock func()
	taken := false // another agent holds it once: a run after release is not held again
	release = func() {
		if unlock != nil {
			unlock()
			unlock = nil
		}
	}
	old := forgetAfterStep
	forgetAfterStep = func(step string) error {
		if step == after && !taken {
			u, err := take()
			if err != nil {
				return err
			}
			unlock, taken = u, true
		}
		return nil
	}
	t.Cleanup(func() { forgetAfterStep = old; release() })
	return release
}

// A person said yes, and the removal then stopped in a step (another agent held the wiki's lock): records were already moved, so the
// reply must not say that nothing changed. It says partial with changed true, what was moved, what is left, and that the same call
// again (which asks the person again) removes the rest. Run again, it removes the rest.
func TestMCPForgetSaysPartialWhenTheRemovalStopsAfterTheYes(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	release := holdAfterStep(t, "memories", w.book.LockForForget) // the step after it, the wiki's, finds the lock taken
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	c.send(answerTo(c.question(), accept(true)))
	failed, body := c.reply(2)
	message, _ := body["message"].(string)
	if !failed || body["status"] != "partial" || body["changed"] != true {
		t.Fatalf("a removal that stopped after the yes did not say partial and changed: %v", body)
	}
	for _, want := range []string{"the person agreed", "Send the same call again", "asks the person again", "records/forgotten/"} {
		if !strings.Contains(message, want) {
			t.Errorf("the message does not say %q: %s", want, message)
		}
	}
	left, _ := body["steps_left"].([]any)
	if len(left) == 0 || left[0] != "wiki" || !reflect.DeepEqual(left[len(left)-1], "agent") {
		t.Errorf("steps_left is not the wiki step and those after it: %v", left)
	}
	plan, _ := body["plan"].(map[string]any)
	if removed, _ := plan["remove"].([]any); len(removed) == 0 {
		t.Errorf("the reply does not say what was removed: %v", body)
	}
	if after := w.snapshot(); reflect.DeepEqual(before, after) {
		t.Errorf("the project did not change, so the premise of the test is wrong")
	}
	c.finish()

	// the lock is free again: the same call, and the person's yes again, removes the rest
	release()
	c = newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	c.send(answerTo(c.question(), accept(true)))
	if failed, body := c.reply(2); failed || body["status"] != "removed" {
		t.Fatalf("the same call again did not remove the rest: %v", body)
	}
	c.finish()
	if problems := w.problems(before); len(problems) > 0 {
		t.Errorf("after the stop and the second call the project is wrong:\n  %s", strings.Join(problems, "\n  "))
	}
}

// A failure before anything was moved still says failed with changed false: no plan was applied, no folder was made.
func TestMCPForgetSaysFailedWhenNothingWasMoved(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	q := c.question()
	// a session lock another agent holds when the person says yes: refused before the first step
	unlock, ok := lockPathWithin(filepath.Join(w.root, "sessions", w.A+".json.work.lock"), 0, nil)
	if !ok {
		t.Fatal("precondition: the test takes the lock")
	}
	defer unlock()
	c.send(answerTo(q, accept(true)))
	failed, body := c.reply(2)
	if !failed || body["status"] != "failed" || body["changed"] != false {
		t.Errorf("a removal that did not begin did not say failed with nothing changed: %v", body)
	}
	c.finish()
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

// A step that the plan found nothing to remove in does not take its lock: a subagent id alone has nothing to remove in the wiki, so the
// wiki's lock held by another agent does not stop it, and a second run is never needed there.
func TestForgetDoesNotTakeTheLockOfAStepThatRemovesNothing(t *testing.T) {
	w := newForgetWorld(t)
	release, err := w.book.LockForForget()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var asked []string
	old := forgetAfterStep
	forgetAfterStep = func(step string) error { asked = append(asked, step); return nil }
	defer func() { forgetAfterStep = old }()
	out, err := forgetCommand(t, w.childA, "--apply")
	if err != nil {
		t.Fatalf("a subagent id alone was stopped by the wiki's lock, though the wiki has nothing of it to remove: %v\n%s", err, out)
	}
	if len(asked) != len((&forgetRun{}).steps()) {
		t.Errorf("the steps that ran: %v", asked)
	}
	// the session's own run still takes the wiki's lock, since it has something to remove there
	w2 := newForgetWorld(t)
	release2, err := w2.book.LockForForget()
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	if _, err := forgetCommand(t, w2.A, "--apply"); err == nil || !strings.Contains(err.Error(), "wiki: ") || !strings.Contains(err.Error(), "wiki is busy") {
		t.Errorf("a step that removes something ran without its lock: %v", err)
	}
}

// The memories step takes the memory's lock: a delivery is written under it, after a check that no line of the same id is there, so
// that check and the removal of lines must not overlap. A hook of another session that holds it stops the step, and the steps before
// stay done.
func TestForgetMemoriesStepTakesTheMemoryLock(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	release := holdAfterStep(t, "assessments", func() (func(), error) { return w.st.LockMemory(w.scope) })
	out, err := forgetCommand(t, w.A, "--apply")
	if err == nil || !strings.Contains(err.Error(), "memories: ") || !strings.Contains(err.Error(), "the memory record is busy") || !strings.Contains(err.Error(), "running the same command again removes the rest") {
		t.Fatalf("the memories step ran while the memory's lock was held by another: %v\n%s", err, out)
	}
	if !w.fileHas("records/memories/"+w.name+".jsonl", w.tA) {
		t.Errorf("the memories step removed lines it had no lock for")
	}
	release()
	if out, err := forgetCommand(t, w.A, "--apply"); err != nil {
		t.Fatalf("the second run: %v\n%s", err, out)
	}
	if problems := w.problems(before); len(problems) > 0 {
		t.Errorf("after the stop and a second run the project is wrong:\n  %s", strings.Join(problems, "\n  "))
	}
}

// The plan an application writes (plan.json, the plan the person's reply carries) is the plan of the listing: a step that reads its own
// records again under its lock, to see they are what the plan said, must take that reading out of the plan again. Left in, each item of
// the steps that take a lock is there twice and the counts are doubled.
func TestForgetTheAppliedPlanIsTheListedPlan(t *testing.T) {
	w := newForgetWorld(t)
	list := &forgetRun{root: w.root, scope: w.scope, st: w.st, book: w.book, ids: []string{w.A}}
	if err := list.execute(false, ""); err != nil {
		t.Fatal(err)
	}
	apply := &forgetRun{root: w.root, scope: w.scope, st: w.st, book: w.book, ids: []string{w.A}}
	if err := apply.execute(true, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := forgetCanon(apply.plan.Remove), forgetCanon(list.plan.Remove); got != want {
		t.Errorf("what the application lists as removed is not what the listing said:\n%s\n--- listed:\n%s", got, want)
	}
	var saved forgetPlan
	data, err := os.ReadFile(filepath.Join(apply.f.Dir, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Done || forgetCanon(saved.Remove) != forgetCanon(list.plan.Remove) {
		t.Errorf("plan.json says done=%v and removes:\n%s\n--- listed:\n%s", saved.Done, forgetCanon(saved.Remove), forgetCanon(list.plan.Remove))
	}
}
