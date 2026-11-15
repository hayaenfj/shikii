package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests run the OpenCode plugin itself (opencode_plugin/index.ts) under node, with a fake ctx standing in for OpenCode and a fake
// shikii (a small shell script that writes down what it was given and answers with prepared JSON). The text of the plugin is checked
// in opencode_plugin_test.go; what it does over a conversation (the brief on every request, the folder it runs in, the continuation
// after a stop block, subagents, failures) can only be seen by running it. Nothing of OpenCode or of a real shikii is started: node
// runs one script, and the fake shikii is a file in a temporary folder. Without node 24 or newer (it strips the types of a .ts file by
// itself; CI's Go job installs node 26) these tests are skipped on a machine, and fail when the environment variable CI is set: a
// check that goes away without a sound would leave CI green without it.

// pluginDriver is the stand-in for OpenCode: it loads the plugin once per project folder (OpenCode 2.3.0 calls setup per location, runs
// the prompt and context hooks only in the instance of the session's location, and tells every instance's ctx.event.subscribe the
// events of every session on the server), gives each instance a ctx, and plays the steps of one scenario. The plugin does not wait for
// one event before it takes the next, so "settled" is found by looking at the work that is left: a started shikii that has not
// finished, a call into ctx that has not answered, an event nobody took yet.
const pluginDriver = `
import fs from "node:fs"
import cp from "node:child_process"
import { syncBuiltinESMExports } from "node:module"
import { pathToFileURL } from "node:url"
const [pluginPath, scenarioPath] = process.argv.slice(2)
const scenario = JSON.parse(fs.readFileSync(scenarioPath, "utf8"))

let active = 0 // started shikii processes that have not finished
const realSpawn = cp.spawn
cp.spawn = (...args) => {
  active++
  const child = realSpawn(...args)
  let done = false
  const finish = () => { if (!done) { done = true; active-- } }
  child.on("close", finish)
  child.on("error", finish)
  return child
}
syncBuiltinESMExports()
const plugin = (await import(pathToFileURL(pluginPath).href)).default

let pending = 0 // calls into ctx that have not answered
const track = async (fn) => { pending++; try { return await fn() } finally { pending-- } }

const subscribers = []
function subscribe() {
  const sub = { queue: [], waiter: null }
  subscribers.push(sub)
  return {
    [Symbol.asyncIterator]() {
      return {
        next() {
          if (sub.queue.length > 0) return Promise.resolve({ value: sub.queue.shift(), done: false })
          return new Promise((resolve) => { sub.waiter = resolve })
        },
      }
    },
  }
}
function broadcast(ev) {
  for (const sub of subscribers) {
    if (sub.waiter) { const w = sub.waiter; sub.waiter = null; w({ value: ev, done: false }) } else sub.queue.push(ev)
  }
}
const idle = () => pending === 0 && active === 0 && subscribers.every((s) => s.queue.length === 0)
const tick = () => new Promise((resolve) => setImmediate(resolve))
async function settle() {
  for (;;) {
    await tick()
    if (idle()) { await tick(); if (idle()) return }
  }
}

const prompts = []
const contexts = []
const errors = []
const sessions = scenario.sessions ?? {}
const locations = scenario.locations ?? { main: scenario.directory }
const instances = {}
for (const [name, dir] of Object.entries(locations)) {
  const hooks = {}
  instances[name] = hooks
  const ctx = {
    location: scenario.noLocation ? undefined : { directory: dir },
    session: {
      hook(n, fn) { hooks[n] = fn },
      get: ({ sessionID }) => track(async () => sessions[sessionID] ?? { id: sessionID }),
      context: ({ sessionID }) => track(async () => scenario.messages?.[sessionID] ?? []),
      prompt: (arg) => track(async () => {
        prompts.push(arg)
        // OpenCode runs the prompt hook for a prompt the plugin sends too
        await hooks.prompt({ sessionID: arg.sessionID, prompt: { text: arg.text }, metadata: arg.metadata })
      }),
    },
    event: { subscribe() { return subscribe() } },
  }
  await plugin.setup(ctx)
}
const first = Object.keys(locations)[0]
const stateDir = scenario.stateDir

for (const step of scenario.steps) {
  try {
    const hooks = instances[step.at ?? first]
    if (step.op === "prompt") {
      await hooks.prompt({ sessionID: step.sid, prompt: { text: step.text }, metadata: step.metadata })
    } else if (step.op === "context") {
      const e = { sessionID: step.sid, system: [{ type: "text", text: "BASE" }] }
      await hooks.context(e)
      contexts.push(e.system.map((s) => s.text))
    } else if (step.op === "succeeded" || step.op === "compaction" || step.op === "subagent") {
      if (step.op === "succeeded") broadcast({ type: "session.execution.succeeded", data: { sessionID: step.sid } })
      if (step.op === "compaction") broadcast({ type: "session.compaction.ended", data: { sessionID: step.sid } })
      if (step.op === "subagent") {
        // measured on 2.3.0: the report of a finished subagent is put into the parent's inbox and wakes the parent; no prompt hook runs
        broadcast({ type: "session.inbox.enqueued", data: { sessionID: step.sid, inboxID: "inbox", item: { type: "synthetic", delivery: "steer", payload: { text: "<subagent/>", metadata: { source: "subagent", childID: "child", agent: "subagent", state: "completed" } } } } })
      }
      if (!step.nowait) await settle()
    } else if (step.op === "event") {
      // any other event of OpenCode's, with the session and what the event adds (an interrupted turn's reason, a failure's error)
      broadcast({ type: step.type, data: { sessionID: step.sid, ...(step.data ?? {}) } })
      if (!step.nowait) await settle()
    } else if (step.op === "move") {
      // session.move (POST /api/session/:id/move on 2.3.0): the session is in another project folder, ctx.session.get answers the new one
      // from then on, and session.moved is told to every instance
      sessions[step.sid] = { ...(sessions[step.sid] ?? { id: step.sid }), location: { directory: step.dir } }
      broadcast({ type: "session.moved", data: { sessionID: step.sid } })
      await settle()
    } else if (step.op === "relocate") {
      // measured on 2.3.0: the session is in the new folder (ctx.session.get answers it) while session.moved has not reached the plugins yet;
      // the model call right after a move is made in this window
      sessions[step.sid] = { ...(sessions[step.sid] ?? { id: step.sid }), location: { directory: step.dir } }
    } else if (step.op === "settle") {
      await settle()
    } else if (step.op === "release") {
      fs.writeFileSync(stateDir + "/release-" + step.event + "-" + step.n, "")
    } else if (step.op === "waitCalls") {
      // wait until the fake shikii has been started step.count times for step.event (it writes its line when it starts)
      for (let i = 0; ; i++) {
        const lines = fs.existsSync(stateDir + "/calls.jsonl") ? fs.readFileSync(stateDir + "/calls.jsonl", "utf8").split("\n").filter((l) => l.includes('"event":"' + step.event + '"')) : []
        if (lines.length >= step.count) break
        if (i > 500) throw new Error("the fake shikii was not started " + step.count + " times for " + step.event)
        await new Promise((resolve) => setTimeout(resolve, 20))
      }
    }
  } catch (err) {
    errors.push(String(err))
  }
}
console.log(JSON.stringify({ prompts, contexts, errors }))
process.exit(0)
`

