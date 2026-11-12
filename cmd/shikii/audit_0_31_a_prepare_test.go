package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

var auditIDs = regexp.MustCompile(`\{"session_id":"[^"]*","turn_id":"[0-9a-f]{16}"\}`)

// audit A: the CLI's prepare in a project with many pending conversations, with the ids of the hook notice and without them.
func TestAuditACLIPrepareWithManyPendingConversations(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	for i := 0; i < 42; i++ {
		session := fmt.Sprintf("s%d", i)
		memoryEffectPrompt(t, scope, session)
		memoryEffectComplete(t, st, scope, session)
	}
	notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "main", "cwd": scope, "prompt": "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"}))
	ids := auditIDs.FindString(notice)
	if ids == "" {
		t.Fatalf("the notice carries the ids: %s", notice)
	}
	for name, input := range map[string]string{
		"with the ids of the notice": strings.TrimSuffix(ids, "}") + `,"query":"README ` + "\u5c0e\u5165\u624b\u9806" + `"}`,
		"without ids":                `{"query":"README ` + "\u5c0e\u5165\u624b\u9806" + `","request":"README ` + "\u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3059\u308b" + `"}`,
	} {
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
		if err != nil {
			t.Errorf("%s: the CLI's prepare is refused: %v", name, err)
			continue
		}
		var reply map[string]any
		if e := json.Unmarshal([]byte(out), &reply); e != nil {
			t.Fatalf("%s: %v: %.200s", name, e, out)
		}
		t.Logf("%s: %d bytes, output=%v", name, len(out), reply["output"])
		if len(out) > 28<<10 {
			t.Errorf("%s: the reply is %d bytes, over 28672", name, len(out))
		}
		for _, key := range []string{"receipt", "task", "how_to_record"} {
			if reply[key] == nil {
				t.Errorf("%s: the reply lacks %s", name, key)
			}
		}
	}
}

// audit A: the person's message is long (a pasted log or specification). The notice asks for work prepare on the CLI with no max_bytes.
func TestAuditACLIPrepareAfterALongRequest(t *testing.T) {
	for _, size := range []int{20 << 10, 27 << 10, 30 << 10, 50 << 10} {
		t.Run(fmt.Sprintf("a request of %d bytes", size), func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			t.Chdir(repo)
			prompt := "Use the agreed CSV columns and keep the existing filters. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", size/56)
			notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
			ids := auditIDs.FindString(notice)
			if ids == "" {
				t.Fatalf("the notice carries the ids: %.300s", notice)
			}
			var out string
			var err error
			withStdin(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export filters"}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
			if err != nil {
				t.Errorf("the call the notice asks for is refused, and the agent holds no receipt: %v", err)
				return
			}
			var reply map[string]any
			if e := json.Unmarshal([]byte(out), &reply); e != nil {
				t.Fatalf("%v: %.200s", e, out)
			}
			t.Logf("%d bytes, receipt=%v, task=%d bytes, output=%v", len(out), reply["receipt"] != nil, len(fmt.Sprint(reply["task"])), reply["output"])
			// A task is never cut, so a request longer than the CLI's own default cannot come within it: the default widens to the
			// bound of the other entries (64 KiB), the task comes whole, and the receipt is there.
			if task := fmt.Sprint(reply["task"]); reply["receipt"] == nil || len(task) < size || len(out) > 64<<10 {
				t.Errorf("the reply holds the receipt and the whole request within 64 KiB: %d bytes, task %d of %d bytes", len(out), len(task), size)
			}
		})
	}
}

// audit A: what the reply says about further pages once the candidates were cut.
func TestAuditACandidateCutAndTheContextsOwnCursor(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", 12)
		applies := fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 12)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("CSV export filters %d", i), Lesson: "Keep the filters when exporting CSV.", AppliesTo: applies, Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	var out string
	withStdin(t, `{"query":"CSV export filters","request":"Check the CSV export","max_bytes":12000}`, func() { out = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
	var reply map[string]any
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatal(err)
	}
	context := reply["knowledge_context"].(map[string]any)
	search := context["search"].(map[string]any)
	query := reply["knowledge_query"].(map[string]any)
	t.Logf("candidates shown=%d total=%v context.next_cursor=%v query.cursor set=%v", len(search["candidates"].([]any)), search["total"], context["next_cursor"], query["cursor"] != nil)
	t.Logf("context.next_action=%v", context["next_action"])
	t.Logf("output=%v", reply["output"])
	if query["cursor"] != nil && context["next_cursor"] == nil && strings.Contains(fmt.Sprint(context["next_action"]), "when next_cursor is present") {
		t.Errorf("the candidates were cut and knowledge_query continues, yet knowledge_context has no next_cursor while its next_action says to read on only when next_cursor is present")
	}
}

