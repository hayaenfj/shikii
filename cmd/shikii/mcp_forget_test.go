package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// forget over MCP asks the person through the client (elicitation/create) and removes only on the person's yes. These tests run the
// server on a pipe, with a client the test plays: it reads what the server sends, answers (or does not), and looks at what the
// project holds afterwards. The project is the one of forget_fixture_test.go; every test uses a temporary HOME and project.

// liveMCP is a client on the other end of runMCP: it sends lines and reads the lines the server writes.
type liveMCP struct {
	t    *testing.T
	to   *io.PipeWriter
	from chan map[string]any
	done chan error
}

func newLiveMCP(t *testing.T, h http.Handler) *liveMCP {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &liveMCP{t: t, to: inW, from: make(chan map[string]any, 64), done: make(chan error, 1)}
	go func() {
		c.done <- runMCP(bufio.NewReader(inR), outW, h)
		_ = outW.Close()
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			var m map[string]any
			if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
				t.Errorf("the server wrote a line that is not JSON: %q", sc.Text())
				continue
			}
			c.from <- m
		}
		close(c.from)
	}()
	t.Cleanup(func() { _ = inW.Close() })
	return c
}

func (c *liveMCP) send(v any) {
	c.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() { _, err := c.to.Write(append(b, '\n')); wrote <- err }()
	select {
	case err := <-wrote:
		if err != nil {
			c.t.Fatalf("writing to the server: %v", err)
		}
	case <-time.After(10 * time.Second):
		c.t.Fatalf("the server did not read %s", b)
	}
}

// next is the next message the server wrote. The server writes in the order it handles, so a test that must know the server wrote
// nothing in between sends a ping and reads that its answer is next, instead of waiting for a silence.
func (c *liveMCP) next() map[string]any {
	c.t.Helper()
	select {
	case m, ok := <-c.from:
		if !ok {
			c.t.Fatal("the server ended while a message was expected")
		}
		return m
	case <-time.After(10 * time.Second):
		c.t.Fatal("no message from the server")
	}
	return nil
}

// finish ends the stream and waits for the server to return.
func (c *liveMCP) finish() {
	c.t.Helper()
	_ = c.to.Close()
	select {
	case err := <-c.done:
		if err != nil {
			c.t.Errorf("runMCP: %v", err)
		}
	case <-time.After(10 * time.Second):
		c.t.Fatal("the server did not end after its input closed")
	}
}

func (c *liveMCP) initialize(capabilities map[string]any) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": capabilities, "clientInfo": map[string]any{"name": "test", "version": "0"}}})
	if m := c.next(); m["id"] != float64(1) || m["result"] == nil {
		c.t.Fatalf("initialize: %v", m)
	}
	c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

// callForget sends a tools/call of shikii_wiki with the arguments.
func (c *liveMCP) callForget(id int, args map[string]any) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}})
}

// reply reads the answer to the call with the id: whether it is an error, and what its text says.
func (c *liveMCP) reply(id int) (bool, map[string]any) {
	c.t.Helper()
	m := c.next()
	if m["id"] != float64(id) || m["method"] != nil {
		c.t.Fatalf("expected the answer to call %d, got %v", id, m)
	}
	return toolBody(c.t, m)
}

// question reads the question the server asks the person and returns it.
func (c *liveMCP) question() map[string]any {
	c.t.Helper()
	m := c.next()
	if m["method"] != "elicitation/create" || m["id"] == nil {
		c.t.Fatalf("expected a question to the person, got %v", m)
	}
	return m
}

// pinged sends a ping and reads that its answer is the next message: the server wrote nothing before it.
func (c *liveMCP) pinged(id int) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "ping"})
	if m := c.next(); m["id"] != float64(id) || m["result"] == nil {
		c.t.Fatalf("expected only the answer to the ping, got %v", m)
	}
}

func toolBody(t *testing.T, m map[string]any) (bool, map[string]any) {
	t.Helper()
	res, _ := m["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no tool result in %v", m)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var body map[string]any
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("the text of the result is not JSON: %q", text)
	}
	return res["isError"] == true, body
}

func (w *forgetWorld) mcpHandler() http.Handler {
	return newServer(serveOpts{storeDir: w.root, projectScope: w.scope})
}

