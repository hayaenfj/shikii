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
const contexts = [] // per model call: the system texts, then every text the plugin added to the messages
const requests = [] // per model call: the system texts and every message's texts, as the model would get them
const errors = []
// measured on 2.3.0: the context hook gets the messages of the model call ({id, role, content}); a user message of the person's request
// has the messageID of its prompt hook, and a summary's checkpoint may carry its text as a plain string
const llm = {} // sessionID -> the messages of the next model call
const queued = {} // sessionID -> messages sent while a turn runs, not delivered yet (steer waits for a step boundary)
let lastID = 0
const newID = () => "msg-" + (++lastID)
const partsOf = (content) => typeof content === "string" ? [content] : Array.isArray(content) ? content.map((p) => String(p?.text ?? "")) : []
const sessions = scenario.sessions ?? {}
const locations = scenario.locations ?? { main: scenario.directory }
const instances = {}
for (const [name, dir] of Object.entries(locations)) {
  const hooks = {}
  instances[name] = hooks
  const ctx = {
    location: scenario.noLocation ? undefined : { directory: dir },
    session: {
      // idleHook: an OpenCode that refuses experimental.idle, with a throw ("throws") or a rejected promise ("rejects")
      hook(n, fn) {
        if (n === "experimental.idle" && scenario.idleHook === "throws") throw new Error("unknown hook " + n)
        if (n === "experimental.idle" && scenario.idleHook === "rejects") return Promise.reject(new Error("unknown hook " + n))
        hooks[n] = fn
      },
      get: ({ sessionID }) => track(async () => sessions[sessionID] ?? { id: sessionID }),
      context: ({ sessionID }) => track(async () => scenario.messages?.[sessionID] ?? []),
      prompt: (arg) => track(async () => {
        prompts.push(arg)
        const id = newID()
        ;(llm[arg.sessionID] ??= []).push({ id, role: "user", content: [{ type: "text", text: arg.text }] })
        // OpenCode runs the prompt hook for a prompt the plugin sends too
        await hooks.prompt({ sessionID: arg.sessionID, messageID: id, prompt: { text: arg.text }, metadata: arg.metadata })
      }),
    },
    event: { subscribe() { return subscribe() } },
  }
  await plugin.setup(ctx)
}
const first = Object.keys(locations)[0]
const stateDir = scenario.stateDir
let exited = false // an "idle" step with exit ended the execution: the scenario stopped there

for (const step of scenario.steps) {
  try {
    const hooks = instances[step.at ?? first]
    if (step.op === "prompt") {
      // queued: the message waits until a "deliver" step (sent while a turn runs); noMessageID: an OpenCode that gives the hook no messageID
      const id = newID()
      const message = { id, role: "user", content: [{ type: "text", text: step.text }] }
      ;((step.queued ? queued : llm)[step.sid] ??= []).push(message)
      const sent = hooks.prompt({ sessionID: step.sid, messageID: step.noMessageID ? undefined : id, prompt: { text: step.text }, metadata: step.metadata })
      // nowait: the next steps run while the prompt hook still works (settle waits for it)
      if (step.nowait) void track(() => sent); else await sent
    } else if (step.op === "context") {
      const given = llm[step.sid] ?? []
      const e = { sessionID: step.sid, system: [{ type: "text", text: "BASE" }], messages: given.map((m) => ({ ...m, content: Array.isArray(m.content) ? m.content.map((p) => ({ ...p })) : m.content })) }
      await hooks.context(e)
      const added = []
      e.messages.forEach((m, i) => added.push(...partsOf(m.content).slice(partsOf(given[i]?.content).length)))
      contexts.push([...e.system.map((s) => s.text), ...added.map((t) => t.trim())])
      requests.push({ system: e.system.map((s) => s.text), messages: e.messages.map((m) => ({ id: m.id, role: m.role, texts: partsOf(m.content).map((t) => t.trim()) })) })
    } else if (step.op === "deliver") {
      ;(llm[step.sid] ??= []).push(...(queued[step.sid] ?? []))
      queued[step.sid] = []
    } else if (step.op === "succeeded" || step.op === "compaction" || step.op === "subagent") {
      if (step.op === "succeeded") broadcast({ type: "session.execution.succeeded", data: { sessionID: step.sid } })
      if (step.op === "compaction") {
        // the summary replaces the messages: a checkpoint with the summary as a string, and the last step.keep messages (none by default)
        const kept = step.keep ? (llm[step.sid] ?? []).slice(-step.keep) : []
        llm[step.sid] = [{ id: newID(), role: "user", content: "SUMMARY-OF-THE-CONVERSATION" }, ...kept]
        broadcast({ type: "session.compaction.ended", data: { sessionID: step.sid } })
      }
      if (step.op === "subagent") {
        // measured on 2.3.0: the report of a finished subagent is put into the parent's inbox and wakes the parent; no prompt hook runs
        broadcast({ type: "session.inbox.enqueued", data: { sessionID: step.sid, inboxID: "inbox", item: { type: "synthetic", delivery: "steer", payload: { text: "<subagent/>", metadata: { source: "subagent", childID: "child", agent: "subagent", state: "completed" } } } } })
      }
      if (!step.nowait) await settle()
    } else if (step.op === "idle") {
      // measured on 2.3.0 (SessionRunner): when a turn has nothing left to do, OpenCode runs the experimental.idle hook of the session's
      // folder and waits for it. A message admitted meanwhile (the plugin's continuation) goes on in the same execution; otherwise the
      // execution ends with session.execution.succeeded. exit: what opencode run does right then with the private server it started
      // (--standalone, or no background service running): it ends it within milliseconds, and the scenario stops before the plugin
      // hears the event
      const before = prompts.length
      if (hooks["experimental.idle"]) await hooks["experimental.idle"]({ sessionID: step.sid, background: [] })
      if (prompts.length === before) {
        broadcast({ type: "session.execution.succeeded", data: { sessionID: step.sid } })
        if (step.exit) { exited = true; break }
      }
      await settle()
    } else if (step.op === "idleStart") {
      // the idle hook starts and the next steps run while it works: measured on 2.3.0, OpenCode waits for the hook in a part of the
      // execution a person's interrupt can cut, so the execution ends as interrupted while the plugin's hook goes on (settle waits for it)
      if (hooks["experimental.idle"]) void track(() => hooks["experimental.idle"]({ sessionID: step.sid, background: [] }))
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
      // wait until the fake shikii has been started step.count times for step.event (it writes its line when it starts), for at most
      // step.within milliseconds (10 seconds when not given)
      const tries = (step.within ?? 10000) / 20
      for (let i = 0; ; i++) {
        const lines = fs.existsSync(stateDir + "/calls.jsonl") ? fs.readFileSync(stateDir + "/calls.jsonl", "utf8").split("\n").filter((l) => l.includes('"event":"' + step.event + '"')) : []
        if (lines.length >= step.count) break
        if (i > tries) throw new Error("the fake shikii was not started " + step.count + " times for " + step.event + " within " + (step.within ?? 10000) + " ms")
        await new Promise((resolve) => setTimeout(resolve, 20))
      }
    }
  } catch (err) {
    errors.push(String(err))
  }
}
console.log(JSON.stringify({ prompts, contexts, requests, errors, exited }))
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
	Requests []modelRequest
	Errors   []string
	Exited   bool // an "idle" step with exit ended the execution and the scenario stopped there
}

