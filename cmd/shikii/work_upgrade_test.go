package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkUpgradeUsesExistingReceiptAndMaintenance(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, nil)
	loopPage(t, b, s.ID, map[string]any{"title": "Second upgrade rule"})
	loopPage(t, b, s.ID, map[string]any{"title": "Third upgrade rule"})
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	if !strings.Contains(out, "Upgrading memory") || !strings.Contains(feedbackContext(t, out), "memory-upgrade") {
		t.Fatal("an installed agent must automatically receive the upgrade work", out)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := v["memory_upgrade"].(map[string]any)
	if !ok || u["status"] != "upgrading" {
		t.Fatal("deliver the actual pinned upgrade articles", v)
	}
	pages := u["pages"].([]any)
	if len(pages) != 2 || !strings.Contains(u["next_action"].(string), "one batch per task") {
		t.Fatal("routine work must review one bounded batch and resume later", u["next_action"])
	}
	page := pages[0].(map[string]any)
	if page["article"] == nil || page["ref"] == "" || page["maintenance_ref"] == "" {
		t.Fatal(page)
	}
	maintenance := []any{}
	for _, raw := range pages {
		p := raw.(map[string]any)
		maintenance = append(maintenance, map[string]any{"ref": p["maintenance_ref"], "reason": "Checked original evidence; the rule remains applicable."})
		if p["task"].(map[string]any)["next_action"] == u["policy"] {
			t.Fatal("the shared policy must not be repeated in each task")
		}
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": maintenance, "result": map[string]any{"changed": "Reviewed the existing rules.", "evidence": []string{page["ref"].(string)}}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	if progress, err := b.MemoryUpgrade(); err != nil || progress.Reviewed != 2 || progress.Status != "upgrading" {
		t.Fatal("a completed batch must retain the unfinished upgrade", progress, err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the next routing condition."})
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u = next["memory_upgrade"].(map[string]any)
	if u["reviewed"] != float64(2) || len(u["pages"].([]any)) != 1 {
		t.Fatal("the next task must resume only the remaining page", u)
	}
	page = u["pages"].([]any)[0].(map[string]any)
	_, err = workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "maintenance": []any{map[string]any{"ref": page["maintenance_ref"], "reason": "Checked the remaining original evidence."}}})
	if err != nil {
		t.Fatal(err)
	}
	next, err = workCall(t, b, "prepare", map[string]any{"receipt": next["receipt"], "query": "memory-upgrade"})
	if err != nil || next["memory_upgrade"].(map[string]any)["status"] != "complete" {
		t.Fatal("resume and finish without another human form", next, err)
	}
}

// Asking for the upgrade batch with a bound smaller than its two pages returned nothing of it: memory_upgrade was left out whole,
// and the agent had to guess a larger max_bytes (2026-11-12: at 20000 bytes output named memory_upgrade among what was omitted,
// in answer to the query memory-upgrade). The batch now stays in a lighter form: each page keeps its reference, its maintenance
// reference, its task and the exact query that reads it, without its body, and output says so. With room the pages come whole.
func TestTheUpgradeBatchStaysWhenItsPagesDoNotFit(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"invoices", "exports"} {
		body := strings.Repeat("The long background of the "+name+" rule. ", 140)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{s.ID}, Title: "Upgrade rule for " + name, Lesson: "Keep the evidence of " + name + ".", AppliesTo: name, Kind: "rule", Scene: "when handling " + name, Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	const bound = 9000
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade", "max_bytes": bound})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	u, _ := v["memory_upgrade"].(map[string]any)
	pages, _ := u["pages"].([]any)
	if len(pages) != 2 || len(raw) > bound || strings.Contains(string(raw), "The long background of the") {
		t.Fatalf("the batch stays within %d bytes, both pages without their bodies: %d pages, %d bytes, output %v", bound, len(pages), len(raw), v["output"])
	}
	for _, entry := range pages {
		page := entry.(map[string]any)
		head, _ := page["article"].(map[string]any)
		query, _ := page["query"].(map[string]any)
		if page["ref"] == "" || page["maintenance_ref"] == "" || page["task"] == nil || page["body_included"] != false || head["title"] == "" || head["id"] == "" ||
			query["query"] != fmt.Sprintf("lesson:%v@%v", head["id"], head["revision"]) || !strings.Contains(fmt.Sprint(page["next_action"]), "Read the page") {
			t.Errorf("a page without its body keeps its references, its head, its task and the query that reads it: %v", page)
		}
	}
	if omitted := fmt.Sprint(v["output"].(map[string]any)["omitted"]); !strings.Contains(omitted, "memory_upgrade.pages[].article") || strings.Contains(omitted, "memory_upgrade ") || strings.HasSuffix(omitted, "memory_upgrade]") {
		t.Errorf("output names the bodies of the batch, not the whole batch: %s", omitted)
	}
	quotable := func() int { // the page texts the session keeps as delivered
		n := 0
		for key := range loadSession(root, "upgrade").Planning.Sources {
			if strings.HasPrefix(key, "article:") {
				n++
			}
		}
		return n
	}
	if n := quotable(); n != 0 {
		t.Errorf("a page whose body was left out is not kept as delivered text: %d kept", n)
	}
	whole, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "memory-upgrade", "max_bytes": 64000})
	if err != nil {
		t.Fatal(err)
	}
	if n := quotable(); n != 2 {
		t.Errorf("the pages that came whole are kept as delivered text: %d kept", n)
	}
	page := whole["memory_upgrade"].(map[string]any)["pages"].([]any)[0].(map[string]any)
	if body, _ := page["article"].(map[string]any)["body"].(string); page["body_included"] != true || !strings.Contains(body, "The long background of the") || page["ref"] != pages[0].(map[string]any)["ref"] {
		t.Errorf("with room the same page comes whole under the same reference: %v", page["ref"])
	}
}