type pluginCall struct {
	Event string
	N     int
	Pwd   string
	Args  string
	Stdin map[string]any
}

type pluginRun struct {
	Prompts []struct {
		SessionID string
		Text      string
		Metadata  map[string]any
	}
	Contexts [][]string
	Errors   []string
}

// pluginBox is one scenario's folders: the plugin made from the embedded text, the fake shikii and the project's folder.
type pluginBox struct {
	dir, project, shikii string
	plugin, driver       string
}

// pluginNodeProblem says why the plugin cannot be run under node here ("" when it can).
func pluginNodeProblem() string {
	node, err := exec.LookPath("node")
	if err != nil {
		return "node is not installed"
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		return "node does not run"
	}
	major, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), ".", 2)[0])
	if major < 24 {
		return fmt.Sprintf("node %s is older than 24: it does not strip types by itself", strings.TrimSpace(string(out)))
	}
	return ""
}

// pluginTestsUnavailable ends a test that cannot run the plugin: a skip on a machine, a failure when ci (the CI variable) is not empty.
func pluginTestsUnavailable(t interface {
	Helper()
	Fatalf(string, ...any)
	Skipf(string, ...any)
}, ci, why string) {
	t.Helper()
	if ci != "" {
		t.Fatalf("CI must run the plugin tests, and they cannot run: %s", why)
		return // a real Fatalf stops the test; the stand-in used by the test of this function does not
	}
	t.Skipf("%s: the plugin is not run", why)
}

func newPluginBox(t *testing.T) *pluginBox {
	t.Helper()
	if why := pluginNodeProblem(); why != "" {
		pluginTestsUnavailable(t, os.Getenv("CI"), why)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := &pluginBox{dir: dir, project: filepath.Join(dir, "project"), shikii: filepath.Join(dir, "fake-shikii")}
	for _, d := range []string{b.project, filepath.Join(dir, "state")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(dir, "state")
	script := `#!/bin/sh
d='` + state + `'
event="$2"
n=$(cat "$d/count-$event" 2>/dev/null || echo 0)
n=$((n+1))
echo "$n" > "$d/count-$event"
input=$(cat)
printf '{"event":"%s","n":%s,"pwd":"%s","args":"%s","stdin":%s}\n' "$event" "$n" "$(pwd -P)" "$*" "${input:-null}" >> "$d/calls.jsonl"
if [ -f "$d/hold-$event-$n" ]; then i=0; while [ ! -f "$d/release-$event-$n" ] && [ $i -lt 500 ]; do sleep 0.02; i=$((i+1)); done; fi
if [ -f "$d/fail-$event-$n" ]; then exit 1; fi
case "$(cat "$d/mode" 2>/dev/null)" in
exit1) exit 1 ;;
garbage) echo 'not json {'; exit 0 ;;
esac
if [ -f "$d/out-$event-$n" ]; then cat "$d/out-$event-$n"; elif [ -f "$d/out-$event" ]; then cat "$d/out-$event"; fi
`
	writeFileT(t, b.shikii, script)
	if err := os.Chmod(b.shikii, 0o755); err != nil {
		t.Fatal(err)
	}
	b.install(t, b.shikii)
	return b
}

// install writes the plugin that shikii would write for the binary bin (as the real install does), with the import OpenCode resolves
// by itself replaced by a stand-in.
func (b *pluginBox) install(t *testing.T, bin string) {
	t.Helper()
	index := opencodePluginFiles(bin)[opencodeIndexFile]
	const imp = `import { Plugin } from "@opencode/plugin"`
	if !strings.Contains(index, imp) {
		t.Fatalf("the plugin imports Plugin from @opencode/plugin: %s", imp)
	}
	index = strings.Replace(index, imp, "const Plugin = { define: (p: any) => p }", 1)
	b.plugin = filepath.Join(b.dir, "plugin", "index.ts")
	writeFileT(t, b.plugin, index)
	writeFileT(t, filepath.Join(b.dir, "plugin", "package.json"), `{"type":"module"}`)
	b.driver = filepath.Join(b.dir, "driver.mjs")
	writeFileT(t, b.driver, pluginDriver)
}

// answer prepares what the fake shikii prints for an event (call n, or every call when n is 0).
func (b *pluginBox) answer(t *testing.T, event string, n int, out string) {
	name := "out-" + event
	if n > 0 {
		name += "-" + strconv.Itoa(n)
	}
	writeFileT(t, filepath.Join(b.dir, "state", name), out)
}

// hold makes call n of the fake shikii for event wait until the scenario's "release" step (the call is written down when it starts).
func (b *pluginBox) hold(t *testing.T, event string, n int) {
	writeFileT(t, filepath.Join(b.dir, "state", fmt.Sprintf("hold-%s-%d", event, n)), "")
}

// fail makes call n of the fake shikii for event exit with 1.
func (b *pluginBox) fail(t *testing.T, event string, n int) {
	writeFileT(t, filepath.Join(b.dir, "state", fmt.Sprintf("fail-%s-%d", event, n)), "")
}

// otherProject is the folder of a second project: the plugin is loaded for it too (a scenario with "locations").
func (b *pluginBox) otherProject(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(b.dir, "projectA")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (b *pluginBox) mode(t *testing.T, mode string) {
	writeFileT(t, filepath.Join(b.dir, "state", "mode"), mode)
}

func additionalJSON(text string) string {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"additionalContext": text}})
	return string(b)
}

// play runs the scenario (the steps and what the fake OpenCode knows) and returns what the plugin did.
func (b *pluginBox) play(t *testing.T, scenario map[string]any) (pluginRun, []pluginCall) {
	t.Helper()
	if _, ok := scenario["directory"]; !ok {
		scenario["directory"] = b.project
	}
	scenario["stateDir"] = filepath.Join(b.dir, "state")
	body, _ := json.Marshal(scenario)
	scenarioPath := filepath.Join(b.dir, "scenario.json")
	writeFileT(t, scenarioPath, string(body))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--no-warnings", b.driver, b.plugin, scenarioPath)
	cmd.Dir = b.dir // not the project's folder: a hook that ran in the folder OpenCode was started in would show
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the plugin did not run under node: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var run pluginRun
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &run); err != nil {
		t.Fatalf("the driver said: %v\n%s", err, out)
	}
	var calls []pluginCall
	if f, err := os.Open(filepath.Join(b.dir, "state", "calls.jsonl")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var c pluginCall
			if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
				t.Fatalf("the fake shikii wrote %q: %v", sc.Text(), err)
			}
			calls = append(calls, c)
		}
	}
	return run, calls
}

func countCalls(calls []pluginCall, event string) int {
	n := 0
	for _, c := range calls {
		if c.Event == event {
			n++
		}
	}
	return n
}

func stopsOf(calls []pluginCall) []pluginCall {
	var stops []pluginCall
	for _, c := range calls {
		if c.Event == "stop" {
			stops = append(stops, c)
		}
	}
	return stops
}

