package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkGrowthFocusedPreparation(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	body := strings.Repeat("Earlier migration details do not govern the export.\n\n", 180) + "Quartz CSV exports must preserve tenant ownership; never include another tenant's rows.\n\n"
	p := reasoningPage(t, b, "Quartz CSV workflow", "howto", body)
	auditRound2SettleMaintenance(t, b)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz CSV tenant ownership"})
	if err != nil {
		t.Fatal(err)
	}
	entry := v["knowledge"].([]any)[0].(map[string]any)
	excerpts, ok := entry["body_excerpts"].([]any)
	if !ok || len(excerpts) == 0 {
		t.Fatal("omitted bodies need exact, focused passages before a full reread", entry)
	}
	bytes, relevant := 0, false
	for _, raw := range excerpts {
		e := raw.(map[string]any)
		start, end := int(e["start"].(float64)), int(e["end"].(float64))
		if p.Body[start:end] != e["text"] {
			t.Fatal("excerpt must be exact", e)
		}
		bytes += end - start
		relevant = relevant || strings.Contains(e["text"].(string), "tenant ownership")
	}
	if !relevant || bytes > 2400 || len(excerpts) > 3 || entry["body_included"] != false {
		t.Fatal(entry)
	}
	if entry["query"].(map[string]any)["query"] != "lesson:"+p.ID+"@"+p.Revision {
		t.Fatal(entry)
	}
}

func TestWorkGrowthRejectedDiscoveryHasNoSideEffects(t *testing.T) {
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Quartz workflow", "applies_to": "Quartz", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	bad, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "discoveries": []any{map[string]any{"ref": ref, "context": "Quartz", "decision": "", "evidence": "No observed change."}}})
	if err != nil || bad["complete"] != false {
		t.Fatal(bad, err)
	}
	token, err := workDecode(v["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, _, err := loadWorkState(dir, b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Expected) != 0 {
		t.Fatal("invalid content must not reserve durable capacity", w.Expected)
	}
}

func TestWorkGrowthOutcomeCheckpointRecoveryAfterRevisionChange(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz workflow", "applies_to": "Quartz", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	payload := map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Quartz check", "content": "The synthetic run completed."}}, "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Check Quartz", "result": "The check completed.", "sources": []string{"new1"}}}}
	old := workAfterWrite
	defer func() { workAfterWrite = old }()
	workAfterWrite = func(kind string) error {
		if kind == "outcome" {
			return errors.New("synthetic checkpoint failure")
		}
		return nil
	}
	first, err := workCall(t, b, "record", payload)
	if err != nil || first["complete"] != false {
		t.Fatal(first, err)
	}
	workAfterWrite = old
	loopPage(t, b, s, map[string]any{"id": p.ID, "title": p.Title, "lesson": "Check Quartz with updated conditions.", "applies_to": p.AppliesTo, "kind": "howto"})
	again, err := workCall(t, b, "record", payload)
	if err != nil || again["complete"] != true {
		t.Fatal("a recorded outcome must recover after its howto changes", again, err)
	}
	value, err := improvementCall(t, b, map[string]any{"operation": "reuse", "request": "Check Quartz", "conditions": "macOS"})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := loopObject(t, value)["steps"].([]any)[0].(map[string]any)["outcomes"].([]any)
	if len(outcomes) != 1 || outcomes[0].(map[string]any)["revision_changed"] != true {
		t.Fatal("the original run and revision must survive once", outcomes)
	}
}