// modelRequest is one model call as the fake OpenCode would send it: the system texts and every message's texts.
type modelRequest struct {
	System   []string
	Messages []struct {
		ID    string
		Role  string
		Texts []string
	}
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

// The brief of the session and the text of the request are in every model call, of the second request too: what the hooks give is not
// kept in the conversation, so the plugin adds it to each call. Each request has its own text, the brief is asked for once.
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
	// the first request's text stays on the first request's message (as Claude Code keeps it in the conversation), once: the second
	// request does not add it again (where each text goes: TestOpenCodePluginPutsItsTextsOnThePersonsMessagesAndLeavesTheSystemAlone)
	if n := strings.Count(strings.Join(run.Contexts[2], " "), "TEXT-OF-REQUEST-ONE"); n != 1 {
		t.Errorf("the second request's call has the first one's text once, on its message: %d times in %q", n, run.Contexts[2])
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
	// the two are asked at once, so their order is not fixed
	for _, c := range calls {
		if c.Event == "user-prompt-submit" && c.Stdin["prompt"] != "Please fix the login" {
			t.Errorf("user-prompt-submit gets the request's text: %v", c.Stdin)
		}
	}
	if countCalls(calls, "user-prompt-submit") != 1 {
		t.Errorf("one user-prompt-submit: %+v", calls)
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

// Measured on 2.3.0: a subagent's session has a parent and the agent it runs as, its prompt hook gets the task after one line of
// OpenCode's, and its idle hook runs when it has nothing left to do. The task goes to shikii as the start of a subagent of the
// parent's session, what shikii gives goes on the task's message, and the subagent's last message goes to subagent-stop. It is not
// the person's request: no user-prompt-submit, no session-start and no stop run for it, and it is never continued.
func TestOpenCodePluginGivesASubagentItsLessonsAndReportsItsLastMessage(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT"))
	b.answer(t, "stop", 0, `{"decision":"block","reason":"R"}`)
	b.answer(t, "subagent-start", 0, additionalJSON("FOR-THE-CHILD"))
	b.answer(t, "subagent-stop", 0, `{"decision":"block","reason":"NEVER-CONTINUED"}`) // whatever it prints, the subagent is not continued
	reply := []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done.\nshikii: lesson:0123456789abcdef:0123456789abcdef unused"}}}}
	run, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1", "agent": "general"}},
		"messages": map[string]any{"child": reply},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "You are a subagent spawned by another session.\nRead the notes and report."},
			{"op": "context", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "the parent steers the subagent"}, // not a task: no second start
			{"op": "context", "sid": "child"},
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "s1", "text": "the person's request"},
			{"op": "context", "sid": "s1"},
		}})
	if len(run.Errors) != 0 || len(run.Prompts) != 0 {
		t.Fatalf("nothing is continued in the subagent's session: %+v", run)
	}
	var starts, stops []pluginCall
	for _, c := range calls {
		switch {
		case c.Event == "subagent-start":
			starts = append(starts, c)
		case c.Event == "subagent-stop":
			stops = append(stops, c)
		case c.Stdin["session_id"] == "child":
			t.Errorf("the subagent's session is no request of the person: %+v", c)
		}
	}
	if len(starts) != 1 || starts[0].Stdin["session_id"] != "s1" || starts[0].Stdin["agent_id"] != "child" || starts[0].Stdin["agent_type"] != "general" || starts[0].Stdin["prompt"] != "Read the notes and report." || starts[0].Pwd != b.project {
		t.Fatalf("the task starts one subagent of the parent's session, with the task alone: %+v", starts)
	}
	if len(stops) != 1 || stops[0].Stdin["session_id"] != "s1" || stops[0].Stdin["agent_id"] != "child" || stops[0].Stdin["last_assistant_message"] != "Done.\nshikii: lesson:0123456789abcdef:0123456789abcdef unused" {
		t.Fatalf("the subagent's last message is reported once under the parent's session: %+v", stops)
	}
	if len(run.Contexts) != 3 || !slices.Contains(run.Contexts[0], "FOR-THE-CHILD") || !slices.Contains(run.Contexts[1], "FOR-THE-CHILD") || slices.Contains(run.Contexts[0], "BRIEF") || !slices.Contains(run.Contexts[2], "TEXT") || slices.Contains(run.Contexts[2], "FOR-THE-CHILD") {
		t.Errorf("every model call of the subagent gets what shikii gave for its task, and only it: %q", run.Contexts)
	}
	if got := run.Requests[1].Messages; len(got) != 2 || !slices.Contains(got[0].Texts, "FOR-THE-CHILD") || slices.Contains(got[1].Texts, "FOR-THE-CHILD") {
		t.Errorf("the text stays on the task's message: %+v", got)
	}
}

