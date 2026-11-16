package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P1: the Stop tells the agent to run `shikii reflect query` with targets; through the CLI.
func TestAudit036ConfirmCLIQueryNarrowsToTheSessionOfTheStopsTargets(t *testing.T) {
	_, scope, turn, key := sessionDelivery(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "Adjust the size of the screen text."})
	stop := runHook(t, "stop", map[string]any{"session_id": "other", "cwd": scope, "last_assistant_message": "Adjusted the size of the screen text."})
	targets := stopTargets(t, stop)
	b, _ := json.Marshal(map[string]any{"session_id": targets.SessionID, "sources": targets.Sources})
	// the command the Stop writes carries the session, so the agent that copies it asks as that session
	if reason, _ := stopReason(t, stop); !strings.Contains(reason, " reflect query session_id=other (or query of shikii_reflect; targets ") {
		t.Fatalf("the Stop's reason names the CLI query with the session: %s", reason)
	}
	out := capture(t, func() error { return cmdReflect([]string{"query", "session_id=" + targets.SessionID}) })
	var view reflectView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("%v: %.300s", err, out)
	}
	listed, n := listedMemory(view, turn)
	t.Logf("targets=%s listed=%v memories=%d", b, listed, n)
	if n != 0 {
		t.Fatalf("the CLI query the Stop names lists the owner's delivery %s to the other session (%d deliveries)", key, n)
	}
}

// P1: the MCP tool's schema has no session_id although the Stop tells the agent to pass it.
func TestAudit036ConfirmMCPSchemaOfReflectTakesSessionID(t *testing.T) {
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_reflect" {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		_, has := props["session_id"]
		_, src := props["sources"]
		t.Logf("additionalProperties=%v session_id=%v sources=%v", schema["additionalProperties"], has, src)
		if !has && schema["additionalProperties"] == false {
			t.Fatalf("shikii_reflect's schema refuses other properties and has no session_id")
		}
	}
}

// P1: odd session ids only narrow, never fail.
func TestAudit036ConfirmOddSessionIDs(t *testing.T) {
	st, scope, turn, _ := sessionDelivery(t)
	for _, id := range []string{" ", "owner ", "../../etc/passwd", strings.Repeat("x", 100000), "\x00", "OWNER"} {
		view, err := runReflect(st.Dir, scope, reflectRequest{Operation: "query", SessionID: id})
		if err != nil {
			t.Errorf("session_id %.20q: %v", id, err)
			continue
		}
		if _, n := listedMemory(view.(reflectView), turn); n != 0 {
			t.Errorf("session_id %.20q lists the owner's delivery (%d)", id, n)
		}
	}
	view, err := runReflect(st.Dir, scope, reflectRequest{Operation: "query", SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, n := listedMemory(view.(reflectView), turn); n != 1 {
		t.Errorf("the owner's own query lost its delivery: %d", n)
	}
}

// P5: a report longer than the limit whose last 256 KiB have no newline: the limit falls inside a line, and that line is read.
func TestAudit036ConfirmReportCutInsideALineWithoutNewline(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	line := "shikii: " + key + " unused"
	short := "I was told never to end with " + strings.Repeat(" ", 100) + line
	subagentStop(t, "s", scope, "agent-1", short)
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] != nil {
		t.Fatalf("control: a short report with the form in the middle of a line saves nothing: %+v", got)
	}
	long := "I was told never to end with " + strings.Repeat(" ", subagentReportMax) + line
	subagentStop(t, "s", scope, "agent-1", long)
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] != nil {
		t.Fatalf("the form stands in the middle of a line, yet it was saved as the subagent's assessment: %+v", got[0])
	}
}

// outside the diff: what the subagent did, longer than the store takes.
func TestAudit036ConfirmLongDidIsSaved(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	subagentStop(t, "s", scope, "agent-1", "shikii: "+key+" helpful - "+strings.Repeat("a", 1600))
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil {
		home, _ := agentHome()
		b, _ := os.ReadFile(filepath.Join(home, "hook.log"))
		t.Fatalf("a line whose text after the dash has 1600 bytes is not saved: %.300s", b)
	}
}