// The brief of the session and the text of the request are in the system text of every model call, of the second request too:
// text put there is not kept in the conversation. Each request has its own text, the brief is asked for once.
func TestOpenCodePluginGivesTheBriefAndTheRequestTextToEveryTurn(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 1, additionalJSON("TEXT-OF-REQUEST-ONE"))
	b.answer(t, "user-prompt-submit", 2, additionalJSON("TEXT-OF-REQUEST-TWO"))
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "first"},
		{"op": "context", "sid": "s1"},
		{"op": "context", "sid": "s1"}, // a second model call of the same turn (after a tool call)
		{"op": "prompt", "sid": "s1", "text": "second"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 3 {
		t.Fatalf("the steps ran: %+v", run)
	}
	for i, want := range []string{"TEXT-OF-REQUEST-ONE", "TEXT-OF-REQUEST-ONE", "TEXT-OF-REQUEST-TWO"} {
		got := run.Contexts[i]
		if !slices.Contains(got, "BASE") || !slices.Contains(got, "BRIEF-OF-THE-SESSION") || !slices.Contains(got, want) {
			t.Errorf("model call %d must carry the brief and %q: %q", i+1, want, got)
		}
	}
	if slices.Contains(run.Contexts[2], "TEXT-OF-REQUEST-ONE") {
		t.Errorf("the second request does not carry the first one's text: %q", run.Contexts[2])
	}
	if n := countCalls(calls, "session-start"); n != 1 {
		t.Errorf("the brief is asked for once per session, asked %d times", n)
	}
	if n := countCalls(calls, "user-prompt-submit"); n != 2 {
		t.Errorf("every request of the person is passed on, passed %d", n)
	}
}

// Another session does not get this session's text, and the first call's JSON is what hookInput reads.
func TestOpenCodePluginPassesTheKeysHookInputReadsFromTheProjectsFolder(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF"))
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "Please fix the login"},
		{"op": "context", "sid": "s2"},
	}})
	if len(run.Errors) != 0 || len(calls) != 2 {
		t.Fatalf("one session-start and one user-prompt-submit: %+v %+v", run, calls)
	}
	for _, c := range calls {
		if c.Pwd != b.project {
			t.Errorf("%s runs in the project's folder %s (as Claude Code and Codex run their hooks), ran in %s", c.Event, b.project, c.Pwd)
		}
		if c.Stdin["session_id"] != "s1" || c.Stdin["cwd"] != b.project {
			t.Errorf("%s gets the session and the folder: %v", c.Event, c.Stdin)
		}
		if c.Args != "hook "+c.Event {
			t.Errorf("the fake shikii is called as `shikii hook <event>`: %q", c.Args)
		}
	}
	if calls[1].Stdin["prompt"] != "Please fix the login" {
		t.Errorf("user-prompt-submit gets the request's text: %v", calls[1].Stdin)
	}
	if len(run.Contexts) != 1 || len(run.Contexts[0]) != 1 {
		t.Errorf("a session that had no request gets nothing added: %q", run.Contexts)
	}
}

// Without a folder from OpenCode the hook is not given one: the plugin does not make one up.
func TestOpenCodePluginGivesNoFolderWhenOpenCodeHasNone(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{"noLocation": true, "steps": []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}}})
	if len(calls) != 2 {
		t.Fatalf("the hooks still run: %+v", calls)
	}
	for _, c := range calls {
		if _, has := c.Stdin["cwd"]; has && c.Stdin["cwd"] != nil {
			t.Errorf("no cwd key is passed when OpenCode gave none: %v", c.Stdin)
		}
		if c.Pwd != b.dir {
			t.Errorf("the hook runs where the plugin was started when OpenCode gave no folder: %s", c.Pwd)
		}
	}
}

// After a summary of the conversation the brief is asked for again at the next request, as Claude Code runs SessionStart again.
func TestOpenCodePluginAsksForTheBriefAgainAfterASummary(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 1, additionalJSON("BRIEF-BEFORE"))
	b.answer(t, "session-start", 2, additionalJSON("BRIEF-AFTER-THE-SUMMARY"))
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "one"},
		{"op": "compaction", "sid": "s2"}, // another session's summary changes nothing here
		{"op": "prompt", "sid": "s1", "text": "two"},
		{"op": "context", "sid": "s1"},
		{"op": "compaction", "sid": "s1"},
		{"op": "prompt", "sid": "s1", "text": "three"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 2 {
		t.Fatalf("the steps ran: %+v", run)
	}
	if n := countCalls(calls, "session-start"); n != 2 {
		t.Errorf("session-start runs at the first request and again after the summary, ran %d times", n)
	}
	if !slices.Contains(run.Contexts[0], "BRIEF-BEFORE") || slices.Contains(run.Contexts[0], "BRIEF-AFTER-THE-SUMMARY") {
		t.Errorf("before the summary the first brief is used: %q", run.Contexts[0])
	}
	if !slices.Contains(run.Contexts[1], "BRIEF-AFTER-THE-SUMMARY") || slices.Contains(run.Contexts[1], "BRIEF-BEFORE") {
		t.Errorf("after the summary the new brief is used: %q", run.Contexts[1])
	}
}

// A stop block continues the turn with its reason, marked so the next stop is stop_hook_active; the continued turn still has the texts.
func TestOpenCodePluginContinuesAfterAStopBlockAndMarksTheNextStop(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT"))
	b.answer(t, "stop", 1, `{"decision":"block","reason":"CONTINUE-WITH-THIS"}`)
	run, calls := b.play(t, map[string]any{
		"messages": map[string]any{"s1": []map[string]any{
			{"type": "user", "content": []map[string]any{{"type": "text", "text": "request"}}},
			{"type": "assistant", "content": []map[string]any{{"type": "reasoning", "text": "thinking"}, {"type": "text", "text": "REPLY-ONE"}, {"type": "text", "text": "REPLY-TWO"}}},
			{"type": "idle", "outcome": "succeeded"},
		}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "request"},
			{"op": "succeeded", "sid": "s1"},
			{"op": "context", "sid": "s1"}, // the model call of the continued turn
			{"op": "succeeded", "sid": "s1"},
		}})
	if len(run.Errors) != 0 {
		t.Fatalf("no hook throws: %v", run.Errors)
	}
	if len(run.Prompts) != 1 || run.Prompts[0].Text != "CONTINUE-WITH-THIS" || run.Prompts[0].SessionID != "s1" || run.Prompts[0].Metadata["shikii"] != "stop-continue" {
		t.Fatalf("the block's reason is sent as a prompt marked as the plugin's own: %+v", run.Prompts)
	}
	stops := stopsOf(calls)
	if len(stops) != 2 {
		t.Fatalf("stop runs at the end of each turn: %d", len(stops))
	}
	if stops[0].Stdin["stop_hook_active"] != false || stops[1].Stdin["stop_hook_active"] != true {
		t.Errorf("the first stop is not stop_hook_active and the one after the continuation is: %v / %v", stops[0].Stdin, stops[1].Stdin)
	}
	if stops[0].Stdin["last_assistant_message"] != "REPLY-ONE\nREPLY-TWO" || stops[0].Stdin["session_id"] != "s1" || stops[0].Stdin["cwd"] != b.project || stops[0].Pwd != b.project {
		t.Errorf("stop gets the last assistant message (its text parts), the session and the folder: %v in %s", stops[0].Stdin, stops[0].Pwd)
	}
	if n := countCalls(calls, "user-prompt-submit"); n != 1 {
		t.Errorf("the plugin's own continuation is not a new request of the person: %d", n)
	}
	if len(run.Contexts) != 1 || !slices.Contains(run.Contexts[0], "BRIEF") || !slices.Contains(run.Contexts[0], "TEXT") {
		t.Errorf("the continued turn still carries the brief and the request's text: %q", run.Contexts)
	}
}