// A subagent's turn that did not succeed reports nothing, a subagent of another folder's session belongs to that folder's instance,
// and a subagent with nothing to say (no last message) is not reported.
func TestOpenCodePluginReportsNoSubagentThatFailedOrIsNotItsOwn(t *testing.T) {
	b := newPluginBox(t)
	other := b.otherProject(t)
	reply := []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done."}}}}
	_, calls := b.play(t, map[string]any{
		"sessions": map[string]any{
			"failed": map[string]any{"id": "failed", "parentID": "s1"},
			"far":    map[string]any{"id": "far", "parentID": "s2", "location": map[string]any{"directory": other}},
			"silent": map[string]any{"id": "silent", "parentID": "s1"},
		},
		"messages": map[string]any{"failed": reply, "far": reply},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "failed", "text": "a task"},
			{"op": "event", "type": "session.execution.failed", "sid": "failed"},
			{"op": "idle", "sid": "failed"},
			{"op": "prompt", "sid": "far", "text": "a task in another folder"},
			{"op": "idle", "sid": "far"},
			{"op": "prompt", "sid": "silent", "text": "a task"},
			{"op": "idle", "sid": "silent"},
			{"op": "succeeded", "sid": "silent"},
		}})
	var events []string
	for _, c := range calls {
		events = append(events, c.Event+":"+fmt.Sprint(c.Stdin["agent_id"]))
	}
	if want := []string{"subagent-start:failed", "subagent-start:silent"}; !slices.Equal(events, want) {
		t.Fatalf("shikii is started for %v, got %v", want, events)
	}
}

// The subagent's report has two ways in, as the person's stop has: the idle hook, inside the execution (OpenCode waits for it), and
// the end of the execution for an OpenCode that does not take that hook. Each alone reports the subagent once.
func TestOpenCodePluginReportsASubagentFromTheIdleHookOrFromTheEndOfItsExecution(t *testing.T) {
	reply := []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done."}}}}
	for name, scenario := range map[string]map[string]any{
		"the idle hook, the scenario ends before the event is heard": {"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "a task"},
			{"op": "idle", "sid": "child", "exit": true},
		}},
		"an OpenCode without the idle hook": {"idleHook": "throws", "steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "a task"},
			{"op": "succeeded", "sid": "child"},
		}},
	} {
		b := newPluginBox(t)
		scenario["sessions"] = map[string]any{"child": map[string]any{"id": "child", "parentID": "s1"}}
		scenario["messages"] = map[string]any{"child": reply}
		_, calls := b.play(t, scenario)
		n := 0
		for _, c := range calls {
			if c.Event == "subagent-stop" && c.Stdin["agent_id"] == "child" && c.Stdin["last_assistant_message"] == "Done." {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: the subagent is reported once, got %d: %+v", name, n, calls)
		}
	}
}

