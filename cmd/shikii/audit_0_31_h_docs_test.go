package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Audit H of 0.31.0 (fourth round): documents and refusals against what the code does now.

const auditHSession = "0f4c1e2a-7b3d-4c5e-8f90-123456789abc" // 36 bytes, as a real session id

// auditHLine is the one line of a file that holds the needle (the test fails when it is gone).
func auditHLine(t *testing.T, path, needle string) string {
	t.Helper()
	for _, line := range strings.Split(string(mustRead(t, path)), "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("%s has no line with %q", path, needle)
	return ""
}

// H-1 [INVARIANT 14, INVARIANT 4]: recording.md says the task and its context fit 128 KiB as JSON or the preparation fails
// explicitly. Since this round the task takes the largest reply less 16 KiB (376832 bytes as JSON), and beyond that it is cut and
// task_context says so: the preparation does not fail. An agent that reads the reference expects a refusal it never gets, or
// treats the 128 KiB as the bound of the task.
func TestAuditHRecordingSaysTheTaskFailsOver128KiB(t *testing.T) {
	line := auditHLine(t, "../../skills/shikii/references/recording.md", "Read `task_context` with `task`")
	const claim = "Task and conversation context each fit 128 KiB after JSON encoding or preparation fails explicitly"
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	// 40 KiB of a pasted table: 240 KiB as JSON
	prompt := "Check the CSV export table. " + strings.Repeat("<td>&</td>", 4<<10)
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditHSession, "cwd": repo, "prompt": prompt}))
	prepared, out, err := auditDCLI(t, strings.TrimSuffix(noticeIDs(t, notice), "}")+`,"query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatalf("precondition: the preparation succeeds: %v", err)
	}
	encoded, _ := json.Marshal(map[string]any{"task": prepared["task"], "task_context": prepared["task_context"]})
	task, _ := prepared["task"].(string)
	t.Logf("request %d bytes; task %d bytes; task and task_context are %d bytes as JSON (128 KiB = %d); reply %d bytes; receipt=%v", len(prompt), len(task), len(encoded), 128<<10, len(out), prepared["receipt"] != nil)
	if strings.Contains(line, claim) && len(encoded) > 128<<10 && prepared["receipt"] != nil {
		t.Errorf("INVARIANT 14: recording.md says %q, and a preparation whose task and task_context are %d bytes as JSON succeeds with a receipt", claim, len(encoded))
	}
	// and the line says what happens instead: the bound of the task, and how a cut task says so (another wrong sentence in place of
	// the old one passed the check above: audit J of 0.31.0)
	for _, want := range []string{"The task takes up to the largest reply less 16 KiB after JSON encoding", "comes cut, and `task_context.text_status` says `stored_request_truncated`"} {
		if !strings.Contains(line, want) {
			t.Errorf("INVARIANT 14: recording.md does not say %q", want)
		}
	}
	if context, _ := prepared["task_context"].(map[string]any); context["text_status"] != "stored_request_truncated" && len(task) < len(prompt) {
		t.Errorf("a task cut to %d of %d bytes does not say so: %v", len(task), len(prompt), context["text_status"])
	}
}

// H-2 [INVARIANT 4, INVARIANT 14]: INVARIANT 4 says the documents that say a long request is not cut also say that the hook keeps
// 64 KiB of it. docs/guide.md and guide.ja.md say it in one paragraph (line 180 / 31) and not in the other (line 645 / 599): there a

// requestNotCut finds a sentence that says a request is not cut: "request ... not cut" in English, a request (\u4f9d\u983c) and
// "not cut" (\u5207\u3089\u305a) right after it in Japanese.
var requestNotCut = regexp.MustCompile("(?i)request[^.;]*not cut|\u4f9d\u983c(\u3092|\u306f)\u5207\u3089\u305a")

// longer request "is not cut", while a request over 64 KiB comes cut.
func TestAuditHGuideSaysALongerRequestIsNotCut(t *testing.T) {
	// every line of the documents an agent or a person reads that says a longer request is not cut (an allowlist of the documents,
	// not of the lines: a new line that says so is checked too)
	lines := map[string]string{}
	var docs []string
	for _, pattern := range []string{"../../docs/*.md", "../../skills/shikii/*.md", "../../skills/shikii/references/*.md", "../../CHANGELOG*.md", "../../README.md", "../../docs/readme/*.md", "../../integrations/*.md"} {
		paths, _ := filepath.Glob(pattern)
		docs = append(docs, paths...)
	}
	if len(docs) < 10 {
		t.Fatalf("the documents are not found: %v", docs)
	}
	for _, path := range docs {
		for i, line := range strings.Split(string(mustRead(t, path)), "\n") {
			if requestNotCut.MatchString(line) {
				lines[fmt.Sprintf("%s:%d", path, i+1)] = line
			}
		}
	}
	if len(lines) < 4 {
		t.Fatalf("the lines that say a longer request is not cut are not found: %v", lines)
	}
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prompt := "Use the agreed CSV columns. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", (70<<10)/56)
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": auditHSession, "cwd": repo, "prompt": prompt}))
	prepared, err := agentCLI(t, strings.TrimSuffix(noticeIDs(t, notice), "}")+`,"query":"CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	task, _ := prepared["task"].(string)
	if len(task) >= len(prompt) {
		t.Fatalf("precondition: a request of %d bytes comes cut: task %d bytes", len(prompt), len(task))
	}
	for path, line := range lines {
		says := true
		names := strings.Contains(line, "64 KiB") || strings.Contains(line, "task_context")
		t.Logf("%s: %.200s", path, line)
		if says && !names {
			t.Errorf("INVARIANT 4: %s says a request longer than the CLI's bound is not cut, without the hook's 64 KiB; a request of %d bytes comes as %d bytes", path, len(prompt), len(task))
		}
	}
}