// A stop that blocks again and again is cut off, and a halt (continue: false) is not continued.
func TestOpenCodePluginStopsContinuingAfterEightAndOnAHalt(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 0, `{"decision":"block","reason":"AGAIN"}`)
	steps := []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}}
	for i := 0; i < 12; i++ {
		steps = append(steps, map[string]any{"op": "succeeded", "sid": "s1"})
	}
	run, calls := b.play(t, map[string]any{"steps": steps})
	// a request and its 8 continuations are 9 turns: each ends once, and an execution after them is no turn of the plugin's
	if len(run.Prompts) != 8 || countCalls(calls, "stop") != 9 {
		t.Errorf("a request is continued 8 times at most: %d continuations, %d stops (want 8 and 9)", len(run.Prompts), countCalls(calls, "stop"))
	}

	h := newPluginBox(t)
	h.answer(t, "stop", 0, `{"continue":false,"stopReason":"enough","decision":"block","reason":"R"}`)
	run, _ = h.play(t, map[string]any{"steps": []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}, {"op": "succeeded", "sid": "s1"}}})
	if len(run.Prompts) != 0 {
		t.Errorf("a halt ends the turn: %+v", run.Prompts)
	}

	// the person's next request starts the count again
	again := newPluginBox(t)
	again.answer(t, "stop", 0, `{"decision":"block","reason":"AGAIN"}`)
	steps = []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}}
	for i := 0; i < 10; i++ {
		steps = append(steps, map[string]any{"op": "succeeded", "sid": "s1"})
	}
	steps = append(steps, map[string]any{"op": "prompt", "sid": "s1", "text": "y"}, map[string]any{"op": "succeeded", "sid": "s1"})
	run, _ = again.play(t, map[string]any{"steps": steps})
	if len(run.Prompts) != 9 {
		t.Errorf("a new request of the person may be continued again: %d continuations", len(run.Prompts))
	}
}

// A subagent's session (it has a parent) is not the person's request: nothing is run, nothing is added, nothing is continued.
func TestOpenCodePluginDoesNothingInASubagentsSession(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT"))
	b.answer(t, "stop", 0, `{"decision":"block","reason":"R"}`)
	run, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1"}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "You are a subagent spawned by another session."},
			{"op": "context", "sid": "child"},
			{"op": "succeeded", "sid": "child"},
			{"op": "prompt", "sid": "s1", "text": "the person's request"},
			{"op": "context", "sid": "s1"},
		}})
	if len(run.Errors) != 0 || len(run.Prompts) != 0 {
		t.Fatalf("nothing is continued in the subagent's session: %+v", run)
	}
	for _, c := range calls {
		if c.Stdin["session_id"] == "child" {
			t.Errorf("the subagent's session reaches shikii: %+v", c)
		}
	}
	if len(run.Contexts) != 2 || len(run.Contexts[0]) != 1 || !slices.Contains(run.Contexts[1], "TEXT") {
		t.Errorf("the subagent's model call gets nothing, the person's session gets its texts: %q", run.Contexts)
	}
}

// A shikii that prints something broken, exits with 1, or is not there never makes a hook throw (a throw refuses the person's request),
// and nothing is added or continued.
func TestOpenCodePluginNeverThrowsWhenShikiiFails(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, b *pluginBox){
		"garbage on stdout": func(t *testing.T, b *pluginBox) { b.mode(t, "garbage") },
		"exit code 1":       func(t *testing.T, b *pluginBox) { b.mode(t, "exit1") },
		"not JSON objects": func(t *testing.T, b *pluginBox) {
			for _, e := range []string{"session-start", "user-prompt-submit", "stop"} {
				b.answer(t, e, 0, `["decision","block"]`)
			}
		},
		"wrong types": func(t *testing.T, b *pluginBox) {
			b.answer(t, "session-start", 0, `{"hookSpecificOutput":{"additionalContext":42}}`)
			b.answer(t, "user-prompt-submit", 0, `{"hookSpecificOutput":{"additionalContext":{"a":1}}}`)
			b.answer(t, "stop", 0, `{"decision":"block","reason":42}`)
		},
		"the binary is gone": func(t *testing.T, b *pluginBox) { b.install(t, filepath.Join(b.dir, "gone", "shikii")) },
	} {
		t.Run(name, func(t *testing.T) {
			b := newPluginBox(t)
			setup(t, b)
			run, _ := b.play(t, map[string]any{"steps": []map[string]any{
				{"op": "prompt", "sid": "s1", "text": "x"},
				{"op": "context", "sid": "s1"},
				{"op": "succeeded", "sid": "s1"},
				{"op": "context", "sid": "s1"},
			}})
			if len(run.Errors) != 0 {
				t.Errorf("a hook threw: %v", run.Errors)
			}
			if len(run.Prompts) != 0 {
				t.Errorf("nothing is continued: %+v", run.Prompts)
			}
			for _, c := range run.Contexts {
				if len(c) != 1 {
					t.Errorf("nothing is added: %q", c)
				}
			}
		})
	}
}

// The plugin runs under node as written: the stand-in driver above would pass a plugin that did nothing, so one call is checked to
// have reached the fake shikii at all.
func TestOpenCodePluginReallyRunsTheFakeShikii(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}}})
	if len(calls) == 0 {
		t.Fatalf("the plugin did not call the fake shikii")
	}
}

// Measured on OpenCode 2.3.0 (a server with two project folders): the plugin is set up once per folder, the prompt and context hooks run
// only in the instance of the session's folder, and ctx.event.subscribe of every instance sees the events of every session. A turn of
// project B ends with one shikii hook stop, run in B's folder: the instance of project A only shares the server.
func TestOpenCodePluginStopRunsOnceInTheSessionsOwnFolder(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions":  map[string]any{"sB": map[string]any{"id": "sB", "location": map[string]any{"directory": b.project}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "B", "sid": "sB", "text": "a request in project B"},
			{"op": "succeeded", "sid": "sB"},
		},
	})
	stops := stopsOf(calls)
	for _, s := range stops {
		if s.Pwd != b.project {
			t.Errorf("shikii hook stop for project B's session ran in another project's folder %s (cwd %v)", s.Pwd, s.Stdin["cwd"])
		}
	}
	if len(stops) != 1 {
		t.Errorf("one turn of project B ran shikii hook stop %d times (want 1)", len(stops))
	}
}

