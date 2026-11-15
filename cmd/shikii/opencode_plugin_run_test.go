package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These tests run the OpenCode plugin itself (opencode_plugin/index.ts) under node, with a fake ctx standing in for OpenCode and a fake
// shikii (a small shell script that writes down what it was given and answers with prepared JSON). The text of the plugin is checked
// in opencode_plugin_test.go; what it does over a conversation (the brief on every request, the folder it runs in, the continuation
// after a stop block, subagents, failures) can only be seen by running it. Nothing of OpenCode or of a real shikii is started: node
// runs one script, and the fake shikii is a file in a temporary folder. Without node 24 or newer (the version CI's Go job installs;
// it strips the types of a .ts file by itself) these tests are skipped.

// pluginDriver is the stand-in for OpenCode: it loads the plugin, gives it a ctx, and plays the steps of one scenario.
const pluginDriver = `
import fs from "node:fs"
import { pathToFileURL } from "node:url"
const [pluginPath, scenarioPath] = process.argv.slice(2)
const scenario = JSON.parse(fs.readFileSync(scenarioPath, "utf8"))
const plugin = (await import(pathToFileURL(pluginPath).href)).default

const hooks = {}
const prompts = []
const contexts = []
const errors = []
const queue = []
let waiter = null
let pushed = 0
let consumed = 0
let idleWaiter = null
const events = {
  [Symbol.asyncIterator]() {
    return {
      next() {
        if (queue.length === 0 && consumed === pushed && idleWaiter) { const r = idleWaiter; idleWaiter = null; r() }
        if (queue.length > 0) { consumed++; return Promise.resolve({ value: queue.shift(), done: false }) }
        return new Promise((resolve) => { waiter = resolve })
      },
    }
  },
}
function push(ev) {
  pushed++
  if (waiter) { const w = waiter; waiter = null; consumed++; w({ value: ev, done: false }) } else queue.push(ev)
}
// settled: the plugin has finished every event pushed so far (it asks for the next one only after handling the last)
function settled() {
  return new Promise((resolve) => {
    idleWaiter = resolve
    if (waiter && consumed === pushed) { const r = idleWaiter; idleWaiter = null; r() }
  })
}

const sessions = scenario.sessions ?? {}
const ctx = {
  location: scenario.noLocation ? undefined : { directory: scenario.directory },
  session: {
    hook(name, fn) { hooks[name] = fn },
    async get({ sessionID }) { return sessions[sessionID] ?? { id: sessionID } },
    async context({ sessionID }) { return scenario.messages?.[sessionID] ?? [] },
    async prompt(arg) {
      prompts.push(arg)
      // OpenCode runs the prompt hook for a prompt the plugin sends too
      await hooks.prompt({ sessionID: arg.sessionID, prompt: { text: arg.text }, metadata: arg.metadata })
    },
  },
  event: { subscribe() { return events } },
}
await plugin.setup(ctx)

for (const step of scenario.steps) {
  try {
    if (step.op === "prompt") {
      await hooks.prompt({ sessionID: step.sid, prompt: { text: step.text }, metadata: step.metadata })
    } else if (step.op === "context") {
      const e = { sessionID: step.sid, system: [{ type: "text", text: "BASE" }] }
      await hooks.context(e)
      contexts.push(e.system.map((s) => s.text))
    } else if (step.op === "succeeded") {
      push({ type: "session.execution.succeeded", data: { sessionID: step.sid } })
      await settled()
    } else if (step.op === "compaction") {
      push({ type: "session.compaction.ended", data: { sessionID: step.sid } })
      await settled()
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

func newPluginBox(t *testing.T) *pluginBox {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed: the plugin is not run")
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		t.Skip("node does not run")
	}
	major, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), ".", 2)[0])
	if major < 24 {
		t.Skipf("node %s is older than 24: it does not strip types by itself", strings.TrimSpace(string(out)))
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

func hasText(texts []string, want string) bool {
	for _, t := range texts {
		if t == want {
			return true
		}
	}
	return false
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
		if !hasText(got, "BASE") || !hasText(got, "BRIEF-OF-THE-SESSION") || !hasText(got, want) {
			t.Errorf("model call %d must carry the brief and %q: %q", i+1, want, got)
		}
	}
	if hasText(run.Contexts[2], "TEXT-OF-REQUEST-ONE") {
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
	if !hasText(run.Contexts[0], "BRIEF-BEFORE") || hasText(run.Contexts[0], "BRIEF-AFTER-THE-SUMMARY") {
		t.Errorf("before the summary the first brief is used: %q", run.Contexts[0])
	}
	if !hasText(run.Contexts[1], "BRIEF-AFTER-THE-SUMMARY") || hasText(run.Contexts[1], "BRIEF-BEFORE") {
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
	var stops []pluginCall
	for _, c := range calls {
		if c.Event == "stop" {
			stops = append(stops, c)
		}
	}
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
	if len(run.Contexts) != 1 || !hasText(run.Contexts[0], "BRIEF") || !hasText(run.Contexts[0], "TEXT") {
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
	if len(run.Prompts) != 8 || countCalls(calls, "stop") != 12 {
		t.Errorf("a request is continued 8 times at most: %d continuations, %d stops", len(run.Prompts), countCalls(calls, "stop"))
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
	if len(run.Contexts) != 2 || len(run.Contexts[0]) != 1 || !hasText(run.Contexts[1], "TEXT") {
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