func answerTo(question map[string]any, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": question["id"], "result": result}
}

func accept(remove any) map[string]any {
	return map[string]any{"action": "accept", "content": map[string]any{"remove": remove}}
}

func forgetArgs(w *forgetWorld, extra map[string]any) map[string]any {
	args := map[string]any{"operation": "forget", "sessions": []string{w.A}}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// A client that does not say it can ask the person is refused, with the list and what a person runs; nothing changes and no
// question is sent. The same for a client that offers only a link to a page (url): it cannot show a form.
func TestMCPForgetWithoutElicitationChangesNothing(t *testing.T) {
	for name, capabilities := range map[string]map[string]any{
		"no capability":     {},
		"a link only":       {"elicitation": map[string]any{"url": map[string]any{}}},
		"elicitation: null": {"elicitation": nil},
	} {
		t.Run(name, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			c := newLiveMCP(t, w.mcpHandler())
			c.initialize(capabilities)
			c.callForget(2, forgetArgs(w, nil))
			failed, body := c.reply(2) // the next message is the answer: no question was sent first
			if !failed || body["status"] != "needs_person" || body["changed"] != false {
				t.Fatalf("a client that cannot ask the person ran forget: %v", body)
			}
			for _, want := range []string{"a person runs shikii forget", "<session id>", "--apply", ".shikii/records/forgotten/"} {
				if !strings.Contains(body["message"].(string), want) {
					t.Errorf("the refusal does not say %q: %s", want, body["message"])
				}
			}
			if plan, _ := body["plan"].(map[string]any); plan == nil || len(plan["remove"].([]any)) < 5 {
				t.Errorf("the refusal does not list what would go: %v", body["plan"])
			}
			c.finish()
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Errorf("the project changed: %v", forgetDiff(before, after))
			}
		})
	}
}

// canElicit reads the capability as the specification says: an empty object means a form, form means a form, url alone does not.
func TestCanElicitReadsTheFormCapability(t *testing.T) {
	for raw, want := range map[string]bool{
		``: false, `null`: false, `true`: false, `"form"`: false, `[]`: false,
		`{}`: true, `{"form":{}}`: true, `{"form":{},"url":{}}`: true, `{"url":{}}`: false,
	} {
		if got := canElicit(json.RawMessage(raw)); got != want {
			t.Errorf("canElicit(%q) = %v, want %v", raw, got, want)
		}
	}
}

// The person's yes removes what the plan listed and nothing else (the same checks as shikii forget --apply), and the question shows
// the person what goes, what stays and what only might belong, with a yes or no that starts as no.
func TestMCPForgetRemovesOnlyAfterThePersonsYes(t *testing.T) {
	for name, capabilities := range map[string]map[string]any{"empty": {"elicitation": map[string]any{}}, "form": {"elicitation": map[string]any{"form": map[string]any{}}}} {
		t.Run(name, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			c := newLiveMCP(t, w.mcpHandler())
			c.initialize(capabilities)
			c.callForget(2, forgetArgs(w, nil))
			q := c.question()
			if _, isString := q["id"].(string); !isString {
				t.Errorf("the id of the question is not a string: %v", q["id"])
			}
			params := q["params"].(map[string]any)
			message := params["message"].(string)
			for _, want := range []string{"Sessions: " + w.A, "Would remove:", "records/guards.jsonl: 2 lines", "Kept (", "Not removed:", ".shikii/records/forgotten/", "Should shikii remove these records now?"} {
				if !strings.Contains(message, want) {
					t.Errorf("the question does not say %q:\n%s", want, message)
				}
			}
			schema := params["requestedSchema"].(map[string]any)
			remove := schema["properties"].(map[string]any)["remove"].(map[string]any)
			if schema["type"] != "object" || remove["type"] != "boolean" || remove["default"] != false || !reflect.DeepEqual(schema["required"], []any{"remove"}) {
				t.Errorf("the form is not one yes or no that starts as no: %v", schema)
			}
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatalf("the project changed before the person answered: %v", forgetDiff(before, after))
			}
			c.send(answerTo(q, accept(true)))
			failed, body := c.reply(2)
			if failed || body["status"] != "removed" || body["changed"] != true || !strings.Contains(body["message"].(string), "records/forgotten/") {
				t.Fatalf("a yes did not remove: %v", body)
			}
			c.finish()
			if problems := w.problems(before); len(problems) > 0 {
				t.Fatalf("forget left the project wrong:\n  %s", strings.Join(problems, "\n  "))
			}
		})
	}
}