// The prompt hook only runs for the folder's own sessions on OpenCode, but a session whose folder is not the plugin's is left alone
// wherever the call comes from.
func TestOpenCodePluginLeavesASessionOfAnotherFolderAlone(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 0, `{"decision":"block","reason":"R"}`)
	run, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"far": map[string]any{"id": "far", "location": map[string]any{"directory": filepath.Join(b.dir, "elsewhere")}}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "far", "text": "x"},
			{"op": "succeeded", "sid": "far"},
		},
	})
	if len(calls) != 0 || len(run.Prompts) != 0 || len(run.Errors) != 0 {
		t.Errorf("nothing runs for another folder's session: %+v %+v", calls, run)
	}
}

// A summary in the middle of a turn (the context overflowed, reason auto) is followed by more model calls of the same turn. Claude Code
// runs SessionStart (source compact) right after the summary: the brief is asked for again at once and the rest of the turn has it.
func TestOpenCodePluginBriefStaysAfterACompactionInTheMiddleOfATurn(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT-OF-THE-REQUEST"))
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a long request"},
		{"op": "context", "sid": "s1"},
		{"op": "compaction", "sid": "s1"}, // the context overflowed: OpenCode summarises and the same turn goes on
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 2 {
		t.Fatalf("the steps ran: %+v", run)
	}
	if !slices.Contains(run.Contexts[0], "BRIEF-OF-THE-SESSION") {
		t.Fatalf("precondition: the first model call has the brief: %q", run.Contexts[0])
	}
	if !slices.Contains(run.Contexts[1], "BRIEF-OF-THE-SESSION") {
		t.Errorf("the model call after the compaction, in the same turn, has no brief: %q", run.Contexts[1])
	}
	if n := countCalls(calls, "session-start"); n != 2 {
		t.Errorf("session-start runs at the first request and right after the summary, ran %d times", n)
	}
}

// A summary whose session-start does not answer keeps the old brief until one does, and a summary of a session this instance never saw a
// request of (another folder's) asks for nothing.
func TestOpenCodePluginKeepsTheOldBriefWhenTheNewOneCannotBeAskedFor(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 1, additionalJSON("OLD-BRIEF"))
	b.fail(t, "session-start", 2)
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "x"},
		{"op": "compaction", "sid": "s1"},
		{"op": "compaction", "sid": "other"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 1 || !slices.Contains(run.Contexts[0], "OLD-BRIEF") {
		t.Errorf("the old brief is used until a new one is asked for: %+v", run)
	}
	if n := countCalls(calls, "session-start"); n != 2 {
		t.Errorf("one session-start at the request, one after the summary of the session it knew (none for the other): %d", n)
	}
}

// A compaction the person asked for ends with the same session.execution.succeeded as a turn does, but no turn of the agent ended
// (Claude Code runs no Stop for /compact): no stop runs, and the stop after it is not stop_hook_active because of an old continuation.
func TestOpenCodePluginNoStopForACompactionAlone(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request"},
		{"op": "succeeded", "sid": "s1"}, // stop 1 blocks: the plugin continues the turn
		{"op": "succeeded", "sid": "s1"}, // stop 2: the end of the continuation (stop_hook_active true, as it should be)
		{"op": "compaction", "sid": "s1"},
		{"op": "succeeded", "sid": "s1"}, // the end of the compaction's execution, no turn of the agent
	}})
	stops := stopsOf(calls)
	if len(stops) < 2 {
		t.Fatalf("precondition: the turn and its continuation ran stop: %+v", stops)
	}
	if len(stops) != 2 {
		t.Errorf("a compaction alone ran shikii hook stop (%d stops, want 2)", len(stops))
	}
	// the next real turn is not marked as a continuation
	_, calls = b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "the next request"},
		{"op": "succeeded", "sid": "s1"},
	}})
	if last := stopsOf(calls); len(last) == 0 || last[len(last)-1].Stdin["stop_hook_active"] != false {
		t.Errorf("the stop of the next request is not stop_hook_active: %+v", last)
	}
}

// OpenCode runs stop after the turn ended, so the person may type the next request before a slow stop answers. A block that comes late
// belongs to the older request: it is not sent as a continuation after the new request, and the new request's stop is not stop_hook_active.
func TestOpenCodePluginDropsTheLateBlockOfAnOlderRequest(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"LATE-CONTINUE-FOR-REQUEST-ONE"}`)
	b.hold(t, "stop", 1)
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "request one"},
		{"op": "succeeded", "sid": "s1", "nowait": true}, // stop 1 starts and waits
		{"op": "waitCalls", "event": "stop", "count": 1},
		{"op": "prompt", "sid": "s1", "text": "request two, a new task"},
		{"op": "release", "event": "stop", "n": 1},
		{"op": "settle"},
		{"op": "succeeded", "sid": "s1"}, // the end of request two
	}})
	if len(run.Errors) != 0 {
		t.Fatalf("the steps ran: %v", run.Errors)
	}
	if len(run.Prompts) != 0 {
		t.Errorf("the late block of request one is not sent after request two: %+v", run.Prompts)
	}
	stops := stopsOf(calls)
	if len(stops) != 2 || stops[1].Stdin["stop_hook_active"] != false {
		t.Errorf("the stop of request two is not stop_hook_active: %+v", stops)
	}
}

// One slow stop does not hold back the events of other sessions.
func TestOpenCodePluginASlowStopDoesNotHoldBackOtherSessions(t *testing.T) {
	b := newPluginBox(t)
	b.hold(t, "stop", 1)
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "one"},
		{"op": "prompt", "sid": "s2", "text": "two"},
		{"op": "succeeded", "sid": "s1", "nowait": true}, // stop 1 starts and waits
		{"op": "succeeded", "sid": "s2", "nowait": true},
		{"op": "waitCalls", "event": "stop", "count": 2}, // the second session's stop starts while the first waits
		{"op": "release", "event": "stop", "n": 1},
		{"op": "settle"},
	}})
	if len(run.Errors) != 0 {
		t.Errorf("the second session's stop was held back by the first: %v", run.Errors)
	}
	if n := countCalls(calls, "stop"); n != 2 {
		t.Errorf("both sessions end their turn: %d stops", n)
	}
}

// Measured on 2.3.0: when a subagent finishes, its report goes into the parent's inbox as a synthetic message
// (metadata.source "subagent") and wakes the parent for a turn the person did not ask for. No prompt hook runs for it. Claude Code's
// notification of a finished task is a turn too, and its Stop is not stop_hook_active: neither is this one, even right after a
// continuation. A report that arrives while a turn already runs changes nothing.
func TestOpenCodePluginASubagentsReportWakesTheParentForATurnOfItsOwn(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
	run, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1"}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "a request"},
			{"op": "succeeded", "sid": "s1"}, // stop 1 blocks: the continuation runs
			{"op": "subagent", "sid": "s1"},  // a report while the continuation runs: nothing changes
			{"op": "succeeded", "sid": "s1"}, // stop 2 (stop_hook_active true)
			{"op": "subagent", "sid": "s1"},  // the report that wakes the parent
			{"op": "succeeded", "sid": "s1"}, // stop 3, for a turn nobody typed
			{"op": "subagent", "sid": "child"},
			{"op": "succeeded", "sid": "child"}, // a subagent's own session: nothing
		}})
	if len(run.Errors) != 0 {
		t.Fatalf("the steps ran: %v", run.Errors)
	}
	stops := stopsOf(calls)
	if len(stops) != 3 {
		t.Fatalf("three turns of the parent ended, got %d stops", len(stops))
	}
	for i, want := range []bool{false, true, false} {
		if stops[i].Stdin["stop_hook_active"] != want {
			t.Errorf("stop %d: stop_hook_active want %v, got %v", i+1, want, stops[i].Stdin["stop_hook_active"])
		}
	}
	if n := countCalls(calls, "user-prompt-submit"); n != 1 {
		t.Errorf("a subagent's report is no request of the person: %d", n)
	}
}

// The "superseded" order below was never seen on 2.3.0: no place that sends it was found, and a message sent while a turn runs joined
// the same execution without an interrupted event (steer and queue alike). It is a made-up order that fixes what the plugin does with it.
// A turn that ends without succeeding (the person interrupted it, OpenCode shut down, the model failed) gets no stop, as Claude Code runs
// no Stop for those: a summary the person asks for right after it runs no stop either. A turn superseded by the person's next message
// does not end the new turn: the new turn's stop still runs once (measured on 2.3.0: interrupted carries the reason user, shutdown,
// superseded or inactivity).
func TestOpenCodePluginNoStopAfterATurnThatDidNotSucceed(t *testing.T) {
	for _, end := range []map[string]any{
		{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "user"}},
		{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "shutdown"}},
		{"op": "event", "type": "session.execution.failed", "sid": "s1", "data": map[string]any{"error": map[string]any{"message": "x"}}},
	} {
		b := newPluginBox(t)
		run, calls := b.play(t, map[string]any{"steps": []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "a request"},
			end,
			{"op": "compaction", "sid": "s1"},
			{"op": "succeeded", "sid": "s1"}, // the end of the summary's execution
		}})
		if len(run.Errors) != 0 {
			t.Fatalf("the steps ran: %v", run.Errors)
		}
		if stops := stopsOf(calls); len(stops) != 0 {
			t.Errorf("after %v a summary alone ran shikii hook stop: %+v", end["data"], stops)
		}
	}
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request"},
		{"op": "prompt", "sid": "s1", "text": "the next message, sent while the turn ran"},
		{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "superseded"}},
		{"op": "succeeded", "sid": "s1"},
	}})
	if stops := stopsOf(calls); len(stops) != 1 {
		t.Errorf("the turn that superseded the first one runs stop once, got %d", len(stops))
	}
}

// The person interrupted a continuation the plugin had sent. A subagent's report that then wakes the parent is a turn nobody typed, not a
// continuation: its stop is not stop_hook_active.
func TestOpenCodePluginAWokenTurnAfterAnInterruptedContinuationIsNotAContinuation(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"GO-ON"}`)
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request"},
		{"op": "succeeded", "sid": "s1"}, // stop 1 blocks: the plugin sends the continuation
		{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "user"}},
		{"op": "subagent", "sid": "s1"},
		{"op": "succeeded", "sid": "s1"},
	}})
	if len(run.Errors) != 0 {
		t.Fatalf("the steps ran: %v", run.Errors)
	}
	stops := stopsOf(calls)
	if len(stops) != 2 {
		t.Fatalf("the request and the woken turn ran stop, got %d", len(stops))
	}
	if stops[1].Stdin["stop_hook_active"] != false {
		t.Errorf("the woken turn's stop is stop_hook_active: %+v", stops[1].Stdin)
	}
}

