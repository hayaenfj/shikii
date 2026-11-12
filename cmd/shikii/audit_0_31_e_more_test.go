package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests beside the findings of audit E of 0.31.0 (third round): what the fixes also promise.

// A report of the earlier request that waits among the pending observations is handed to the next preparation as evidenceN. It is
// noted there, so a refusal that lists what evidence can cite does not name it.
func TestAuditEAReportAmongThePendingObservationsIsNotListedAsUsable(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	done, err := f.record(first, map[string]any{
		"result":    map[string]any{"changed": "Edited the export.", "checked": "Looked at it."},
		"reasoning": map[string]any{"phase": "finish", "focus": "Review the export", "assessment": "retain", "reason": "The export was edited.", "evidence": []any{"current"}, "next_action": "None.", "result": "Done."}})
	if err != nil || done["complete"] != true {
		t.Fatalf("precondition: the result and the agent's ending review are recorded: %v %.400s", err, auditJSON(done))
	}
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	w := loadSession(f.root, "planning").Planning.Work
	report := ""
	for ref, id := range w.Sources {
		if strings.HasPrefix(ref, "evidence") && w.Reports[id] {
			report = ref
		}
	}
	if report == "" {
		t.Fatalf("a report of the earlier request is among the pending observations, and the preparation does not note it: sources=%v reports=%v", w.Sources, w.Reports)
	}
	// new9 names an observation this call does not send: the refusal lists the references evidence can cite
	refused, err := f.record(second, map[string]any{"result": map[string]any{"changed": "Checked the totals.", "evidence": []any{"new9"}}})
	if raw := auditJSON(refused["errors"]); err != nil || !strings.Contains(raw, "unknown source reference") || strings.Contains(raw, report) {
		t.Errorf("the refusal of an unknown reference lists %s, a report, among what evidence can cite: %v %s", report, err, raw)
	}
}

// A request that is nearly all characters JSON escapes (six bytes for one <) does not fit the largest reply even alone: the task
// is cut, says so as a request longer than what the hook keeps does, and the agent holds a receipt.
func TestAuditEARequestOfNothingButEscapesIsCutAndSaysSo(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prompt := "Check the CSV export table. " + strings.Repeat("<", 65000)
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	var out string
	var err error
	withStdin(t, strings.TrimSuffix(auditIDs.FindString(notice), "}")+`,"query":"CSV export filters"}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
	if err != nil {
		t.Fatalf("the preparation is refused, and the agent holds no receipt: %v", err)
	}
	var reply map[string]any
	if e := json.Unmarshal([]byte(out), &reply); e != nil {
		t.Fatalf("%v: %.200s", e, out)
	}
	task, _ := reply["task"].(string)
	context, _ := reply["task_context"].(map[string]any)
	if reply["receipt"] == nil || len(task) == 0 || len(task) >= len(prompt) || !strings.HasPrefix(prompt, task) || context["text_status"] != "stored_request_truncated" || context["next_action"] == nil {
		t.Errorf("the task is a head of the request and says that it is cut: receipt=%v task %d of %d bytes, status=%v next_action=%v", reply["receipt"] != nil, len(task), len(prompt), context["text_status"], context["next_action"])
	}
	if len(out) > workSourceBudget*3 {
		t.Errorf("the reply is %d bytes, over the largest bound a caller can name (%d)", len(out), workSourceBudget*3)
	}
}

// The block has the approval line and the note is missing: the note is written when it can be, and when it cannot (here ~/.shikii
// is a file) nothing stops and the person's file stays as it is.
func TestAuditEANoteThatCannotBeWrittenStopsNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".shikii"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "[mcp_servers.shikii]\ncommand = \"shikii\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n"
	if err := os.WriteFile(config, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := allowCodexTools(config); err != nil {
		t.Errorf("a note that cannot be written stops the caller: %v", err)
	}
	if got, _ := os.ReadFile(config); string(got) != text {
		t.Errorf("the person's file changed:\n%s", got)
	}
}