// Anything but a yes changes nothing: no, dismissing the question, a yes with the box unchecked, and answers that are not the form that
// was asked (a string or a number for the box, no box, no content, an action that is none of the three, no object, an error).
func TestMCPForgetChangesNothingWithoutAClearYes(t *testing.T) {
	cases := []struct {
		name   string
		answer func(q map[string]any) map[string]any
		status string
	}{
		{"declined", func(q map[string]any) map[string]any { return answerTo(q, map[string]any{"action": "decline"}) }, "declined"},
		{"cancelled", func(q map[string]any) map[string]any { return answerTo(q, map[string]any{"action": "cancel"}) }, "dismissed"},
		{"accepted with the box unchecked", func(q map[string]any) map[string]any { return answerTo(q, accept(false)) }, "declined"},
		{"the box is the string true", func(q map[string]any) map[string]any { return answerTo(q, accept("true")) }, "not_confirmed"},
		{"the box is the number 1", func(q map[string]any) map[string]any { return answerTo(q, accept(1)) }, "not_confirmed"},
		{"the box is null", func(q map[string]any) map[string]any { return answerTo(q, accept(nil)) }, "not_confirmed"},
		{"accepted without content", func(q map[string]any) map[string]any {
			return answerTo(q, map[string]any{"action": "accept"})
		}, "not_confirmed"},
		{"accepted with other content", func(q map[string]any) map[string]any {
			return answerTo(q, map[string]any{"action": "accept", "content": map[string]any{"confirm": true}})
		}, "not_confirmed"},
		{"an action that is none of the three", func(q map[string]any) map[string]any {
			return answerTo(q, map[string]any{"action": "yes", "content": map[string]any{"remove": true}})
		}, "not_confirmed"},
		{"no action", func(q map[string]any) map[string]any {
			return answerTo(q, map[string]any{"content": map[string]any{"remove": true}})
		}, "not_confirmed"},
		{"the result is a string", func(q map[string]any) map[string]any { return answerTo(q, "yes") }, "not_confirmed"},
		{"an error from the client", func(q map[string]any) map[string]any {
			return map[string]any{"jsonrpc": "2.0", "id": q["id"], "error": map[string]any{"code": -32601, "message": "no such method"}}
		}, "needs_person"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newForgetWorld(t)
			before := w.snapshot()
			c := newLiveMCP(t, w.mcpHandler())
			c.initialize(map[string]any{"elicitation": map[string]any{}})
			c.callForget(2, forgetArgs(w, nil))
			c.send(tc.answer(c.question()))
			_, body := c.reply(2)
			if body["status"] != tc.status || body["changed"] != false {
				t.Errorf("the answer ended as %v, want %s and nothing changed: %v", body["status"], tc.status, body["message"])
			}
			c.finish()
			if after := w.snapshot(); !reflect.DeepEqual(before, after) {
				t.Errorf("the project changed: %v", forgetDiff(before, after))
			}
		})
	}
}