func TestWorkGrowthContinuationAndDiscovery(t *testing.T) {
	b, s := loopBook(t)
	loopPage(t, b, s, map[string]any{"title": "Quartz exports", "lesson": "Preserve Quartz export filters.", "applies_to": "Quartz exports"})
	second := loopPage(t, b, s, map[string]any{"title": "Amber invoices", "lesson": "Check Amber invoice totals.", "applies_to": "Amber invoices"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Inspect Quartz exports then Amber invoices"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := v["receipt"]
	payload := map[string]any{"receipt": receipt, "observations": []any{map[string]any{"title": "First check", "content": "The Quartz filter was checked."}}}
	first, err := workCall(t, b, "record", payload)
	if err != nil || first["complete"] != true {
		t.Fatal(first, err)
	}
	more, err := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "query": "Amber invoices"})
	if err != nil {
		t.Fatal("mid-work knowledge must extend the same receipt", err)
	}
	if more["receipt"] != receipt {
		t.Fatal("continuation replaced the receipt", more)
	}
	var ref any
	for _, raw := range more["knowledge"].([]any) {
		e := raw.(map[string]any)
		if e["id"] == second.ID {
			ref = e["ref"]
		}
	}
	if ref == nil {
		t.Fatal(more)
	}
	discovery := map[string]any{"receipt": receipt, "discoveries": []any{map[string]any{"ref": ref, "context": "Zephyr billing reconciliation", "decision": "Applied the Amber totals check.", "evidence": "The synthetic totals stayed balanced."}}}
	saved, err := workCall(t, b, "record", discovery)
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	pages, err := b.Recall("Zephyr billing reconciliation")
	if err != nil || len(pages) != 1 || pages[0].ID != second.ID {
		t.Fatal("actual use must reach the existing discovery ranking", pages, err)
	}
	again, err := workCall(t, b, "record", payload)
	if err != nil || again["complete"] != true || mustJSONAuditRound2(first["saved"]) != mustJSONAuditRound2(again["saved"]) {
		t.Fatal("old saves must remain retryable", again, err)
	}
	other, _ := loopBook(t)
	if _, err = workCall(t, other, "prepare", map[string]any{"receipt": receipt, "query": "Amber invoices"}); err == nil {
		t.Fatal("a continuation belongs to its project")
	}
	separate, err := workCall(t, b, "prepare", map[string]any{"query": "Amber invoices", "request": "Reconcile a separate Amber billing run"})
	if err != nil {
		t.Fatal(err)
	}
	separateRef := separate["knowledge"].([]any)[0].(map[string]any)["ref"]
	separateUse, err := workCall(t, b, "record", map[string]any{"receipt": separate["receipt"], "discoveries": []any{map[string]any{"ref": separateRef, "context": "A separate Amber billing run", "decision": "Applied the Amber totals check to this run.", "evidence": "This run's synthetic totals stayed balanced."}}})
	if err != nil || separateUse["complete"] != true {
		t.Fatal("different work must not collide on the same article lookup", separateUse, err)
	}
	loopPage(t, b, s, map[string]any{"id": second.ID, "title": second.Title, "lesson": "Check Amber totals and dates.", "applies_to": second.AppliesTo})
	updated, err := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "query": "lesson:" + second.ID})
	if err != nil {
		t.Fatal(err)
	}
	newRef := updated["knowledge"].([]any)[0].(map[string]any)["ref"]
	if newRef == ref {
		t.Fatal("new revisions must not overwrite the earlier reference", updated)
	}
	discovery["discoveries"].([]any)[0].(map[string]any)["evidence"] = "Another execution with the old reference."
	stale, err := workCall(t, b, "record", discovery)
	if err != nil || stale["complete"] != false {
		t.Fatal("old references cannot create new discoveries after an edit", stale, err)
	}

	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	hooked := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, hooked, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain Amber invoices"})
	if _, err = workCall(t, hooked, "prepare", map[string]any{"receipt": prepared["receipt"], "query": "Amber invoices"}); err == nil {
		t.Fatal("the old turn cannot add knowledge to the new request")
	}
}

func TestWorkGrowthOutcomeReturnsConditionsAndEvidence(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz export checks", "lesson": "Check Quartz exports with tenant filters.", "applies_to": "Quartz exports", "kind": "howto"})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz exports", "request": "Validate Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := v["knowledge"].([]any)[0].(map[string]any)["ref"]
	payload := map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Synthetic export result", "content": "Tenant filters passed under an unprivileged user."}}, "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux, unprivileged user", "action": "Run the tenant-filter export check.", "failure": "Root ignored the expected permission denial.", "correction": "Repeat under an unprivileged user.", "result": "Tenant-filter and permission checks passed.", "sources": []string{"new1"}}}}
	saved, err := workCall(t, b, "record", payload)
	if err != nil || saved["complete"] != true {
		t.Fatal("checked execution must be tied to its procedure", saved, err)
	}
	again, err := workCall(t, b, "record", payload)
	if err != nil || again["complete"] != true {
		t.Fatal(again, err)
	}
	value, err := improvementCall(t, b, map[string]any{"operation": "reuse", "request": "Validate Quartz exports", "conditions": "macOS, ordinary user"})
	if err != nil {
		t.Fatal(err)
	}
	experience := loopObject(t, value)
	steps := experience["steps"].([]any)
	if len(steps) != 1 {
		t.Fatal(experience)
	}
	step := steps[0].(map[string]any)
	outcomes, ok := step["outcomes"].([]any)
	if !ok || len(outcomes) != 1 {
		t.Fatal("retry must not duplicate procedure outcomes", step)
	}
	o := outcomes[0].(map[string]any)
	if o["conditions"] != "Linux, unprivileged user" || o["correction"] != "Repeat under an unprivileged user." || len(o["evidence"].([]any)) == 0 {
		t.Fatal(o)
	}
	if o["revision"] != p.Revision || experience["conditions"] != "macOS, ordinary user" {
		t.Fatal("past and current conditions must stay separate", experience)
	}
	bad := map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": ref, "conditions": "Linux", "action": "Run checks", "result": "Passed", "sources": []string{"invented"}}}}
	refused, err := workCall(t, b, "record", bad)
	if err != nil || refused["complete"] != false {
		t.Fatal("outcomes require real prepared evidence", refused, err)
	}
	data, _ := json.Marshal(saved)
	if strings.Contains(string(data), `"truth"`) {
		t.Fatal("recorded execution must not create a truth", string(data))
	}
}