// A report for a session this instance never saw a request of wakes nothing, even when OpenCode does not say which folder it is in: the
// event reaches the plugin of every folder.
func TestOpenCodePluginASubagentsReportForAnUnseenSessionWakesNothing(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "subagent", "sid": "unseen"},
		{"op": "succeeded", "sid": "unseen"},
	}})
	if len(calls) != 0 {
		t.Errorf("nothing runs for a session this instance never saw: %+v", calls)
	}
}

// A report from another folder's session wakes nothing in this instance.
func TestOpenCodePluginASubagentsReportInAnotherFoldersSessionWakesNothing(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"far": map[string]any{"id": "far", "location": map[string]any{"directory": filepath.Join(b.dir, "elsewhere")}}},
		"steps": []map[string]any{
			{"op": "subagent", "sid": "far"},
			{"op": "succeeded", "sid": "far"},
		}})
	if len(calls) != 0 {
		t.Errorf("another folder's session is not this plugin's: %+v", calls)
	}
}

// CI must not go green without the plugin's checks: when node is missing the tests fail on CI and are skipped elsewhere.
type fakeT struct{ fatal, skip bool }

func (f *fakeT) Helper()               {}
func (f *fakeT) Fatalf(string, ...any) { f.fatal = true }
func (f *fakeT) Skipf(string, ...any)  { f.skip = true }

func TestPluginTestsFailInsteadOfSkippingOnCI(t *testing.T) {
	var onCI, onMachine fakeT
	pluginTestsUnavailable(&onCI, "true", "node is not installed")
	pluginTestsUnavailable(&onMachine, "", "node is not installed")
	if !onCI.fatal || onCI.skip {
		t.Errorf("with CI set the plugin tests fail when they cannot run: %+v", onCI)
	}
	if onMachine.fatal || !onMachine.skip {
		t.Errorf("on a machine without CI they are skipped: %+v", onMachine)
	}
	t.Setenv("PATH", t.TempDir())
	if why := pluginNodeProblem(); !strings.Contains(why, "not installed") {
		t.Errorf("a PATH without node is a problem the tests name: %q", why)
	}
}

// Measured on 2.3.0: a subagent runs in the background, and its report wakes the parent for a turn of its own. When the report comes while
// the parent's stop still runs (shikii's Stop may ask its model for up to 20 seconds), the block of that stop is still sent: the person
// typed nothing, so the request number does not move.
func TestOpenCodePluginABlockIsKeptWhenASubagentsReportComesDuringTheStop(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
	b.hold(t, "stop", 1)
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request that starts a subagent in the background"},
		{"op": "succeeded", "sid": "s1", "nowait": true}, // stop 1 starts and waits (shikii asks its model)
		{"op": "waitCalls", "event": "stop", "count": 1},
		{"op": "subagent", "sid": "s1", "nowait": true}, // the child finishes meanwhile: its report wakes the parent
		{"op": "release", "event": "stop", "n": 1},
		{"op": "settle"},
	}})
	if len(run.Errors) != 0 {
		t.Fatalf("the steps ran: %v", run.Errors)
	}
	if n := countCalls(calls, "user-prompt-submit"); n != 1 {
		t.Fatalf("precondition: the person sent one request: %d", n)
	}
	sent := false
	for _, p := range run.Prompts {
		if p.Text == "SAVE-THE-RECORD" {
			sent = true
		}
	}
	if !sent {
		t.Errorf("the block of the stop was dropped because a subagent's report came while it ran (the person sent nothing): prompts %+v", run.Prompts)
	}
}