// An answer that carries another id is no answer to the question: it is passed over, and when the stream ends with the question still
// open nothing is removed, however the passed-over answers read.
func TestMCPForgetIgnoresAnAnswerWithAnotherID(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	q := c.question()
	for _, id := range []any{"someone-else", 1, 2, nil, q["id"].(string) + "0", strings.ToUpper(q["id"].(string))} {
		c.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": accept(true)})
	}
	c.finish() // the input ends: the question stays unanswered
	failed, body := lastToolBody(t, c)
	if !failed || body["status"] != "unanswered" || body["changed"] != false {
		t.Fatalf("an answer with another id was taken: %v", body)
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

// lastToolBody reads what the server wrote after its input ended: the answer to the call, which is the last message.
func lastToolBody(t *testing.T, c *liveMCP) (bool, map[string]any) {
	t.Helper()
	var last map[string]any
	for m := range c.from {
		last = m
	}
	if last == nil {
		t.Fatal("the server wrote no answer to the call")
	}
	return toolBody(t, last)
}

// What an agent passes never answers for the person: a remove, a confirm or an apply among the arguments (or in work) is refused
// before any question is sent, and a client that would say yes to every question is not even asked.
func TestMCPForgetTakesNoConfirmationFromItsArguments(t *testing.T) {
	for _, extra := range []map[string]any{
		{"remove": true}, {"confirm": true}, {"apply": true}, {"answer": "accept"}, {"action": "accept", "content": map[string]any{"remove": true}},
		{"yes": true}, {"elicitation": map[string]any{"remove": true}},
	} {
		w := newForgetWorld(t)
		before := w.snapshot()
		c := newLiveMCP(t, w.mcpHandler())
		c.initialize(map[string]any{"elicitation": map[string]any{}})
		c.callForget(2, forgetArgs(w, extra))
		failed, body := c.reply(2) // the next message is the answer: no question was sent
		if !failed || body["error"] == nil || !strings.Contains(body["error"].(string), "never an argument") {
			t.Errorf("%v: not refused: %v", extra, body)
		}
		c.finish()
		if after := w.snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("%v: the project changed: %v", extra, forgetDiff(before, after))
		}
	}
	// the same inside work: only work.session_id is read, and nothing there answers anything
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, map[string]any{"work": map[string]any{"remove": true, "confirm": true}}))
	q := c.question() // asked: work's other fields are not an answer
	c.send(answerTo(q, map[string]any{"action": "decline"}))
	if _, body := c.reply(2); body["status"] != "declined" {
		t.Errorf("a person's no was not taken: %v", body)
	}
	c.finish()
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

// shikii serve has no forget: the HTTP entry refuses it, whatever it is sent, and the project is as it was.
func TestHTTPRefusesForget(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	h := w.mcpHandler()
	for _, body := range []string{`{"operation":"forget"}`, `{"operation":"forget","sessions":["` + w.A + `"]}`, `{"operation":"forget","work":{"session_id":"x"}}`, `{"operation":"forget","remove":true}`} {
		code, out, err := callTool(h, "shikii_wiki", json.RawMessage(body))
		if err != nil || code < 400 {
			t.Errorf("HTTP ran %s: %d %s %v", body, code, out, err)
		}
	}
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

// The conditions are shikii forget's: the session that asks is never removed, whatever the environment of the server says; a session
// that was written to within ten minutes, or that nothing names, is refused; none of them is asked about.
func TestMCPForgetRefusesWhatTheCommandRefuses(t *testing.T) {
	t.Run("the session that asks", func(t *testing.T) {
		w := newForgetWorld(t)
		before := w.snapshot()
		c := newLiveMCP(t, w.mcpHandler())
		c.initialize(map[string]any{"elicitation": map[string]any{}})
		c.callForget(2, forgetArgs(w, map[string]any{"work": map[string]any{"session_id": w.A}}))
		failed, body := c.reply(2) // no question first
		if !failed || body["status"] != "refused" || !strings.Contains(body["message"].(string), "the session that asked for this removal") {
			t.Errorf("the session that asks was not refused: %v", body)
		}
		c.finish()
		if after := w.snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("the project changed: %v", forgetDiff(before, after))
		}
	})
	t.Run("a session written to a minute ago", func(t *testing.T) {
		w := newForgetWorld(t)
		now := time.Now()
		if err := os.Chtimes(filepath.Join(w.root, "sessions", w.A+".json"), now, now); err != nil {
			t.Fatal(err)
		}
		before := w.snapshot()
		c := newLiveMCP(t, w.mcpHandler())
		c.initialize(map[string]any{"elicitation": map[string]any{}})
		c.callForget(2, forgetArgs(w, nil))
		failed, body := c.reply(2)
		if !failed || body["status"] != "refused" || !strings.Contains(body["message"].(string), "may still be running") {
			t.Errorf("a session that may be running was not refused: %v", body)
		}
		c.finish()
		if after := w.snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("the project changed: %v", forgetDiff(before, after))
		}
	})
	t.Run("an id nothing names", func(t *testing.T) {
		w := newForgetWorld(t)
		before := w.snapshot()
		c := newLiveMCP(t, w.mcpHandler())
		c.initialize(map[string]any{"elicitation": map[string]any{}})
		c.callForget(2, map[string]any{"operation": "forget", "sessions": []string{"ses_typo"}})
		failed, body := c.reply(2)
		if !failed || !strings.Contains(body["message"].(string), "nothing in this project's .shikii/ names ses_typo") {
			t.Errorf("an id nothing names was not refused: %v", body)
		}
		c.finish()
		if after := w.snapshot(); !reflect.DeepEqual(before, after) {
			t.Errorf("the project changed: %v", forgetDiff(before, after))
		}
	})
	t.Run("no sessions, and a bad id", func(t *testing.T) {
		w := newForgetWorld(t)
		c := newLiveMCP(t, w.mcpHandler())
		c.initialize(map[string]any{"elicitation": map[string]any{}})
		for i, args := range []map[string]any{{"operation": "forget"}, {"operation": "forget", "sessions": []string{}}, {"operation": "forget", "sessions": []string{"a/../b"}}} {
			c.callForget(2+i, args)
			if failed, _ := c.reply(2 + i); !failed {
				t.Errorf("%v was not refused", args)
			}
		}
		c.finish()
	})
	// The server's own environment is not read: the variable names a session of an agent that started the server long ago.
	t.Run("the environment of the server is not read", func(t *testing.T) {
		w := newForgetWorld(t)
		t.Setenv("CLAUDE_CODE_SESSION_ID", w.A)
		t.Setenv("CODEX_THREAD_ID", w.A)
		before := w.snapshot()
		c := newLiveMCP(t, w.mcpHandler())
		c.initialize(map[string]any{"elicitation": map[string]any{}})
		c.callForget(2, forgetArgs(w, nil))
		c.send(answerTo(c.question(), accept(true)))
		if failed, body := c.reply(2); failed || body["status"] != "removed" {
			t.Fatalf("the environment of the server kept a session that the call did not name: %v", body)
		}
		c.finish()
		if problems := w.problems(before); len(problems) > 0 {
			t.Fatalf("forget left the project wrong:\n  %s", strings.Join(problems, "\n  "))
		}
	})
}

