package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Probes written by audit B (0.31.0). Several only printed what the candidate did (the report quoted the output) and failed on
// nothing; the second round of audits named them (D, U-2). Each test here now fails when what it shows is broken. The three that
// showed what a test of the second round checks are gone: the read-only config.toml (TestAuditDGap_ReadOnlyCodexConfig), the hooks
// installed half-way (TestAuditDGap_InitSaysWhoGotTheHooks) and the field the memory refusal names
// (TestAuditDGap_TheRefusalNamesTheFieldTheReplyHas).

func auditQuiet(t *testing.T) {
	t.Helper()
	oldURL, oldLook := hookLocalURL, lookPath
	hookLocalURL = "http://127.0.0.1:1"
	lookPath = func(string) (string, error) { return "", errors.New("not on PATH (audit)") }
	t.Cleanup(func() { hookLocalURL, lookPath = oldURL, oldLook })
}

func TestAuditB_UninstallRemovesLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "model = \"x\"\n\n[mcp_servers.shikii]\ncommand = \"/opt/shikii\"\n" + codexServerArgsLine + "\n" + codexToolsAllowedLine + "\n\n[mcp_servers.other]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := removeCodexServer(path, true)
	after, _ := os.ReadFile(path)
	t.Logf("found=%v err=%v\n--- after\n%s", found, err, after)
	if strings.Contains(string(after), "default_tools_approval_mode") || strings.Contains(string(after), "shikii") || !strings.Contains(string(after), "[mcp_servers.other]") || !strings.Contains(string(after), "model = \"x\"") {
		t.Errorf("uninstall must cut the block with its approval line and keep the rest")
	}
}

func TestAuditB_MovedOnlyProjectGetsHooks(t *testing.T) {
	auditQuiet(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	parent := t.TempDir()
	first := filepath.Join(parent, "project")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(first)
	capture(t, func() error { return cmdInit(nil) })
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	agent, _ := agentHome()
	t.Logf("after init + hook uninstall: registered projects=%d, agents with a shikii hook=%v", len(registeredProjects(agent)), agentsWithShikiiHook(home))

	// the person renames the folder of their only project and runs init there (the documented way to reconnect it)
	moved := filepath.Join(parent, "project-renamed")
	t.Chdir(parent)
	if err := os.Rename(first, moved); err != nil {
		t.Fatal(err)
	}
	t.Chdir(moved)
	out := capture(t, func() error { return cmdInit(nil) })
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Hooks:") || strings.Contains(line, "hooks added") || strings.Contains(line, "Reconnected") {
			t.Logf("OUTPUT: %.300s", line)
		}
	}
	t.Logf("after init in the renamed folder: agents with a shikii hook=%v", agentsWithShikiiHook(home))
	if got := agentsWithShikiiHook(home); len(got) != 0 {
		t.Errorf("INVARIANT 6: a machine that already had a project and no hooks got the hooks from init: %v", got)
	}
}

func TestAuditB_DeletedStoreGetsHooksAgain(t *testing.T) {
	auditQuiet(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "init does not add them again") {
		t.Fatalf("precondition: the first init installs the hooks and says init does not add them again:\n%s", out)
	}
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if err := os.RemoveAll(filepath.Join(project, ".shikii")); err != nil { // the person starts this project over
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdInit(nil) })
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Hooks:") || strings.Contains(line, "hooks added") {
			t.Logf("OUTPUT: %.300s", line)
		}
	}
	if got := agentsWithShikiiHook(home); len(got) != 0 {
		t.Errorf("the first init said \"init does not add them again\", and init added them again: %v", got)
	}
}

func TestAuditB_StopBlockShortMessageAsksTwoFlows(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OK\u3067\u3059") // a short message outside the acknowledgement list, after a finished request
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	reason, block := stopReason(t, out)
	t.Logf("blocked=%v\n%s", block, reason)
	t.Logf("asks hook plan submit: %v; asks work prepare: %v; asks work record: %v", strings.Contains(reason, "hook plan submit"), strings.Contains(reason, " work prepare"), strings.Contains(reason, "work record"))
	if block && strings.Contains(reason, "hook plan submit") && strings.Contains(reason, " work prepare") {
		t.Errorf("INVARIANT 7: the Stop block asks for hook plan submit and for work prepare -> work record in the same block")
	}
}