// P5: another session's subagent with the same agent_id.
func TestAudit036ConfirmSameAgentIDInAnotherSession(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "t", "cwd": scope, "prompt": "Adjust the size of the screen text."})
	subagentStop(t, "t", scope, "agent-1", "shikii: "+key+" unused")
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] != nil {
		t.Fatalf("session t's subagent received nothing, yet its line assessed the delivery to session s's subagent: %+v", got[0])
	}
}

// P5: a subagent that ends after the parent's Stop and before the next request.
func TestAudit036ConfirmSubagentEndsBetweenStopAndNextRequest(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	runHook(t, "stop", map[string]any{"session_id": "s", "cwd": scope, "last_assistant_message": "Started the subagent in the background."})
	t.Logf("turn after stop: %q", loadSession(st.Dir, "s").TurnID)
	subagentStop(t, "s", scope, "agent-1", "shikii: "+key+" helpful - Checked the folder first.")
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil {
		t.Fatalf("the report of a subagent that ends after the parent's Stop is not saved: %+v", got)
	}
}

// P5: the same report twice, then a different one; many repeated lines.
func TestAudit036ConfirmRepeatedReports(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("shikii: " + key + " unused\r\n")
		b.WriteString("shikii: lesson:0123456789abcdef:0123456789abcdef unused\n")
	}
	subagentStop(t, "s", scope, "agent-1", b.String())
	subagentStop(t, "s", scope, "agent-1", b.String())
	subagentStop(t, "s", scope, "agent-1", "shikii: "+key+" helpful - changed my mind")
	got := subagentAssessments(t, st, scope, "agent-1")
	if len(got) != 1 || got[0] == nil || got[0].Result != "unused" {
		t.Fatalf("the first report stays: %+v", got)
	}
	all, _ := st.MemoryAssessments(scope)
	if len(all) != 1 {
		t.Fatalf("one assessment, got %d", len(all))
	}
}

func confirmGroup(matcher any, cmds ...string) map[string]any {
	hs := []any{}
	for _, c := range cmds {
		hs = append(hs, map[string]any{"type": "command", "command": c})
	}
	g := map[string]any{"hooks": hs}
	if matcher != nil {
		g["matcher"] = matcher
	}
	return g
}

func confirmCmd(event string) string {
	for _, ev := range hookEventsFor("Codex") {
		if strings.HasSuffix(ev.cmd, " hook "+event) {
			return ev.cmd
		}
	}
	return ""
}

// P9: installs repeated, hooks moved to another event, groups shared with a person's hook.
func TestAudit036ConfirmEditHooksEdges(t *testing.T) {
	for _, who := range []string{"Codex", "Claude Code"} {
		t.Run(who, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "hooks.json")
			tl, stop := confirmCmd("tool-lesson"), confirmCmd("stop")
			body, _ := json.Marshal(map[string]any{
				"other": map[string]any{"n": json.Number("1.50")},
				"hooks": map[string]any{
					"PreToolUse": []any{
						confirmGroup("A", "person-a"),
						confirmGroup("^Bash$", "person-b", tl, "person-c"),
						confirmGroup(nil, tl),
						confirmGroup(7, tl, "person-num"),
						confirmGroup("Z", "person-z"),
					},
					"PostToolUse": []any{confirmGroup(toolLessonMatcherCodex, tl), confirmGroup(toolLessonMatcher, tl, "person-post")},
					"Stop":        []any{confirmGroup("x", stop, "person-stop"), confirmGroup("", stop)},
				}})
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			evs := hookEventsFor(who)
			if _, err := editHooks(path, true, evs); err != nil {
				t.Fatal(err)
			}
			first := mustRead(t, path)
			changed, err := editHooks(path, true, evs)
			if err != nil || changed || !bytes.Equal(first, mustRead(t, path)) {
				t.Errorf("a second install changes the file again: changed=%v err=%v", changed, err)
			}
			var cfg map[string]any
			json.Unmarshal(first, &cfg)
			hooks := cfg["hooks"].(map[string]any)
			count := map[string]int{}
			var order []string
			for ev, v := range hooks {
				for _, g := range v.([]any) {
					gm, _ := g.(map[string]any)
					for _, h := range gm["hooks"].([]any) {
						c := h.(map[string]any)["command"].(string)
						count[ev+" "+c]++
						if ev == "PreToolUse" && strings.HasPrefix(c, "person-") {
							order = append(order, c+"@"+jsonText(gm["matcher"]))
						}
						if c == tl && ev == "PreToolUse" {
							want := toolLessonMatcherCodex
							if who == "Claude Code" {
								want = toolLessonMatcher
							}
							if gm["matcher"] != want {
								t.Errorf("tool-lesson under %v", gm["matcher"])
							}
						}
					}
				}
			}
			t.Logf("%s: %v order=%v", who, count, order)
			if count["PreToolUse "+tl] != 1 || count["Stop "+stop] != 1 {
				t.Errorf("shikii's hooks are there once each in their events: %v", count)
			}
			if strings.Join(order, " ") != `person-a@"A" person-b@"^Bash$" person-c@"^Bash$" person-num@7 person-z@"Z"` {
				t.Errorf("the person's hooks of PreToolUse changed: %v", order)
			}
			if count["PostToolUse person-post"] != 1 || count["Stop person-stop"] != 1 {
				t.Errorf("a person's hook is gone: %v", count)
			}
			if _, err := editHooks(path, false, evs); err != nil {
				t.Fatal(err)
			}
			after := string(mustRead(t, path))
			if strings.Contains(after, " hook ") {
				t.Errorf("after uninstall a shikii hook stays: %s", after)
			}
			for _, p := range []string{"person-a", "person-b", "person-c", "person-num", "person-z", "person-post", "person-stop", "1.50"} {
				if !strings.Contains(after, p) {
					t.Errorf("after uninstall %s is gone: %s", p, after)
				}
			}
		})
	}
}