// A yes is a yes to the plan the person was shown. When the records changed while the person decided, what would be removed is no
// longer what they saw, so nothing is removed and the agent is told to list it again.
func TestMCPForgetRemovesNothingWhenThePlanChangedWhileAsking(t *testing.T) {
	w := newForgetWorld(t)
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	q := c.question()
	// the session writes again while the question is on the person's screen: a file of its own appears, so the plan lists one more
	extra := filepath.Join(w.root, "sessions", w.A+".files")
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	before := w.snapshot()
	c.send(answerTo(q, accept(true)))
	failed, body := c.reply(2)
	if !failed || body["status"] != "changed" || body["changed"] != false || !strings.Contains(body["message"].(string), "list it again") {
		t.Fatalf("a yes to another plan removed records: %v", body)
	}
	c.finish()
	// the locks taken for the application leave empty lock files, as a refusal on a lock does; no record changed
	if after := w.snapshot(); len(withoutLockFiles(forgetDiff(before, after))) > 0 {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

func withoutLockFiles(diff []string) []string {
	var out []string
	for _, line := range diff {
		if !strings.HasSuffix(line, ".lock") {
			out = append(out, line)
		}
	}
	return out
}

// The wait has a limit: when no answer comes, nothing is removed, the client is told the question is cancelled, and an answer that
// comes after the limit is passed over.
func TestMCPForgetStopsWaitingForAnAnswer(t *testing.T) {
	old := mcpAskLimit
	mcpAskLimit = 200 * time.Millisecond
	t.Cleanup(func() { mcpAskLimit = old })
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	q := c.question()
	cancelled := c.next() // the question is withdrawn from the person's screen
	if cancelled["method"] != "notifications/cancelled" || cancelled["params"].(map[string]any)["requestId"] != q["id"] {
		t.Fatalf("the client was not told the question is cancelled: %v", cancelled)
	}
	failed, body := c.reply(2)
	if !failed || body["status"] != "unanswered" || body["changed"] != false {
		t.Fatalf("no answer ended as %v", body)
	}
	c.send(answerTo(q, accept(true))) // too late
	c.pinged(3)                       // nothing came back for the late answer (not even an error)
	c.finish()
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

// While the question is open nothing else runs: a ping is answered, a call of a tool and any other request are told shikii is busy,
// and the person's answer still ends it.
func TestMCPForgetAnswersOtherRequestsWhileItWaits(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	q := c.question()
	c.pinged(3)
	c.send(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/list"})
	if m := c.next(); m["id"] != float64(4) || m["error"] == nil || !strings.Contains(m["error"].(map[string]any)["message"].(string), "busy") {
		t.Errorf("tools/list was not told shikii is busy: %v", m)
	}
	c.send(map[string]any{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "shikii_status", "arguments": map[string]any{}}})
	if failed, body := c.reply(5); !failed || !strings.Contains(body["error"].(string), "busy") {
		t.Errorf("a tool call was not told shikii is busy: %v", body)
	}
	c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/roots/list_changed"}) // a notification passes
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("the project changed while it waited: %v", forgetDiff(before, after))
	}
	c.send(answerTo(q, accept(true)))
	if failed, body := c.reply(2); failed || body["status"] != "removed" {
		t.Fatalf("the person's yes was not taken after the other requests: %v", body)
	}
	c.finish()
}

// When the client cancels the call that asked, the question is withdrawn, the call gets no answer, and nothing is removed.
func TestMCPForgetEndsWhenTheClientCancelsTheCall(t *testing.T) {
	w := newForgetWorld(t)
	before := w.snapshot()
	c := newLiveMCP(t, w.mcpHandler())
	c.initialize(map[string]any{"elicitation": map[string]any{}})
	c.callForget(2, forgetArgs(w, nil))
	q := c.question()
	c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 2}})
	if m := c.next(); m["method"] != "notifications/cancelled" || m["params"].(map[string]any)["requestId"] != q["id"] {
		t.Fatalf("the question was not withdrawn: %v", m)
	}
	c.pinged(3) // the call of id 2 got no answer: the ping's is next
	c.send(answerTo(q, accept(true)))
	c.pinged(4)
	c.finish()
	if after := w.snapshot(); !reflect.DeepEqual(before, after) {
		t.Errorf("the project changed: %v", forgetDiff(before, after))
	}
}