// A receipt without hook ids has no current request (INVARIANT 10): evidence [current] is refused with what to send instead, and
// the report is kept, waiting for its evidence under the id the refusal names. With the ids of a hook's request the same result
// is complete (the control: current is refused only where there is no request to cite).
func TestAuditB_StandaloneCurrentEvidence(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	result := func() map[string]any {
		return map[string]any{"changed": "Nothing.", "checked": "Read the export.", "evidence": []any{"current"}}
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": result()})
	raw, _ := json.Marshal(saved)
	t.Logf("standalone receipt, evidence [current]: err=%v reply=%.900s", err, raw)
	refusal, id := "", ""
	items, _ := saved["errors"].([]any)
	for _, item := range items {
		if entry, _ := item.(map[string]any); entry["kind"] == "result" {
			refusal, _ = entry["error"].(string)
			id, _ = entry["id"].(string)
		}
	}
	if err != nil || saved["complete"] == true || !strings.Contains(refusal, `unknown source reference "current"`) || !strings.Contains(refusal, "new1") {
		t.Errorf("a receipt without hook ids that cites current is refused with what to cite instead: err=%v complete=%v %q", err, saved["complete"], refusal)
	}
	storage, _ := saved["storage"].(map[string]any)
	if id == "" || storage["status"] != "saved-pending" || storage["result_source"] != id {
		t.Errorf("the report is kept and waits for its evidence under the id the refusal names (%q): storage=%v", id, storage)
	}
	// control: the ids of a hook's request
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	hooked, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV export filters"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err = workCall(t, b, "record", map[string]any{"receipt": hooked["receipt"], "result": result()})
	if raw, _ = json.Marshal(saved["errors"]); err != nil || saved["complete"] != true {
		t.Errorf("control: with the ids of a hook's request, current is evidence and the result is complete: err=%v complete=%v errors=%s", err, saved["complete"], raw)
	}
}

// What the notice says of the legacy calls ("Do not run the legacy hook plan query: it replaces the receipt"): hook plan query
// replaces the receipt of work prepare, and the record then says what replaced it and what to use; hook plan submit leaves the
// receipt as it is, and the record with it is taken.
func TestAuditB_LegacyPlanCallsAndTheReceipt(t *testing.T) {
	for _, legacy := range []string{"query", "submit"} {
		t.Run(legacy, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			t.Chdir(repo)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			turn := loadSession(root, "planning").TurnID
			prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
			if err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV columns"}
			if legacy == "submit" {
				payload = planningPayload(root)
			}
			if _, perr := planningCall(t, legacy, payload); perr != nil {
				t.Fatalf("precondition: hook plan %s is taken: %v", legacy, perr)
			}
			saved, rerr := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Nothing.", "checked": "Read the export."}})
			t.Logf("work prepare, then hook plan %s, then work record with the receipt: err=%v", legacy, rerr)
			if legacy == "query" {
				if rerr == nil {
					t.Fatalf("hook plan query replaced what the receipt prepared, and the record with that receipt is taken: %v", saved)
				}
				for _, w := range []string{"the work receipt is expired or no longer current", "hook plan query", "replaced it", "prepare again"} {
					if !strings.Contains(rerr.Error(), w) {
						t.Errorf("the refusal of the replaced receipt lacks %q: %v", w, rerr)
					}
				}
				return
			}
			if rerr != nil || saved["complete"] != true {
				t.Errorf("hook plan submit does not replace the receipt, and the record with it is not taken: err=%v %v", rerr, saved)
			}
		})
	}
}