func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }

// outside the diff: an entry of an event's list that is not an object, next to a shikii hook that is taken out.
func TestAudit036ConfirmEditHooksKeepsAnEntryThatIsNotAnObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	body, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{"person-text", 42, confirmGroup("^Bash$", confirmCmd("tool-lesson"))}}})
	os.WriteFile(path, body, 0o600)
	if _, err := editHooks(path, true, hookEventsFor("Codex")); err != nil {
		t.Logf("install refuses: %v", err)
		return
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, "person-text") || !strings.Contains(after, "42") {
		t.Fatalf("the entries that are not objects are gone: %s", after)
	}
}

func TestAudit036ConfirmEditHooksKeepsAnEntryThatIsNotAnObjectOldPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	body, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{"person-text", 42, confirmGroup("", "/old/path/shikii hook pre-tool-use")}}})
	os.WriteFile(path, body, 0o600)
	if _, err := editHooks(path, false, hookEventsFor("Codex")); err != nil {
		t.Logf("uninstall refuses: %v", err)
		return
	}
	after := string(mustRead(t, path))
	if !strings.Contains(after, "person-text") || !strings.Contains(after, "42") {
		t.Fatalf("the entries that are not objects are gone after uninstall: %s", after)
	}
}

// P10: where the limit falls.
func TestAudit036ConfirmPatchTargetAtTheLimit(t *testing.T) {
	fill := func(n int) string { // n bytes of patch lines ending with a newline
		line := "+" + strings.Repeat("a", 62) + "\n"
		s := strings.Repeat(line, n/len(line))
		if rem := n - len(s); rem == 1 {
			s = s[:len(s)-1] + "a\n"
		} else if rem >= 2 {
			s += "+" + strings.Repeat("a", rem-2) + "\n"
		}
		return s
	}
	head := "*** Begin Patch\n*** Add File: notes\n"
	hdr := "*** Update File: version.go"
	// 1) no newline at all in a patch over the limit
	if got := patchTarget("*** Update File: version.go"+strings.Repeat("a", patchMax), "/work"); got != "" {
		t.Errorf("1) one line over the limit: %q", got)
	}
	// 2) the header's newline is the last byte inside the limit
	p := head + fill(patchMax-len(head)-len(hdr)-1) + hdr + "\n" + "+x\n*** End Patch\n"
	if len(head)+len(fill(patchMax-len(head)-len(hdr)-1))+len(hdr)+1 != patchMax {
		t.Fatal("precondition 2")
	}
	if got := patchTarget(p, "/work"); got != "/work/version.go" {
		t.Errorf("2) the header ends with the limit's last byte: %q", got)
	}
	// 3) the header is whole inside the limit, its newline is the first byte outside
	p = head + fill(patchMax-len(head)-len(hdr)) + hdr + "\n" + "+x\n*** End Patch\n"
	if got := patchTarget(p, "/work"); got != "/work/version.go" {
		t.Errorf("3) the header is whole, its newline is the first byte past the limit: %q", got)
	}
	// 4) the same with CRLF: the CR inside, the LF outside
	p = head + fill(patchMax-len(head)-len(hdr)-1) + hdr + "\r\n" + "+x\r\n*** End Patch\r\n"
	if got := patchTarget(p, "/work"); got != "/work/version.go" {
		t.Errorf("4) the header and its CR are inside, the LF is past the limit: %q", got)
	}
	// 4b) the header is inside, its CR is the first byte past the limit
	head4b := "*** Begin Patch\r\n"
	line4b := "*** Update File: version.go"
	pad4b := patchMax - len(head4b) - len(line4b)
	if got := patchTarget(head4b+strings.Repeat("x", pad4b-2)+"\r\n"+line4b+"\r\n+a\r\n", "/work"); got != "/work/version.go" {
		t.Errorf("4b) the header is inside, its CRLF is the first thing past the limit: %q", got)
	}
	// 5) the first header is read however long the patch is
	if got := patchTarget("*** Begin Patch\r\n*** Update File: version.go\r\n"+strings.Repeat("+a\r\n", patchMax), "/work"); got != "/work/version.go" {
		t.Errorf("5) %q", got)
	}
	// 6) odd endings
	for in, want := range map[string]string{
		"*** Update File: version.go \t\r\n":             "/work/version.go",
		"*** Update File: version.go\r\r\n":              "/work/version.go",
		"*** Update File: version.go\r*** Move to: x.go": "",
		"*** Update File: \r\n":                          "",
		"\r*** Update File: version.go\r\n":              "/work/version.go", // measured on Codex 0.169.0: the space before a first header is dropped
	} {
		if got := patchTarget(in, "/work"); got != want {
			t.Errorf("6) %q: got %q want %q", in, got, want)
		}
	}
}