func TestWorkGrowthTransportAndBoundaries(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	reasoningPage(t, b, "Quartz export workflow", "howto", "Check tenant ownership for Quartz exports.")
	old := stdin
	defer func() { stdin = old }()
	stdin = strings.NewReader(`{"query":"Quartz exports","request":"Check Quartz exports"}`)
	raw := capture(t, func() error { return cmdWork([]string{"prepare"}) })
	var first map[string]any
	if err := json.Unmarshal([]byte(raw), &first); err != nil {
		t.Fatal(err)
	}
	receipt := first["receipt"]
	h := newServer(serveOpts{storeDir: root, projectScope: b.Scope})
	request, _ := json.Marshal(map[string]any{"operation": "prepare", "work": map[string]any{"receipt": receipt, "proposal": "Keep tenant ownership checks."}})
	code, body, err := callTool(h, "shikii_wiki", request)
	if err != nil || code != 200 || !strings.Contains(string(body), "experience") {
		t.Fatal(code, string(body), err)
	}
	ref := first["knowledge"].([]any)[0].(map[string]any)["ref"]
	payload := map[string]any{"operation": "record", "work": map[string]any{"receipt": receipt, "observations": []any{map[string]any{"kind": "agent", "title": "Reported result", "content": "Only an agent report is available."}}, "outcomes": []any{map[string]any{"ref": ref, "conditions": "macOS", "action": "Check Quartz export", "result": "Reported as complete", "sources": []string{"new1"}}}}}
	request, _ = json.Marshal(payload)
	r := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(request)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(strings.ReplaceAll(w.Body.String(), " ", ""), `"complete":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	stdin = strings.NewReader(string(mustOutcomePayload(t, map[string]any{"receipt": receipt, "conditions": "Linux"})))
	raw = capture(t, func() error { return cmdWork([]string{"prepare"}) })
	var more map[string]any
	if err = json.Unmarshal([]byte(raw), &more); err != nil {
		t.Fatal(err)
	}
	step := more["experience"].(map[string]any)["steps"].([]any)[0].(map[string]any)
	if len(step["outcomes"].([]any)[0].(map[string]any)["unknown"].([]any)) == 0 {
		t.Fatal("an agent report cannot establish success", step)
	}
	// Evidence integrity is checked again at reuse, independently of the index.
	o := step["outcomes"].([]any)[0].(map[string]any)
	sourceID := o["evidence"].([]any)[0].(map[string]any)["source_id"].(string)
	paths, err := filepath.Glob(filepath.Join(root, "records", "wiki-sources", "*", sourceID+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	original, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	if err = json.Unmarshal(original, &changed); err != nil {
		t.Fatal(err)
	}
	changed["content"] = "A different but structurally valid result."
	if err = os.WriteFile(paths[0], mustOutcomePayload(t, changed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = workCall(t, b, "prepare", map[string]any{"receipt": receipt, "conditions": "Linux"}); err == nil {
		t.Fatal("changed evidence cannot be reused")
	}
	props := workSchema["properties"].(map[string]any)
	for _, field := range []string{"proposal", "conditions", "cursor", "discoveries", "outcomes"} {
		if props[field] == nil {
			t.Fatal("MCP schema lacks content field", field)
		}
	}
}

func mustOutcomePayload(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