// An agent that cannot be refused by name: forget is an operation of the definition, with the sessions it takes and what to read in
// the reply, and the definition says the person is asked.
func TestMCPDefinitionListsForget(t *testing.T) {
	found := false
	for _, op := range mcpWikiOperations(t) {
		found = found || op == "forget"
	}
	if !found {
		t.Fatal("forget is not in the enum of shikii_wiki")
	}
	var schema map[string]any
	var description string
	for _, tool := range mcpTools {
		if tool["name"] == "shikii_wiki" {
			schema = tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
			description = tool["description"].(string)
		}
	}
	sessions, _ := schema["sessions"].(map[string]any)
	if sessions == nil || sessions["type"] != "array" {
		t.Fatalf("shikii_wiki has no sessions: %v", sessions)
	}
	for _, want := range []string{"the answer is never an argument", "work.session_id", "status and changed"} {
		if !strings.Contains(sessions["description"].(string), want) {
			t.Errorf("sessions does not say %q: %s", want, sessions["description"])
		}
	}
	if !strings.Contains(description, "forget removes finished sessions' records after shikii asks the person") {
		t.Errorf("the description of shikii_wiki does not say the person is asked")
	}
}

// mcpForgetJSONEntry is the row of forget in jsonEntries: the arguments of the call are read by the one reader, so a text that does not
// parse is refused in its words, before any question is sent (the connection has no stream).
func mcpForgetJSONEntry(t *testing.T, h http.Handler, input string) string {
	t.Helper()
	answer, answered := (&mcpConn{}).forget(json.RawMessage("1"), h, json.RawMessage(input))
	if !answered || answer["isError"] != true {
		t.Errorf("forget did not refuse %q: %v", input, answer)
		return ""
	}
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(answer["content"].([]map[string]any)[0]["text"].(string)), &refusal)
	return refusal.Error
}