// A subagent that is continued and ends that turn without saying anything new is not reported again with the message of its
// earlier turn: a turn with no message reports nothing.
func TestOpenCodePluginReportsNoSubagentTurnThatAddedNoMessage(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{
		"sessions": map[string]any{"child": map[string]any{"id": "child", "parentID": "s1"}},
		"messages": map[string]any{"child": []map[string]any{{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "Done."}}}}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "child", "text": "a task"},
			{"op": "idle", "sid": "child"},
			{"op": "prompt", "sid": "child", "text": "one more thing"},
			{"op": "idle", "sid": "child"},
		}})
	if n := countCalls(calls, "subagent-stop"); n != 1 {
		t.Fatalf("the second turn added no message, so the subagent is reported once, got %d: %+v", n, calls)
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
// The other way into a moved turn that still runs: the person sends a message in the new folder while it runs. On 2.3.0 the message joins
// the same execution (steer and queue alike) and its prompt hook runs in the new folder's instance. The request is prepared there
// (user-prompt-submit in the new folder), and the one execution still ends with one stop, in the folder the turn began in.
func TestOpenCodePluginAPersonsMessageDuringAMovedTurnRunsNoSecondStop(t *testing.T) {
	b := newPluginBox(t)
	projectA := b.otherProject(t)
	_, calls := b.play(t, map[string]any{
		"locations": map[string]any{"A": projectA, "B": b.project},
		"sessions": map[string]any{"s1": map[string]any{"id": "s1", "location": map[string]any{"directory": projectA},
			"time": map[string]any{"busy": 2, "idle": 1}}}, // a turn runs
		"steps": []map[string]any{
			{"op": "prompt", "at": "A", "sid": "s1", "text": "a request in project A"},
			{"op": "move", "sid": "s1", "dir": b.project},
			{"op": "prompt", "at": "B", "sid": "s1", "text": "the person adds a word while it runs"},
			{"op": "succeeded", "sid": "s1"}, // the one execution ends
		},
	})
	stops := stopsOf(calls)
	if len(stops) != 1 {
		t.Fatalf("one execution ended, and it ran %d stops: %+v", len(stops), stops)
	}
	if stops[0].Pwd != projectA {
		t.Errorf("the stop runs in the folder the turn began in, ran in %s", stops[0].Pwd)
	}
	prepared := false
	for _, c := range calls {
		if c.Event == "user-prompt-submit" && c.Pwd == b.project {
			prepared = true
		}
	}
	if !prepared {
		t.Errorf("the message sent in the new folder is prepared there: %+v", calls)
	}
}

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

// A first session-start that gives nothing (it failed, timed out or printed nothing) leaves the session without a brief until a summary of
// the conversation or a move asks again: the next requests do not ask again, as Claude Code runs SessionStart only at those times too.
func TestOpenCodePluginAsksForTheBriefAgainOnlyAfterASummaryWhenTheFirstAskGaveNothing(t *testing.T) {
	b := newPluginBox(t)
	b.fail(t, "session-start", 1)
	b.answer(t, "session-start", 2, additionalJSON("BRIEF-AFTER-THE-SUMMARY"))
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "one"},
		{"op": "context", "sid": "s1"},
		{"op": "prompt", "sid": "s1", "text": "two"},
		{"op": "context", "sid": "s1"},
		{"op": "compaction", "sid": "s1"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 3 {
		t.Fatalf("the steps ran: %+v", run)
	}
	starts := 0
	for i, c := range calls {
		if c.Event == "session-start" {
			starts++
		}
		if c.Event == "user-prompt-submit" && c.N == 2 && starts != 1 {
			t.Errorf("the second request asked for the brief again (call %d): %+v", i, calls)
		}
	}
	if starts != 2 {
		t.Errorf("session-start runs at the first request and after the summary only, ran %d times", starts)
	}
	for i, c := range run.Contexts[:2] {
		if slices.Contains(c, "BRIEF-AFTER-THE-SUMMARY") {
			t.Errorf("model call %d has a brief before the summary asked for one: %q", i+1, c)
		}
	}
	if !slices.Contains(run.Contexts[2], "BRIEF-AFTER-THE-SUMMARY") {
		t.Errorf("after the summary the brief is there: %q", run.Contexts[2])
	}
}

// The first request of a session asks shikii for the session's brief and for the request's text at the same time: asking for one after
// the other would add the time of session-start to the person's first request (each may take up to 15 seconds).
func TestOpenCodePluginAsksForTheBriefAndTheRequestsTextAtOnce(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT-OF-THE-REQUEST"))
	b.hold(t, "session-start", 1)
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "the first request", "nowait": true},
		// session-start is held until the release below (the fake shikii waits about ten seconds at most)
		{"op": "waitCalls", "event": "user-prompt-submit", "count": 1, "within": 3000},
		{"op": "release", "event": "session-start", "n": 1},
		{"op": "settle"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 {
		t.Errorf("user-prompt-submit is asked while session-start still runs: %v", run.Errors)
	}
	if len(run.Contexts) != 1 || !slices.Contains(run.Contexts[0], "BRIEF-OF-THE-SESSION") || !slices.Contains(run.Contexts[0], "TEXT-OF-THE-REQUEST") {
		t.Errorf("the model call has both answers: %q", run.Contexts)
	}
	if countCalls(calls, "session-start") != 1 || countCalls(calls, "user-prompt-submit") != 1 {
		t.Errorf("each is asked once: %+v", calls)
	}
}

// The plugin keeps a few things per session for as long as the server runs. Measured in OpenCode 2.3.0's own code: deleting a session
// sends session.deleted ({sessionID}) to every plugin. The plugin then forgets the session: nothing is added to its model calls any more,
// its execution ends with no stop, a subagent's report wakes nothing, and other sessions keep what they have.
func TestOpenCodePluginForgetsADeletedSession(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT-OF-THE-REQUEST"))
	run, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request in the session that is deleted"},
		{"op": "prompt", "sid": "s2", "text": "a request in another session"},
		{"op": "context", "sid": "s1"},
		{"op": "event", "type": "session.deleted", "sid": "s1"},
		{"op": "context", "sid": "s1"},
		{"op": "context", "sid": "s2"},
		{"op": "succeeded", "sid": "s1"},
		{"op": "subagent", "sid": "s1"},
		{"op": "succeeded", "sid": "s1"},
		{"op": "succeeded", "sid": "s2"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 3 {
		t.Fatalf("the steps ran: %+v", run)
	}
	if !slices.Contains(run.Contexts[0], "TEXT-OF-THE-REQUEST") {
		t.Fatalf("precondition: before the deletion the session has its texts: %q", run.Contexts[0])
	}
	if len(run.Contexts[1]) != 1 {
		t.Errorf("a deleted session gets nothing added: %q", run.Contexts[1])
	}
	if !slices.Contains(run.Contexts[2], "BRIEF-OF-THE-SESSION") || !slices.Contains(run.Contexts[2], "TEXT-OF-THE-REQUEST") {
		t.Errorf("another session keeps its texts: %q", run.Contexts[2])
	}
	for _, c := range stopsOf(calls) {
		if c.Stdin["session_id"] == "s1" {
			t.Errorf("a deleted session runs no stop: %v", c.Stdin)
		}
	}
	if n := len(stopsOf(calls)); n != 1 {
		t.Errorf("only the other session's turn ends with a stop: %d stops", n)
	}
}

// textsOf is the texts of message i of a model call ("" texts and missing messages give nil).
func textsOf(r modelRequest, i int) []string {
	if i >= len(r.Messages) {
		return nil
	}
	return r.Messages[i].Texts
}

// The model's system text is the same in every model call: a provider caches the start of a request, and a system text that changed
// with every request made it start over each time. The brief goes on the first message of the conversation and each request's text on
// that request's own message, where they stay in the later calls (as Claude Code keeps what its hooks add in the conversation), so the
// earlier part of every call is sent as it was.
func TestOpenCodePluginPutsItsTextsOnThePersonsMessagesAndLeavesTheSystemAlone(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 1, additionalJSON("TEXT-OF-REQUEST-ONE"))
	b.answer(t, "user-prompt-submit", 2, additionalJSON("TEXT-OF-REQUEST-TWO"))
	run, _ := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "first"},
		{"op": "context", "sid": "s1"},
		{"op": "context", "sid": "s1"}, // a second model call of the same turn
		{"op": "prompt", "sid": "s1", "text": "second"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Requests) != 3 {
		t.Fatalf("the steps ran: %+v", run)
	}
	for i, r := range run.Requests {
		if !slices.Equal(r.System, []string{"BASE"}) {
			t.Errorf("model call %d: the system text is changed: %q", i+1, r.System)
		}
	}
	one := []string{"first", "BRIEF-OF-THE-SESSION", "TEXT-OF-REQUEST-ONE"}
	for i := range 3 {
		if got := textsOf(run.Requests[i], 0); !slices.Equal(got, one) {
			t.Errorf("model call %d: the first request's message carries the brief and its text, as in every call: %q", i+1, got)
		}
	}
	if got := textsOf(run.Requests[2], 1); !slices.Equal(got, []string{"second", "TEXT-OF-REQUEST-TWO"}) {
		t.Errorf("the second request's message carries its own text: %q", got)
	}
}