// The help's second form (no hook ids), followed through the CLI as its replies say (INVARIANT 10). how_to_record and the
// refusals offered current and the report's own names, and each of those was refused in turn (B-I1). Now nothing they offer is
// refused: the refusal of current asks for an observation of the call, and sending that completes the report.
func TestAuditB_StandaloneFollowsTheRefusals(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	cli := func(input string, args ...string) (map[string]any, error) {
		t.Helper()
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cliCall(args) }) })
		var reply map[string]any
		_ = json.Unmarshal([]byte(out), &reply)
		return reply, err
	}
	prepared, err := cli(`{"query":"CSV export filters","request":"Check the CSV export"}`, "work", "prepare") // the help's form, filled in
	if err != nil {
		t.Fatal(err)
	}
	how, _ := prepared["how_to_record"].(string)
	t.Logf("how_to_record says: %.260s", how)
	if strings.Contains(how, "(current,") || !strings.Contains(how, "newN for observation N") {
		t.Errorf("the reply of a preparation without hook ids offers current as evidence, or does not say what evidence takes: %s", how)
	}
	receipt, _ := json.Marshal(prepared["receipt"])
	errorOf := func(saved map[string]any) string {
		raw, _ := json.Marshal(saved["errors"])
		return string(raw)
	}
	saved, err := cli(`{"receipt":`+string(receipt)+`,"result":{"changed":"Nothing.","checked":"Read the export.","evidence":["current"]}}`, "work", "record")
	t.Logf("1) evidence [current]: err=%v complete=%v errors=%s", err, saved["complete"], errorOf(saved))
	id, refusal := "", ""
	items, _ := saved["errors"].([]any)
	for _, raw := range items {
		if item, _ := raw.(map[string]any); item["kind"] == "result" {
			id, _ = item["id"].(string)
			refusal, _ = item["error"].(string)
		}
	}
	if err != nil || id == "" || saved["complete"] == true || !strings.Contains(refusal, `unknown source reference "current"`) || !strings.Contains(refusal, "new1") {
		t.Fatalf("1) current is refused with what to send, and the report waits under its id: err=%v id=%q %q", err, id, refusal)
	}
	// nothing the refusal offers is the report itself (it listed observed1 and result as "usable here")
	if strings.Contains(refusal, "observed1") || strings.Contains(refusal, "usable here") {
		t.Errorf("1) the refusal offers the report's own names as evidence: %q", refusal)
	}
	for _, ref := range []string{"observed1", "result"} { // the report's own names: refused, without sending the agent back to current
		saved, err = cli(`{"receipt":`+string(receipt)+`,"result":{"ref":"`+id+`","evidence":["`+ref+`"]}}`, "work", "record")
		text := errorOf(saved)
		t.Logf("2) evidence [%s], the report itself: err=%v complete=%v errors=%s", ref, err, saved["complete"], text)
		if err != nil || saved["complete"] == true || !strings.Contains(text, "is this result itself") || strings.Contains(text, "current") {
			t.Errorf("2) the report's own name %s is refused as evidence, and the refusal does not offer current: err=%v complete=%v %s", ref, err, saved["complete"], text)
		}
	}
	// 3) what refusal 1 asks for: an observation of this call, cited as new1
	saved, err = cli(`{"receipt":`+string(receipt)+`,"observations":[{"kind":"test","title":"Export","content":"The export keeps its filters."}],"result":{"ref":"`+id+`","evidence":["new1"]}}`, "work", "record")
	t.Logf("3) an observation of the call, cited as new1: err=%v complete=%v errors=%s", err, saved["complete"], errorOf(saved))
	if err != nil || saved["complete"] != true {
		t.Errorf("3) following the refusal (an observation of this call, cited as new1) completes the report: err=%v complete=%v errors=%s", err, saved["complete"], errorOf(saved))
	}
}