// H-3 (U): a receipt without hook ids sent with agreed, open or continues_previous. The whole call is refused (the result in it
// too) with "use the current hook context for planning": an agent without a hook has no such context, and the refusal does not
// say what to send instead (the same call without those fields). The changelog says a call shikii cannot accept is answered with
// the cause and what to send instead.
func TestAuditHStandaloneAgreedRefusalSaysWhatToSend(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prepared, err := agentCLI(t, `{"query":"CSV export","request":"Check the CSV export"}`, "work", "prepare")
	if err != nil {
		t.Fatal(err)
	}
	receipt := auditJSON(prepared["receipt"])
	result := `"result":{"changed":"Nothing.","checked":"Read the export."}`
	for name, extra := range map[string]string{
		"agreed and open unknown (planning.md's minimal record)": `"agreed":{"unknown":true},"open":{"unknown":true},`,
		"continues_previous alone":                               `"continues_previous":false,`,
	} {
		_, _, err := auditDCLI(t, `{"receipt":`+receipt+`,`+extra+result+`}`, "work", "record")
		t.Logf("%s -> %v", name, err)
		// refused, and the refusal names what to leave out and to send the rest ("without" alone matched "a receipt without hook
		// ids" in a refusal that said nothing of what to send: audit J of 0.31.0)
		if err == nil || !strings.Contains(err.Error(), "leave agreed, open and continues_previous out") || !strings.Contains(err.Error(), "send the rest again") {
			t.Errorf("%s: the record is not refused with what to send instead (the same record without agreed, open and continues_previous): %v", name, err)
		}
	}
	// negative: the same record without them is taken
	if saved, err := agentCLI(t, `{"receipt":`+receipt+`,`+result+`}`, "work", "record"); err != nil || saved["complete"] != true {
		t.Errorf("negative: the record without agreed is taken: %v %v", err, saved)
	}
}

// The documents that list the steps of the bound of reflect query name the step that keeps the delivered memories as keys only
// (reflectBare), as the output of a reply that took it says (audit H of 0.31.0, U-11).
func TestTheDocumentsNameTheStepOfBareDeliveries(t *testing.T) {
	for path, words := range map[string]string{
		"../../skills/shikii/references/review.md": "the delivered memories keep their keys only",
		"../../docs/guide.md":                      "the delivered memories keep their keys only",
		"../../docs/guide.ja.md":                   "\u5c4a\u3044\u305f\u8a18\u61b6\u3092\u3001\u305d\u308c\u305e\u308c\u3092\u898b\u5206\u3051\u308b\u8b58\u5225\u5b50\u3060\u3051",
	} {
		if !strings.Contains(string(mustRead(t, path)), words) {
			t.Errorf("%s does not name the step that keeps the delivered memories as keys only (%q)", path, words)
		}
	}
	if !strings.Contains(bareNote, "keeps its source_id and key") {
		t.Errorf("the output of the step no longer says what stays: %s", bareNote)
	}
}