// A summary in the middle of a turn takes the request's message away: the brief asked for again and the request's text go on the
// summary's message (its text may be a plain string), and the system text is still left alone. When the request's message is kept after
// the summary, its text stays on it. A summary that comes before any model call of the request takes its message as well.
func TestOpenCodePluginPutsItsTextsOnTheSummaryWhenTheRequestsMessageWentWithIt(t *testing.T) {
	for _, keep := range []int{0, 1, -1} { // -1: the summary comes before the request's first model call
		first := map[string]any{"op": "context", "sid": "s1"}
		if keep < 0 {
			first = map[string]any{"op": "settle"}
		}
		b := newPluginBox(t)
		b.answer(t, "session-start", 1, additionalJSON("BRIEF-BEFORE"))
		b.answer(t, "session-start", 2, additionalJSON("BRIEF-AFTER-THE-SUMMARY"))
		b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT-OF-THE-REQUEST"))
		run, _ := b.play(t, map[string]any{"steps": []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "a long request"},
			first,
			{"op": "compaction", "sid": "s1", "keep": max(keep, 0)},
			{"op": "context", "sid": "s1"},
		}})
		if len(run.Errors) != 0 || len(run.Requests) == 0 {
			t.Fatalf("keep %d: the steps ran: %+v", keep, run)
		}
		after := run.Requests[len(run.Requests)-1]
		if !slices.Equal(after.System, []string{"BASE"}) {
			t.Errorf("keep %d: the system text is changed after the summary: %q", keep, after.System)
		}
		want := [][]string{{"SUMMARY-OF-THE-CONVERSATION", "BRIEF-AFTER-THE-SUMMARY", "TEXT-OF-THE-REQUEST"}}
		if keep == 1 {
			want = [][]string{{"SUMMARY-OF-THE-CONVERSATION", "BRIEF-AFTER-THE-SUMMARY"}, {"a long request", "TEXT-OF-THE-REQUEST"}}
		}
		if len(after.Messages) != len(want) {
			t.Fatalf("keep %d: the call after the summary: %+v", keep, after.Messages)
		}
		for i := range want {
			if got := textsOf(after, i); !slices.Equal(got, want[i]) {
				t.Errorf("keep %d: message %d after the summary: %q, want %q", keep, i+1, got, want[i])
			}
		}
	}
}