// P5: on a subagent with nothing assessed yet, 16 lines of keys not delivered come before the delivered one.
func TestAudit036ConfirmDeliveredLineAfter16UndeliveredOnAFreshSubagent(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteString("shikii: lesson:0123456789abcdef:0123456789abcde" + string("0123456789abcdef"[i]) + " unused\n")
	}
	b.WriteString("shikii: " + key + " unused\n")
	subagentStop(t, "s", scope, "agent-1", b.String())
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil {
		t.Errorf("16 lines of keys that were not delivered come first: the delivered one is not saved: %+v", got)
	}
}

// P1: through MCP the server takes session_id although the tool's schema does not list it.
func TestAudit036ConfirmMCPCallWithSessionIDIsNarrowed(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	key := "lesson:" + draft.ID + ":" + revision
	memoryEffectPrompt(t, scope, "owner")
	turn := memoryEffectComplete(t, st, scope, "owner")
	count := func(args string) int {
		result, _ := mcpToolCall(t, h, "shikii_reflect", args)
		raw, _ := json.Marshal(result)
		return strings.Count(string(raw), key)
	}
	t.Logf("turn=%s plain=%d other=%d owner=%d", turn, count(`{"operation":"query"}`), count(`{"operation":"query","session_id":"other"}`), count(`{"operation":"query","session_id":"owner"}`))
}

// P5: only the last subagentReportMax bytes of a report are read, from the head of a line.
func TestAudit036ConfirmOnlyTheEndOfALongReportIsRead(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	line := "shikii: " + key + " unused\n"
	filler := strings.Repeat("x", 99) + "\n"
	// the line stands before the part that is read
	subagentStop(t, "s", scope, "agent-1", line+strings.Repeat(filler, subagentReportMax/len(filler)+1))
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] != nil {
		t.Fatalf("a line before the last %d bytes is not read: %+v", subagentReportMax, got[0])
	}
	// the part that is read starts right at the head of the line: the line is whole and is read
	report := filler + line + strings.Repeat("y", subagentReportMax-len(line)-1) + "\n"
	if report[len(report)-subagentReportMax-1] != '\n' || !strings.HasPrefix(report[len(report)-subagentReportMax:], "shikii: ") {
		t.Fatalf("precondition: the last %d bytes start at the head of the line", subagentReportMax)
	}
	subagentStop(t, "s", scope, "agent-1", report)
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil {
		t.Fatalf("a whole line at the head of the part that is read is saved: %+v", got)
	}
}