// A session moved to another project folder (session.move, event session.moved) belongs to the instance of the new folder from then on:
// the instance of the old folder asks again where it is, and runs no stop and no session-start for it.
func TestOpenCodePluginAMovedSessionRunsItsHooksOnlyInItsNewFolder(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions":  map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A"},
			{"op": "succeeded", "sid": "s1"},
			{"op": "move", "sid": "s1", "dir": b.project},
			{"op": "prompt", "at": "B", "sid": "s1", "text": "a request after the move, it starts a subagent"},
			{"op": "succeeded", "sid": "s1"},
			{"op": "subagent", "sid": "s1"}, // the report wakes the parent
			{"op": "succeeded", "sid": "s1"},
			{"op": "compaction", "sid": "s1"},
		},
	})
	moved := false
	for _, c := range calls {
		if c.Event == "user-prompt-submit" && c.Pwd == b.project {
			moved = true
			continue
		}
		if moved && c.Pwd != b.project {
			t.Errorf("after the move, shikii hook %s ran in the old folder %s", c.Event, c.Pwd)
		}
	}
	if !moved {
		t.Fatalf("precondition: the request after the move ran in project B: %+v", calls)
	}
	if n := len(stopsOf(calls)); n != 3 {
		t.Errorf("three turns ended (A's request, B's request, the woken turn): %d stops", n)
	}
}

// A move is told to the plugin of every folder. Only the instance of the folder the session came to takes it over and asks for its
// brief: neither the folder it left nor an unrelated folder runs shikii for it.
func TestOpenCodePluginOnlyTheNewFolderTakesOverAMovedSession(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	projectC := filepath.Join(b.dir, "projectC")
	if err := os.MkdirAll(projectC, 0o755); err != nil {
		t.Fatal(err)
	}
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project, "C": projectC},
		"sessions":  map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A"},
			{"op": "succeeded", "sid": "s1"},
			{"op": "move", "sid": "s1", "dir": b.project},
		},
	})
	var after []pluginCall
	stopped := false
	for _, c := range calls {
		if c.Event == "stop" {
			stopped = true
			continue
		}
		if stopped {
			after = append(after, c)
		}
	}
	if !stopped {
		t.Fatalf("precondition: the turn in project A ended with a stop: %+v", calls)
	}
	if len(after) != 1 || after[0].Event != "session-start" || after[0].Pwd != b.project {
		t.Errorf("after the move only project B asks for the session's brief: %+v", after)
	}
}

// A summary in the middle of a turn takes the turn's messages out of ctx.session.context, so stop would be given no reply. The plugin
// keeps the text of the last assistant message from session.text.ended ({sessionID, assistantMessageID, ordinal, text}) and uses it
// only when the context gives none; a new message replaces what was kept of the older one.
func TestOpenCodePluginGivesStopTheReplyWhenASummaryTookItOutOfTheContext(t *testing.T) {
	b := newPluginBox(t)
	text := func(sid, id string, ordinal int, text string) map[string]any {
		return map[string]any{"op": "event", "type": "session.text.ended", "sid": sid, "data": map[string]any{"assistantMessageID": id, "ordinal": ordinal, "text": text}}
	}
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a long request"},
		text("s1", "m1", 0, "OLD-STEP-TEXT"),
		text("s1", "m1", 2, "OLD-THIRD-PART"), // an older message: none of it is kept once a newer message starts
		text("s1", "m2", 1, "SECOND-PART"),
		text("s1", "m2", 0, "FIRST-PART"),
		{"op": "compaction", "sid": "s1"}, // the context now holds no message of the turn (the fake ctx answers an empty list)
		{"op": "succeeded", "sid": "s1"},
		{"op": "prompt", "sid": "s1", "text": "the next request"},
		{"op": "succeeded", "sid": "s1"}, // no text this turn: nothing left over from the last one
	}})
	stops := stopsOf(calls)
	if len(stops) != 2 {
		t.Fatalf("two turns ended: %+v", stops)
	}
	if got := stops[0].Stdin["last_assistant_message"]; got != "FIRST-PART\nSECOND-PART" {
		t.Errorf("stop gets the text of the last assistant message from the events: %q", got)
	}
	if got := stops[1].Stdin["last_assistant_message"]; got != "" {
		t.Errorf("a turn without text is not given the last turn's reply: %q", got)
	}
}

// Text from outside a turn of this instance (an execution nobody here started, such as a summary you asked for, or a session this
// instance has not seen a request of yet) is not kept: a later turn whose reply cannot be read gets no reply rather than that text.
func TestOpenCodePluginKeepsNoTextFromOutsideATurn(t *testing.T) {
	b := newPluginBox(t)
	stale := map[string]any{"op": "event", "type": "session.text.ended", "sid": "s1", "data": map[string]any{"assistantMessageID": "m0", "ordinal": 0, "text": "TEXT-FROM-OUTSIDE-A-TURN"}}
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		stale, // before this instance saw a request of s1
		{"op": "prompt", "sid": "s1", "text": "a request"},
		{"op": "succeeded", "sid": "s1"},
		stale, // after the turn ended, before the next request
		{"op": "prompt", "sid": "s1", "text": "the next request"},
		{"op": "succeeded", "sid": "s1"}, // the fake ctx answers an empty context and no text came during either turn
	}})
	stops := stopsOf(calls)
	if len(stops) != 2 {
		t.Fatalf("two turns ended: %+v", stops)
	}
	for i, s := range stops {
		if got := s.Stdin["last_assistant_message"]; got != "" {
			t.Errorf("stop %d got text from outside its turn: %q", i+1, got)
		}
	}
}

// The context comes first: when it still holds the reply, the kept text is not used.
func TestOpenCodePluginPrefersTheContextsReplyToTheKeptText(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{
		"messages": map[string]any{"s1": []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "FROM-THE-CONTEXT"}}}}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "x"},
			{"op": "event", "type": "session.text.ended", "sid": "s1", "data": map[string]any{"assistantMessageID": "m1", "ordinal": 0, "text": "FROM-THE-EVENT"}},
			{"op": "succeeded", "sid": "s1"},
		}})
	if got := stopsOf(calls)[0].Stdin["last_assistant_message"]; got != "FROM-THE-CONTEXT" {
		t.Errorf("the context's reply is used: %q", got)
	}
}

// Measured on 2.3.0: a session moved while its turn runs (the default delivery is steer: it is delivered at the next step boundary) goes
// on in the new folder, and the same execution ends there with session.execution.succeeded. The instance of the folder that prepared the
// request ends the turn, in that folder, once; the instance of the new folder never saw the turn start and runs no stop.
func TestOpenCodePluginATurnMovedWhileItRunsStillEndsWithOneStop(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions":  map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A"},
			{"op": "move", "sid": "s1", "dir": b.project}, // delivered at a step boundary while the turn runs
			{"op": "succeeded", "sid": "s1"},              // the same execution ends in project B
		},
	})
	if n := countCalls(calls, "user-prompt-submit"); n != 1 {
		t.Fatalf("precondition: one request: %d", n)
	}
	stops := stopsOf(calls)
	if len(stops) != 1 || stops[0].Pwd != projectA {
		t.Errorf("the moved turn ends with one stop, in the folder that began it: %+v", stops)
	}
}