// A message the person sends while a turn runs waits until a step boundary (steer). Until it arrives in the model's messages, its text
// goes nowhere: the running turn's calls are not given the next request's text. When it arrives, the text goes on it.
func TestOpenCodePluginPutsTheTextOfAWaitingMessageOnItWhenItArrives(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "user-prompt-submit", 1, additionalJSON("TEXT-OF-REQUEST-ONE"))
	b.answer(t, "user-prompt-submit", 2, additionalJSON("TEXT-OF-REQUEST-TWO"))
	run, _ := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "first"},
		{"op": "context", "sid": "s1"},
		{"op": "prompt", "sid": "s1", "text": "second", "queued": true},
		{"op": "context", "sid": "s1"}, // the running turn's next call: the second message has not arrived
		{"op": "deliver", "sid": "s1"},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Requests) != 3 {
		t.Fatalf("the steps ran: %+v", run)
	}
	if slices.Contains(run.Contexts[1], "TEXT-OF-REQUEST-TWO") {
		t.Errorf("the text of a message that has not arrived is given to the running turn: %q", run.Contexts[1])
	}
	if got := textsOf(run.Requests[1], 0); !slices.Equal(got, []string{"first", "TEXT-OF-REQUEST-ONE"}) {
		t.Errorf("the running turn keeps its request's text on its message: %q", got)
	}
	if got := textsOf(run.Requests[2], 1); !slices.Equal(got, []string{"second", "TEXT-OF-REQUEST-TWO"}) {
		t.Errorf("the message that arrived carries its text: %q", got)
	}
	if got := textsOf(run.Requests[2], 0); !slices.Equal(got, []string{"first", "TEXT-OF-REQUEST-ONE"}) {
		t.Errorf("the first message is sent as it was: %q", got)
	}
}

// An OpenCode that does not give the prompt hook the request's messageID: the text cannot be put on its message, so it goes into the
// system text as before rather than nowhere.
func TestOpenCodePluginStillGivesTheTextWhenOpenCodeGivesNoMessageID(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "session-start", 0, additionalJSON("BRIEF-OF-THE-SESSION"))
	b.answer(t, "user-prompt-submit", 0, additionalJSON("TEXT-OF-THE-REQUEST"))
	run, _ := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request", "noMessageID": true},
		{"op": "context", "sid": "s1"},
	}})
	if len(run.Errors) != 0 || len(run.Contexts) != 1 {
		t.Fatalf("the steps ran: %+v", run)
	}
	if !slices.Contains(run.Contexts[0], "TEXT-OF-THE-REQUEST") || !slices.Contains(run.Contexts[0], "BRIEF-OF-THE-SESSION") {
		t.Errorf("the model call still gets the brief and the request's text: %q", run.Contexts[0])
	}
}

