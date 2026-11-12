package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// audit G (round 4), INVARIANTS 4: a request the hook keeps (64 KiB) that is nearly all characters JSON writes in six bytes. The
// task is cut where it fits, text_status says so, and the CLI's default call still returns a receipt within the largest bound.
func TestAuditGCLIPrepareWithARequestOfSixByteCharacters(t *testing.T) {
	for _, c := range []struct {
		name, unit string
	}{
		{"angle brackets", "<"},
		{"ampersands and a three-byte character", "&&&&&\u65e5"},
		{"control characters", "\x01\x02\x03"},
		{"line separators", "\u2028<"},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			t.Chdir(repo)
			prompt := "Fix the table.\n" + strings.Repeat(c.unit, (64<<10)/len(c.unit))
			notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
			ids := auditIDs.FindString(notice)
			if ids == "" {
				t.Fatalf("the notice carries the ids: %.300s", notice)
			}
			var out string
			var err error
			withStdin(t, strings.TrimSuffix(ids, "}")+`,"query":"export page table"}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
			if err != nil {
				t.Errorf("INVARIANT 4: the CLI default prepare is refused for a %d-byte request of six-byte characters: %v", len(prompt), err)
				return
			}
			var reply map[string]any
			if e := json.Unmarshal([]byte(out), &reply); e != nil {
				t.Fatalf("%v: %.200s", e, out)
			}
			task := fmt.Sprint(reply["task"])
			ctx, _ := reply["task_context"].(map[string]any)
			t.Logf("request %d bytes: reply %d bytes (bound %d), task %d bytes, text_status=%v provided_bytes=%v receipt=%v conversation %d bytes, output=%.300s", len(prompt), len(out), workSourceBudget*3, len(task), ctx["text_status"], ctx["provided_bytes"], reply["receipt"] != nil, len(auditJSON(reply["conversation"])), auditJSON(reply["output"]))
			if len(out) > workSourceBudget*3+1 {
				t.Errorf("INVARIANT 4: the reply is %d bytes, over the largest bound %d", len(out), workSourceBudget*3)
			}
			if reply["receipt"] == nil {
				t.Errorf("INVARIANT 4: no receipt")
			}
			if !utf8.ValidString(task) {
				t.Errorf("the task was cut inside a character")
			}
			if fmt.Sprint(ctx["provided_bytes"]) != fmt.Sprint(len(task)) {
				t.Errorf("provided_bytes %v is not the length of the task returned (%d)", ctx["provided_bytes"], len(task))
			}
			if len(task) < len(strings.TrimSpace(prompt)) && ctx["text_status"] != "stored_request_truncated" {
				t.Errorf("the task is cut (%d of %d bytes) and text_status does not say so: %v", len(task), len(prompt), ctx["text_status"])
			}
		})
	}
}

// The same requests with max_bytes named: the reply never goes over the named bound, and a bound it cannot meet is refused.
func TestAuditGNamedBoundWithARequestOfSixByteCharacters(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	prompt := "Fix the table.\n" + strings.Repeat("<", 64<<10)
	notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	ids := auditIDs.FindString(notice)
	for _, bound := range []int{workMinBudget, 28 << 10, 64 << 10, 200000, workSourceBudget*3 - 1, workSourceBudget * 3} {
		var out string
		var err error
		withStdin(t, strings.TrimSuffix(ids, "}")+fmt.Sprintf(`,"query":"export page table","max_bytes":%d}`, bound), func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
		t.Logf("max_bytes %d: err=%v reply %d bytes", bound, err, len(out))
		if err == nil && len(out) > bound+1 {
			t.Errorf("max_bytes %d: the reply is %d bytes", bound, len(out))
		}
		if err != nil && strings.Contains(err.Error(), "individual planning") {
			t.Errorf("max_bytes %d: refused with the old flow: %v", bound, err)
		}
	}
}