// audit A: a sweep of bounds over a store with lessons, pending conversations and deliveries: every reply is within its bound,
// keeps receipt, task and how_to_record, and names what it left out.
func TestAuditAPrepareBoundSweep(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	for i := 0; i < 30; i++ {
		body := fmt.Sprintf("README step %d. ", i) + strings.Repeat("Check the setup steps of the README on a machine that has no project yet. ", 6+i*3)
		applies := fmt.Sprintf("README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044 %d ", i) + strings.Repeat("when the README setup steps change ", 2+i%7)
		if _, err := b.Ingest(notebook.Draft{Sources: draft.Sources, Title: fmt.Sprintf("README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3059\u308b %d", i), Lesson: "Check the README setup steps on a fresh machine.", AppliesTo: applies, Kind: "rule", Scene: "\u30c6\u30b9\u30c8\u306e\u6642", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		session := fmt.Sprintf("s%d", i)
		memoryEffectPrompt(t, scope, session)
		memoryEffectComplete(t, st, scope, session)
	}
	notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "main", "cwd": scope, "prompt": "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"}))
	ids := auditIDs.FindString(notice)
	if ids == "" {
		t.Fatalf("the notice carries the ids: %.300s", notice)
	}
	refused, limited, whole := 0, 0, 0
	check := func(name, input string, bound int) {
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
		if err != nil {
			refused++
			if !strings.Contains(err.Error(), "max_bytes") {
				t.Errorf("%s at %d: refused without naming the bound: %v", name, bound, err)
			}
			return
		}
		var reply map[string]any
		if e := json.Unmarshal([]byte(out), &reply); e != nil {
			t.Fatalf("%s at %d: %v: %.200s", name, bound, e, out)
		}
		if len(out) > bound {
			t.Errorf("%s at %d: the reply is %d bytes", name, bound, len(out))
		}
		for _, key := range []string{"receipt", "task", "how_to_record"} {
			if reply[key] == nil || reply[key] == "" {
				t.Errorf("%s at %d: the reply lacks %s", name, bound, key)
			}
		}
		if output, _ := reply["output"].(map[string]any); output != nil {
			limited++
			if output["limited"] != true || fmt.Sprint(output["max_bytes"]) != fmt.Sprint(bound) {
				t.Errorf("%s at %d: output: %v", name, bound, output)
			}
			for _, raw := range reply["knowledge"].([]any) {
				entry := raw.(map[string]any)
				if entry["body_included"] == false && entry["title"] == nil && entry["text"] == nil {
					// down to its references: allowed only when the title did not fit either; count it
					t.Logf("%s at %d: a lesson without title (%d bytes total)", name, bound, len(out))
				}
			}
		} else {
			whole++
		}
	}
	for bound := 8192; bound <= 70000; bound += 617 {
		check("standalone", fmt.Sprintf(`{"query":"README `+"\u5c0e\u5165\u624b\u9806"+`","request":"README `+"\u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3059\u308b"+`","max_bytes":%d}`, bound), bound)
		check("with ids", strings.TrimSuffix(ids, "}")+fmt.Sprintf(`,"query":"README `+"\u5c0e\u5165\u624b\u9806"+`","max_bytes":%d}`, bound), bound)
	}
	check("standalone default", `{"query":"README `+"\u5c0e\u5165\u624b\u9806"+`","request":"README `+"\u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3059\u308b"+`"}`, 28<<10)
	check("with ids default", strings.TrimSuffix(ids, "}")+`,"query":"README `+"\u5c0e\u5165\u624b\u9806"+`"}`, 28<<10)
	t.Logf("refused=%d limited=%d whole=%d", refused, limited, whole)
}

// audit A: lessons reduced to their titles or references: what output says about it.
func TestAuditAOutputNamesTheLessonsTextLeftOut(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The ledger totals match.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		long := fmt.Sprintf("Ledger totals %d: ", i) + strings.Repeat("compare the ledger totals with the bank statement before closing the month. ", 20)
		body := "Checked."
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("Ledger totals %d", i), Lesson: long, AppliesTo: long[:400], Kind: "rule", Scene: "when closing the month", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	var out string
	withStdin(t, `{"query":"ledger totals bank statement","request":"Close the month","max_bytes":8192}`, func() { out = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
	var reply map[string]any
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatal(err)
	}
	output := reply["output"].(map[string]any)
	reduced := 0
	for _, raw := range reply["knowledge"].([]any) {
		if entry := raw.(map[string]any); entry["summary"] == nil && entry["article"] == nil {
			reduced++
		}
	}
	t.Logf("%d bytes; %d lessons without summary and body; omitted=%v", len(out), reduced, output["omitted"])
	named := false
	for _, raw := range output["omitted"].([]any) {
		if item := fmt.Sprint(raw); item == "knowledge" || strings.HasPrefix(item, "knowledge[") || strings.HasPrefix(item, "knowledge ") {
			named = true
		}
	}
	if reduced > 0 && !named {
		t.Errorf("%d lessons lost their summary, applies_to and body, and output.omitted does not name them: %v", reduced, output["omitted"])
	}
	if len(out) < 8192*6/10 {
		t.Errorf("the reply uses %d of 8192 bytes, and the lessons are down to their titles", len(out))
	}
}

// audit A: the same long request with the bound the CLI had before (65536): the control of TestAuditACLIPrepareAfterALongRequest.
func TestAuditACLIPrepareAfterALongRequestWithTheFormerBound(t *testing.T) {
	for _, size := range []int{27 << 10, 30 << 10, 50 << 10} {
		repo, _, _ := planningFixture(t)
		t.Chdir(repo)
		prompt := "Use the agreed CSV columns and keep the existing filters. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", size/56)
		notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
		ids := auditIDs.FindString(notice)
		var out string
		var err error
		withStdin(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export filters","max_bytes":65536}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
		if err != nil || !strings.Contains(out, `"receipt"`) {
			t.Errorf("a request of %d bytes with max_bytes 65536: %v", size, err)
		} else {
			t.Logf("a request of %d bytes with max_bytes 65536: %d bytes, with a receipt", size, len(out))
		}
	}
}