// Measured on 2.3.0: opencode run waits for the execution to end and then exits, and the private server it started (--standalone, or
// no background service running) is ended with it, 40 ms after session.execution.succeeded: a stop started from that event never ran.
// OpenCode runs its experimental.idle hook inside the execution when the turn has nothing left to do, and waits for it. The turn's stop
// runs there, and a block's continuation goes on in the same execution, so it ends (and opencode run exits) only after the last stop.
func TestOpenCodePluginRunsStopBeforeTheExecutionEnds(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
	run, calls := b.play(t, map[string]any{
		"messages": map[string]any{"s1": []map[string]any{
			{"type": "user", "content": []map[string]any{{"type": "text", "text": "a request"}}},
			{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "THE-REPLY"}}},
		}},
		"steps": []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "a request"},
			{"op": "idle", "sid": "s1", "exit": true}, // the turn ends: stop blocks, and the continuation keeps the execution going
			{"op": "idle", "sid": "s1", "exit": true}, // the continuation ends: its stop lets the execution end, and the server goes
		}})
	if len(run.Errors) != 0 {
		t.Fatalf("no hook throws: %v", run.Errors)
	}
	stops := stopsOf(calls)
	if len(stops) != 2 {
		t.Fatalf("the turn and its continuation run stop before the execution ends: %d stops, want 2", len(stops))
	}
	if stops[0].Stdin["stop_hook_active"] != false || stops[1].Stdin["stop_hook_active"] != true {
		t.Errorf("the first stop is not stop_hook_active and the continuation's is: %v / %v", stops[0].Stdin, stops[1].Stdin)
	}
	if stops[0].Stdin["last_assistant_message"] != "THE-REPLY" || stops[0].Pwd != b.project {
		t.Errorf("stop gets the turn's reply and runs in the project's folder: %v in %s", stops[0].Stdin, stops[0].Pwd)
	}
	if len(run.Prompts) != 1 || run.Prompts[0].Text != "SAVE-THE-RECORD" || run.Prompts[0].Metadata["shikii"] != "stop-continue" {
		t.Errorf("the block's reason is sent, marked as the plugin's own, before the execution ends: %+v", run.Prompts)
	}
	if !run.Exited {
		t.Errorf("the execution ended after the continuation's stop")
	}
}

// The end of the execution follows the idle hook: a turn whose stop ran in the idle hook runs no second stop at session.execution.succeeded.
// An OpenCode that does not run the idle hook still gets the stop at that event (the turns ended with "succeeded" in the tests above).
func TestOpenCodePluginRunsOneStopWhenTheIdleHookAndTheEndOfTheExecutionBothCome(t *testing.T) {
	b := newPluginBox(t)
	_, calls := b.play(t, map[string]any{"steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request"},
		{"op": "idle", "sid": "s1"}, // the idle hook, then session.execution.succeeded, heard by the plugin
		{"op": "prompt", "sid": "s1", "text": "the next request"},
		{"op": "idle", "sid": "s1"},
	}})
	stops := stopsOf(calls)
	if len(stops) != 2 {
		t.Fatalf("each of the two turns runs stop once: %d stops", len(stops))
	}
	for i, s := range stops {
		if s.Stdin["stop_hook_active"] != false {
			t.Errorf("stop %d is not stop_hook_active: %v", i+1, s.Stdin)
		}
	}
}

// Inside the execution too, a request is continued 8 times at most, and a halt (continue: false) is not continued: the execution ends.
func TestOpenCodePluginStopsContinuingInsideTheExecutionAfterEightAndOnAHalt(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 0, `{"decision":"block","reason":"AGAIN"}`)
	steps := []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}}
	for i := 0; i < 12; i++ {
		steps = append(steps, map[string]any{"op": "idle", "sid": "s1", "exit": true})
	}
	run, calls := b.play(t, map[string]any{"steps": steps})
	if len(run.Prompts) != 8 || countCalls(calls, "stop") != 9 || !run.Exited {
		t.Errorf("a request and its 8 continuations end in one execution: %d continuations, %d stops, ended %v (want 8, 9, true)", len(run.Prompts), countCalls(calls, "stop"), run.Exited)
	}

	h := newPluginBox(t)
	h.answer(t, "stop", 0, `{"continue":false,"stopReason":"enough","decision":"block","reason":"R"}`)
	run, calls = h.play(t, map[string]any{"steps": []map[string]any{{"op": "prompt", "sid": "s1", "text": "x"}, {"op": "idle", "sid": "s1", "exit": true}}})
	if len(run.Prompts) != 0 || countCalls(calls, "stop") != 1 || !run.Exited {
		t.Errorf("a halt ends the execution: %+v, %d stops", run.Prompts, countCalls(calls, "stop"))
	}
}

var idleStopMessages = map[string]any{"s1": []map[string]any{
	{"type": "user", "content": []map[string]any{{"type": "text", "text": "a request"}}},
	{"type": "assistant", "content": []map[string]any{{"type": "text", "text": "THE-REPLY"}}},
}}