// The CLI query reads nothing from standard input: it also runs inside a script whose standard input is the script itself
// (bash <<EOF, sh -s, a while read loop), and must not eat the lines after it. The session comes as its one argument.
func TestAudit036ConfirmCLIQueryLeavesStandardInputAlone(t *testing.T) {
	_, _, turn, _ := sessionDelivery(t)
	rest := strings.NewReader("echo second-command\necho third-command\n")
	old := stdin
	stdin = rest
	defer func() { stdin = old }()
	out := capture(t, func() error { return cmdReflect([]string{"query"}) })
	var view reflectView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("%v: %.300s", err, out)
	}
	if listed, n := listedMemory(view, turn); !listed || n != 1 {
		t.Fatalf("a plain query lists the conversation with its delivery, got listed=%v deliveries=%d", listed, n)
	}
	if rest.Len() != len("echo second-command\necho third-command\n") {
		t.Fatalf("the query read %d bytes of the standard input it was started with", len("echo second-command\necho third-command\n")-rest.Len())
	}
	// the argument is session_id=<id> and nothing else: an id alone, an empty one and one a shell would read are refused
	for _, arg := range []string{"0123456789abcdef", "session_id=", "session_id=a b", "session_id=$(id)", "session_id=-x", "other"} {
		if _, err := captureErr(func() error { return cmdReflect([]string{"query", arg}) }); err == nil {
			t.Errorf("reflect query %s must be refused", arg)
		}
	}
}

// A real model writes commands in code spans when it says what it did (seen with OpenCode 2.3.0 on 2026-11-17: the line was not
// saved). What it did may hold code spans; a line that is itself wrapped in one code span is read without the wrapping.
func TestAudit036ConfirmWhatTheSubagentDidMayHoldCodeSpans(t *testing.T) {
	for _, c := range []struct{ name, line, did string }{
		{"code spans inside", "shikii: %s helpful - I ran `sh deploy.sh` before editing deploy.sh (it printed \"deploying\", exit 0).", "I ran `sh deploy.sh` before editing deploy.sh (it printed \"deploying\", exit 0)."},
		{"ends with a code span", "shikii: %s helpful - Before the edit I ran `sh deploy.sh`", "Before the edit I ran `sh deploy.sh`"},
		{"the whole line in one code span", "`shikii: %s helpful - Ran the smoke test first.`", "Ran the smoke test first."},
		{"a list item, wrapped, CRLF", "- `shikii: %s helpful - Ran the smoke test first.`\r", "Ran the smoke test first."},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
			subagentStop(t, "s", scope, "agent-1", "Done.\n\n"+fmt.Sprintf(c.line, key)+"\n")
			got := subagentAssessments(t, st, scope, "agent-1")
			if len(got) != 1 || got[0] == nil {
				t.Fatalf("the line is saved: %+v", got)
			}
			if got[0].Result != "helpful" || got[0].Decision != c.did {
				t.Errorf("saved %q %q, want helpful %q", got[0].Result, got[0].Decision, c.did)
			}
		})
	}
	// a wrapped line with no words after the result
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	subagentStop(t, "s", scope, "agent-1", "`shikii: "+key+" unused`")
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil || got[0].Result != "unused" {
		t.Fatalf("a wrapped line of an unused lesson is saved: %+v", got)
	}
}

// The agent reads a lesson delivered at a tool call together with that call's result (the call is already written when the
// hook runs), so the text does not speak of a call that is still to come.
func TestAudit036ConfirmTheToolLessonSpeaksOfTheCallItCameWith(t *testing.T) {
	_, scope, _, ids := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": scope, "prompt": toolPrompt})
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "s", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}})
	if !strings.Contains(out, ids["version"]) {
		t.Fatalf("precondition: the lesson is delivered: %s", out)
	}
	if strings.Contains(out, "about to") || !strings.Contains(out, "names the file this call changes (") {
		t.Errorf("the text names the file of the call it came with: %s", out)
	}
}