// After such a move the session is the new folder's: the subagent report that wakes it is handled there, and that turn has its own stop.
func TestOpenCodePluginAWokenTurnAfterAMoveInTheMiddleOfATurnHasItsStop(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions":  map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A that starts a subagent"},
			{"op": "move", "sid": "s1", "dir": b.project},
			{"op": "succeeded", "sid": "s1"},
			{"op": "subagent", "sid": "s1"}, // the report wakes the session, now in project B
			{"op": "succeeded", "sid": "s1"},
		},
	})
	stops := stopsOf(calls)
	if len(stops) != 2 || stops[0].Pwd != projectA || stops[1].Pwd != b.project {
		t.Errorf("two turns ended: the moved one in A, the woken one in B: %+v", stops)
	}
}

// Measured on 2.3.0: the model call right after a move in the middle of a turn is made before session.moved reaches the plugins, so it
// has neither the old folder's brief and request text nor the new folder's brief. The calls after session.moved get the brief of the new
// project (asked for when the session came). The request's own text is never handed over: it was prepared, and counted, in the folder the
// request was made in, and asking again there would count the person's request twice.
func TestOpenCodePluginTheModelCallsAfterAMoveGetTheNewFoldersBriefButNotTheRequestsText(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT-OF-THE-REQUEST"))
	run, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions":  map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A"},
			{"op": "context", "at": "A", "sid": "s1"}, // the first model call, in A
			{"op": "relocate", "sid": "s1", "dir": b.project},
			{"op": "context", "at": "B", "sid": "s1"}, // the call right after the move: session.moved has not reached the plugins
			{"op": "event", "type": "session.moved", "sid": "s1"},
			{"op": "context", "at": "B", "sid": "s1"}, // the next call of the same turn
		},
	})
	if len(run.Contexts) != 3 || !slices.Contains(run.Contexts[0], "TEXT-OF-THE-REQUEST") {
		t.Fatalf("precondition: the first model call has the request's text: %+v", run.Contexts)
	}
	if slices.Contains(run.Contexts[1], "BRIEF-OF-THE-SESSION") || slices.Contains(run.Contexts[1], "TEXT-OF-THE-REQUEST") {
		t.Errorf("the call right after the move is made before the move is told: it has nothing of the session: %q", run.Contexts[1])
	}
	if !slices.Contains(run.Contexts[2], "BRIEF-OF-THE-SESSION") {
		t.Errorf("the call after session.moved has the brief asked for in the new folder: %q", run.Contexts[2])
	}
	if slices.Contains(run.Contexts[2], "TEXT-OF-THE-REQUEST") {
		t.Errorf("the request's text is not handed over to the new folder: %q", run.Contexts[2])
	}
	if n := countCalls(calls, "user-prompt-submit"); n != 1 {
		t.Errorf("the person's request is counted once: %d", n)
	}
	for _, c := range calls {
		if c.Event == "session-start" && c.Pwd == b.project {
			return
		}
	}
	t.Errorf("session-start ran in the new folder: %+v", calls)
}

// The kept text of a turn that did not succeed (interrupted, failed) is dropped with the turn: a later turn whose reply cannot be read
// from the context is not given it.
func TestOpenCodePluginTheTextOfAnInterruptedTurnIsNoLaterTurnsReply(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request the person interrupts"},
		{"op": "event", "type": "session.text.ended", "sid": "s1", "data": map[string]any{"assistantMessageID": "m1", "ordinal": 0, "text": "TEXT-OF-THE-INTERRUPTED-TURN"}},
		{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "user"}},
		{"op": "prompt", "sid": "s1", "text": "the next request"},
		{"op": "compaction", "sid": "s1"}, // the fake ctx answers an empty context
		{"op": "succeeded", "sid": "s1"},
	}})
	stops := stopsOf(calls)
	if len(stops) != 1 {
		t.Fatalf("one turn ended: %+v", stops)
	}
	if got := stops[0].Stdin["last_assistant_message"]; got != "" {
		t.Errorf("the next turn got the interrupted turn's text as its reply: %q", got)
	}
}

// Measured on 2.3.0: a turn moved in its middle goes on in the new folder, and a subagent's report that comes while it still runs joins
// that same execution, which ends once. The new folder took the session over, but the turn is the old folder's: only the old folder runs
// its stop (the session says a turn runs: time.busy is later than time.idle). A report after the turn ended wakes a turn of the new folder
// (the test of the woken turn above).
func TestOpenCodePluginAReportDuringAMovedTurnRunsNoSecondStop(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions": map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA},
			"time": map[string]any{"busy": 2, "idle": 1}}}, // a turn runs
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A that starts a subagent"},
			{"op": "move", "sid": "s1", "dir": b.project}, // delivered at a step boundary while the turn runs
			{"op": "subagent", "sid": "s1"},               // the report comes while the turn still runs in B: it joins it
			{"op": "succeeded", "sid": "s1"},              // the one execution ends
		},
	})
	if stops := stopsOf(calls); len(stops) != 1 || stops[0].Pwd != projectA {
		t.Errorf("one turn ended: one stop, in the folder that began it: %+v", stops)
	}
}

// The turn that was the old folder's is over when its execution ends: a report after that wakes a turn of the new folder, with its own stop,
// also when the session said a turn ran at the move.
func TestOpenCodePluginAReportAfterAMovedTurnEndedWakesTheNewFolder(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions": map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA},
			"time": map[string]any{"busy": 2, "idle": 1}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A that starts a subagent"},
			{"op": "move", "sid": "s1", "dir": b.project},
			{"op": "succeeded", "sid": "s1"},
			{"op": "subagent", "sid": "s1"},
			{"op": "succeeded", "sid": "s1"},
		},
	})
	if stops := stopsOf(calls); len(stops) != 2 || stops[0].Pwd != projectA || stops[1].Pwd != b.project {
		t.Errorf("the moved turn ends in A, the woken turn in B: %+v", stops)
	}
}

// A session that runs no turn when it moves has nothing to leave to the old folder: a report right after wakes the new folder.
func TestOpenCodePluginAReportAfterAnIdleSessionMovedWakesTheNewFolder(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions": map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA},
			"time": map[string]any{"busy": 1, "idle": 2}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A that starts a subagent"},
			{"op": "succeeded", "sid": "s1"},
			{"op": "move", "sid": "s1", "dir": b.project},
			{"op": "subagent", "sid": "s1"},
			{"op": "succeeded", "sid": "s1"},
		},
	})
	if stops := stopsOf(calls); len(stops) != 2 || stops[1].Pwd != b.project {
		t.Errorf("the woken turn ends in B: %+v", stops)
	}
}

// The execution ends while the instance still asks where the session is: the turn is over by the time it knows, so nothing is left to the
// old folder and a later report wakes the new folder.
func TestOpenCodePluginATurnThatEndsWhileAMoveIsBeingHandledLeavesNothingToTheOldFolder(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions": map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA},
			"time": map[string]any{"busy": 2, "idle": 1}}},
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A that starts a subagent"},
			{"op": "relocate", "sid": "s1", "dir": b.project},
			{"op": "event", "type": "session.moved", "sid": "s1", "nowait": true},
			{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "user"}},
			{"op": "subagent", "sid": "s1"},
			{"op": "succeeded", "sid": "s1"},
		},
	})
	if stops := stopsOf(calls); len(stops) != 1 || stops[0].Pwd != b.project {
		t.Errorf("the report after the turn ended wakes B: %+v", stops)
	}
}