// carried.next_action said: "cite or edit the pages without reading them again", and a page the interrupted work had edited was
// then refused (B-A5). It now says which pages are edited as they are and how the others are read again. Followed as written:
// a page that work did not edit is edited under the reference taken over; a page it edited is refused under that reference,
// read again with the receipt (query lesson:<id>), and edited under the reference the reading gives.
func TestAuditB_CarriedPagesCanBeEdited(t *testing.T) {
	for _, editedBefore := range []bool{false, true} {
		repo, root, _ := planningFixture(t)
		t.Chdir(repo)
		b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		body := "The export keeps the filters."
		page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
		if err != nil {
			t.Fatal(err)
		}
		ask := func(prompt string) string {
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt})
			return loadSession(root, "planning").TurnID
		}
		refOf := func(prepared map[string]any) string {
			for _, raw := range prepared["knowledge"].([]any) {
				if entry := raw.(map[string]any); entry["id"] == page.Page.ID {
					return entry["ref"].(string)
				}
			}
			return ""
		}
		first, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": ask(planningTestRequest), "query": "CSV export filters"})
		if err != nil {
			t.Fatal(err)
		}
		ref := refOf(first)
		now := "The export keeps the filters."
		if editedBefore {
			saved, err := workCall(t, b, "record", map[string]any{"receipt": first["receipt"],
				"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}},
				"notes":        []any{map[string]any{"target": ref, "sources": []any{"new1"}, "change_reason": "Add the totals.", "edits": []any{map[string]any{"field": "body", "before": now, "after": "The export keeps the filters and the totals."}}}}})
			if raw, _ := json.Marshal(saved["errors"]); err != nil || string(raw) != "null" {
				t.Fatalf("the first edit: %v %s", err, raw)
			}
			now = "The export keeps the filters and the totals."
		}
		second, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": ask("Also check the dates of the CSV export."), "query": "invoice numbering"})
		if err != nil {
			t.Fatal(err)
		}
		carried, _ := json.Marshal(second["carried"])
		edit := func(target string) (map[string]any, error) {
			return workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
				"observations": []any{map[string]any{"kind": "test", "title": "Dates", "content": "The dates match."}},
				"notes":        []any{map[string]any{"target": target, "sources": []any{"new1"}, "change_reason": "Add the dates.", "edits": []any{map[string]any{"field": "body", "before": now, "after": now + " Dates too."}}}}})
		}
		reached := func() bool {
			live, err := b.LiveWhere(func(p notebook.Page) bool { return p.ID == page.Page.ID })
			return err == nil && len(live) == 1 && live[0].Body == now+" Dates too."
		}
		saved, err := edit(ref)
		raw, _ := json.Marshal(saved["errors"])
		t.Logf("the interrupted work edited the page: %v; carried=%s; editing %s with the new receipt: err=%v errors=%.700s", editedBefore, carried, ref, err, raw)
		articles, _ := second["carried"].(map[string]any)["articles"].([]any)
		next, _ := second["carried"].(map[string]any)["next_action"].(string)
		if len(articles) != 1 || articles[0] != ref || !strings.Contains(next, "edit the pages that work did not edit") || !strings.Contains(next, "prepare with this receipt and query lesson:") {
			t.Errorf("edited before=%v: the page is taken over under %s, and carried says which pages are edited as they are and how the others are read again: %s", editedBefore, ref, carried)
		}
		if !editedBefore {
			if err != nil || string(raw) != "null" || !reached() {
				t.Errorf("a page the interrupted work did not edit is edited under the reference taken over (%s): err=%v errors=%s reached=%v", ref, err, raw, reached())
			}
			continue
		}
		if err != nil || !strings.Contains(string(raw), "read it again before saving") || reached() {
			t.Errorf("a page the interrupted work edited is refused under the reference taken over, with what to do: err=%v errors=%s reached=%v", err, raw, reached())
		}
		// as carried says: read the page again with this receipt, and edit it under the reference that reading gives
		again, err := workCall(t, b, "prepare", map[string]any{"receipt": second["receipt"], "query": "lesson:" + page.Page.ID})
		if err != nil {
			t.Fatalf("reading the page again with the receipt (query lesson:%s): %v", page.Page.ID, err)
		}
		fresh := refOf(again)
		if fresh == "" || fresh == ref {
			t.Fatalf("reading the page again gives a reference of its own for the revision it has now: %q (taken over: %s)", fresh, ref)
		}
		saved, err = edit(fresh)
		if raw, _ = json.Marshal(saved["errors"]); err != nil || string(raw) != "null" || !reached() {
			t.Errorf("the page read again is edited under its new reference (%s): err=%v errors=%s reached=%v", fresh, err, raw, reached())
		}
	}
}

func TestAuditB_HandoffCallsThatCannotBeRead(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	run := func(input string, args ...string) (string, error) {
		var out string
		var err error
		withStdin(t, input, func() { out, err = captureErr(func() error { return cliCall(args) }) })
		return out, err
	}
	if _, err := run("The report, three hours of work.\n", "handoff", "write", "audit/report.md"); err != nil {
		t.Fatal(err)
	}
	// a call that cannot be read (INVARIANT 2): the first line of the refusal says what is wrong with it, and the forms follow.
	// Before, the first line was the usage line for all four.
	for _, c := range []struct {
		args []string
		says string
	}{
		{[]string{"handoff", "delete", "audit/report.md"}, `"delete"`},
		{[]string{"handoff", "read"}, "the path"},
		{[]string{"handoff", "write", "audit/report.md", "more"}, "one path"},
		{[]string{"handoff", "write", "--path", "audit/report.md"}, "no flags"},
	} {
		_, err := run("", c.args...)
		first, rest := "", ""
		if err != nil {
			first, rest, _ = strings.Cut(err.Error(), "\n")
		}
		t.Logf("shikii %s -> first line of the refusal: %q", strings.Join(c.args, " "), first)
		if err == nil || strings.HasPrefix(first, "usage:") || !strings.Contains(first, "handoff") || !strings.Contains(first, c.says) || !strings.Contains(rest, "usage: shikii handoff") {
			t.Errorf("shikii %s: the first line of the refusal says what is wrong with the call (%s), and the forms follow: %v", strings.Join(c.args, " "), c.says, err)
		}
	}
	// nothing piped: an agent's shell gives an empty standard input. It replaced the record with 0 bytes (B-I2).
	out, err := run("", "handoff", "write", "audit/report.md")
	t.Logf("shikii handoff write audit/report.md with an empty standard input: out=%q err=%v", out, err)
	if err == nil || out != "" || !strings.Contains(err.Error(), "standard input was empty") {
		t.Errorf("handoff write with an empty standard input is refused, and says why: out=%q err=%v", out, err)
	}
	saved, err := run("", "handoff", "read", "audit/report.md")
	if err != nil || saved != "The report, three hours of work.\n" {
		t.Errorf("the record is as it was after the refused write: %q %v", saved, err)
	}
}