// The person interrupts while the turn's stop runs in the idle hook (OpenCode shows the session busy for the whole stop, up to its 20
// seconds). OpenCode ends the execution as interrupted, and opencode run exits with 1. The turn the person stopped is not continued:
// the plugin's rule for a turn that ended without succeeding, and Claude Code continues no turn the person stopped. Without the
// interrupt (the control) the same block continues the turn.
func TestOpenCodePluginDoesNotContinueATurnInterruptedWhileItsStopRuns(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		b := newPluginBox(t)
		b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
		b.hold(t, "stop", 1)
		steps := []map[string]any{
			{"op": "prompt", "sid": "s1", "text": "a request"},
			{"op": "idleStart", "sid": "s1"},
			{"op": "waitCalls", "event": "stop", "count": 1},
		}
		if interrupt {
			steps = append(steps, map[string]any{"op": "event", "type": "session.execution.interrupted", "sid": "s1", "data": map[string]any{"reason": "user"}, "nowait": true})
		}
		steps = append(steps, map[string]any{"op": "release", "event": "stop", "n": 1}, map[string]any{"op": "settle"})
		run, calls := b.play(t, map[string]any{"messages": idleStopMessages, "steps": steps})
		if len(run.Errors) != 0 {
			t.Fatalf("no hook throws: %v", run.Errors)
		}
		if n := countCalls(calls, "stop"); n != 1 {
			t.Fatalf("interrupt %v: one stop runs: %d", interrupt, n)
		}
		switch {
		case !interrupt && len(run.Prompts) != 1:
			t.Errorf("without an interrupt the block continues the turn: %+v", run.Prompts)
		case interrupt && len(run.Prompts) != 0:
			t.Errorf("the person interrupted while stop ran, and the plugin still sent the continuation: %+v", run.Prompts)
		}
	}
}

// The person's next request while the turn's stop runs in the idle hook: the old stop's block is for the old turn and is dropped, and
// the new turn gets its own stop, not stop_hook_active.
func TestOpenCodePluginDropsTheIdleStopsBlockWhenTheNextRequestComes(t *testing.T) {
	b := newPluginBox(t)
	b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
	b.hold(t, "stop", 1)
	run, calls := b.play(t, map[string]any{"messages": idleStopMessages, "steps": []map[string]any{
		{"op": "prompt", "sid": "s1", "text": "a request"},
		{"op": "idleStart", "sid": "s1"},
		{"op": "waitCalls", "event": "stop", "count": 1},
		{"op": "prompt", "sid": "s1", "text": "the next request"},
		{"op": "release", "event": "stop", "n": 1},
		{"op": "settle"},
		{"op": "idle", "sid": "s1"},
	}})
	if len(run.Errors) != 0 {
		t.Fatalf("no hook throws: %v", run.Errors)
	}
	stops := stopsOf(calls)
	if len(stops) != 2 || len(run.Prompts) != 0 {
		t.Fatalf("two stops and no continuation: %d stops, %+v", len(stops), run.Prompts)
	}
	if stops[1].Stdin["stop_hook_active"] != false {
		t.Errorf("the new request's stop is not stop_hook_active: %v", stops[1].Stdin)
	}
}

// An OpenCode whose ctx.session.hook refuses experimental.idle (a throw, or a rejected promise): the plugin still loads, and the turn's
// stop runs at session.execution.succeeded, once, and its block continues the turn.
func TestOpenCodePluginRunsStopAtTheEndOfTheExecutionWhenOpenCodeRefusesTheIdleHook(t *testing.T) {
	for _, how := range []string{"throws", "rejects"} {
		t.Run(how, func(t *testing.T) {
			b := newPluginBox(t)
			b.answer(t, "stop", 1, `{"decision":"block","reason":"SAVE-THE-RECORD"}`)
			run, calls := b.play(t, map[string]any{"idleHook": how, "messages": idleStopMessages, "steps": []map[string]any{
				{"op": "prompt", "sid": "s1", "text": "a request"},
				{"op": "idle", "sid": "s1"}, // no idle hook: the execution ends with succeeded, and the plugin hears it
				{"op": "idle", "sid": "s1"},
			}})
			if len(run.Errors) != 0 {
				t.Fatalf("no hook throws: %v", run.Errors)
			}
			stops := stopsOf(calls)
			if len(stops) != 2 || len(run.Prompts) != 1 {
				t.Errorf("the stop runs at succeeded and continues once: %d stops, %+v", len(stops), run.Prompts)
			}
		})
	}
}

// A shikii that fails, prints something broken or is not there never makes the idle hook throw (OpenCode would fail the execution), and
// nothing is continued: the execution ends as it would without the plugin.
func TestOpenCodePluginTheIdleHookNeverThrowsWhenShikiiFails(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, b *pluginBox){
		"garbage on stdout":  func(t *testing.T, b *pluginBox) { b.mode(t, "garbage") },
		"exit code 1":        func(t *testing.T, b *pluginBox) { b.mode(t, "exit1") },
		"wrong types":        func(t *testing.T, b *pluginBox) { b.answer(t, "stop", 0, `{"decision":"block","reason":42}`) },
		"the binary is gone": func(t *testing.T, b *pluginBox) { b.install(t, filepath.Join(b.dir, "gone", "shikii")) },
	} {
		t.Run(name, func(t *testing.T) {
			b := newPluginBox(t)
			setup(t, b)
			run, _ := b.play(t, map[string]any{"steps": []map[string]any{
				{"op": "prompt", "sid": "s1", "text": "x"},
				{"op": "idle", "sid": "s1", "exit": true},
			}})
			if len(run.Errors) != 0 {
				t.Errorf("the idle hook threw: %v", run.Errors)
			}
			if len(run.Prompts) != 0 || !run.Exited {
				t.Errorf("nothing is continued and the execution ends: %+v, ended %v", run.Prompts, run.Exited)
			}
		})
	}
}
