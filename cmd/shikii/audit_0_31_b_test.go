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

// Probes written by audit B (0.31.0). They print what the candidate does; the report quotes the output.

func auditQuiet(t *testing.T) {
	t.Helper()
	oldURL, oldLook := hookLocalURL, lookPath
	hookLocalURL = "http://127.0.0.1:1"
	lookPath = func(string) (string, error) { return "", errors.New("not on PATH (audit)") }
	t.Cleanup(func() { hookLocalURL, lookPath = oldURL, oldLook })
}

func TestAuditB_ReadOnlyCodexConfig(t *testing.T) {
	auditQuiet(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	own := "[mcp_servers.shikii]\ncommand = " + quotedList([]string{exe}) + "\n" + codexServerArgsLine + "\n"
	if err := os.WriteFile(path, []byte(own), 0o444); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return hookInstall([]string{"--yes"}, true) })
	after, _ := os.ReadFile(path)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "config.toml") || strings.Contains(line, "Codex will run") || strings.Contains(line, "without Codex asking") {
			t.Logf("OUTPUT: %s", line)
		}
	}
	t.Logf("file unchanged=%v; output says 'will run ... (adds': %v; output says it was added: %v; output says it was NOT added / read-only: %v",
		string(after) == own, strings.Contains(out, "Codex will run shikii's MCP tools without asking each time (adds"), strings.Contains(out, "now run without Codex asking"), strings.Contains(out, "read-only") || strings.Contains(out, "not add"))
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

func TestAuditB_InitPartialHookFailure(t *testing.T) {
	auditQuiet(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(gitRepo(t))
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(`{"hooks":{"Stop":"not a list"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdInit(nil) })
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Hooks:") || strings.Contains(line, "hooks added") || strings.HasPrefix(line, "Next:") || strings.HasPrefix(line, "Page:") {
			t.Logf("OUTPUT: %.420s", line)
		}
	}
	t.Logf("agents that now have a shikii hook: %v", agentsWithShikiiHook(home))
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

func TestAuditB_StandaloneCurrentEvidence(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Nothing.", "checked": "Read the export.", "evidence": []any{"current"}}})
	raw, _ := json.Marshal(saved)
	t.Logf("standalone receipt, evidence [current]: err=%v reply=%.900s", err, raw)
}

func TestAuditB_LegacyPlanCallsAndTheReceipt(t *testing.T) {
	for _, legacy := range []string{"query", "submit"} {
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
		_, perr := planningCall(t, legacy, payload)
		_, rerr := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Nothing.", "checked": "Read the export."}})
		t.Logf("work prepare, then hook plan %s (err=%v), then work record with the receipt: err=%v", legacy, perr, rerr)
	}
}

// The help's second form (no hook ids) and how_to_record both name "current" as a reference evidence takes.
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
	t.Logf("how_to_record says: %.260s", prepared["how_to_record"])
	receipt, _ := json.Marshal(prepared["receipt"])
	errorOf := func(saved map[string]any) string {
		raw, _ := json.Marshal(saved["errors"])
		return string(raw)
	}
	saved, err := cli(`{"receipt":`+string(receipt)+`,"result":{"changed":"Nothing.","checked":"Read the export.","evidence":["current"]}}`, "work", "record")
	t.Logf("1) evidence [current]: err=%v complete=%v errors=%s", err, saved["complete"], errorOf(saved))
	id := ""
	for _, raw := range saved["errors"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "result" {
			id, _ = item["id"].(string)
		}
	}
	for _, ref := range []string{"observed1", "result"} { // what refusal 1 lists as "usable here"
		saved, err = cli(`{"receipt":`+string(receipt)+`,"result":{"ref":"`+id+`","evidence":["`+ref+`"]}}`, "work", "record")
		t.Logf("2) following \"usable here\" with evidence [%s]: err=%v complete=%v errors=%s", ref, err, saved["complete"], errorOf(saved))
	}
}

// carried.next_action says: "cite or edit the pages without reading them again".
func TestAuditB_CarriedPagesCanBeEdited(t *testing.T) {
	for _, editedBefore := range []bool{false, true} {
		repo, root, _ := planningFixture(t)
		t.Chdir(repo)
		b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
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
		saved, err := workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
			"observations": []any{map[string]any{"kind": "test", "title": "Dates", "content": "The dates match."}},
			"notes":        []any{map[string]any{"target": ref, "sources": []any{"new1"}, "change_reason": "Add the dates.", "edits": []any{map[string]any{"field": "body", "before": now, "after": now + " Dates too."}}}}})
		raw, _ := json.Marshal(saved["errors"])
		t.Logf("the interrupted work edited the page: %v; carried=%s; editing %s with the new receipt: err=%v errors=%.700s", editedBefore, carried, ref, err, raw)
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
	for _, args := range [][]string{{"handoff", "delete", "audit/report.md"}, {"handoff", "read"}, {"handoff", "write", "audit/report.md", "more"}, {"handoff", "write", "--path", "audit/report.md"}} {
		_, err := run("", args...)
		first := ""
		if err != nil {
			first, _, _ = strings.Cut(err.Error(), "\n")
		}
		t.Logf("shikii %s -> first line of the refusal: %q", strings.Join(args, " "), first)
	}
	out, err := run("", "handoff", "write", "audit/report.md") // nothing piped: an agent's shell gives an empty standard input
	t.Logf("shikii handoff write audit/report.md with an empty standard input: out=%q err=%v", out, err)
	saved, _ := run("", "handoff", "read", "audit/report.md")
	t.Logf("the record now reads: %q", saved)
}

// The refusal of an unknown memory reference names where the prepared refs are in the reply of prepare.
func TestAuditB_WhereTheMemoryRefsAre(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "s1")
	memoryEffectComplete(t, st, scope, "s1")
	b := notebook.Book{Root: st.Dir, Scope: scope}
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "s1", "turn_id": loadSession(st.Dir, "s1").TurnID, "query": "README"})
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range prepared["pending"].([]any) {
		entry := raw.(map[string]any)
		var keys []string
		for key := range entry {
			keys = append(keys, key)
		}
		sortRefs(keys)
		memory, _ := json.Marshal(entry["memory"])
		t.Logf("pending[%d] has the keys %v; pending[%d].memories=%v; pending[%d].memory=%.160s", i, keys, i, entry["memories"], i, memory)
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "memory": []any{map[string]any{"ref": "knowledge1", "result": "helpful", "decision": "d", "evidence": "e"}}})
	raw, _ := json.Marshal(saved["errors"])
	t.Logf("record with memory ref knowledge1: err=%v errors=%s", err, raw)
}
