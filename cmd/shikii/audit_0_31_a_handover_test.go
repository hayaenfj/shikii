package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

type auditFlow struct {
	t    *testing.T
	repo string
	root string
	b    notebook.Book
}

func newAuditFlow(t *testing.T) *auditFlow {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	return &auditFlow{t: t, repo: repo, root: root, b: notebook.Book{Root: root, Scope: feedbackScope(repo)}}
}

// ask sends the person's message and returns the ids of its notice as a map.
func (f *auditFlow) ask(prompt string) map[string]any {
	f.t.Helper()
	notice := feedbackContext(f.t, runUnreviewedHook(f.t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": prompt}))
	for _, line := range strings.Split(notice, "\n") {
		if strings.HasPrefix(line, `{"session_id":`) {
			var ids map[string]any
			if err := json.Unmarshal([]byte(line), &ids); err != nil {
				f.t.Fatal(err)
			}
			return ids
		}
	}
	f.t.Fatalf("the notice carries the ids: %s", notice)
	return nil
}

func (f *auditFlow) prepare(ids map[string]any, query string) map[string]any {
	f.t.Helper()
	fields := map[string]any{"query": query}
	for k, v := range ids {
		fields[k] = v
	}
	reply, err := workCall(f.t, f.b, "prepare", fields)
	if err != nil {
		f.t.Fatalf("prepare: %v", err)
	}
	return reply
}

func (f *auditFlow) record(prepared map[string]any, fields map[string]any) (map[string]any, error) {
	f.t.Helper()
	fields["receipt"] = prepared["receipt"]
	return workCall(f.t, f.b, "record", fields)
}

func auditJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }

// audit A: the report of the interrupted request goes over as observedN, and the next request's result cites it.
func TestAuditAHandoverCarriesTheInterruptedResultAsEvidence(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	saved, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Edited the export."}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first record: %.600s", auditJSON(saved))
	// the person sends a message before the agent's turn ended
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	t.Logf("carried: %s", auditJSON(second["carried"]))
	if second["carried"] != nil && strings.Contains(auditJSON(second["carried"]), "observed") {
		t.Errorf("the result report of the interrupted request was handed over as a saved source: %s", auditJSON(second["carried"]))
	}
	saved, err = f.record(second, map[string]any{"result": map[string]any{"changed": "Checked the totals.", "evidence": []any{"observed1"}}})
	t.Logf("second record: %.900s %v", auditJSON(saved), err)
	if err == nil && saved["complete"] == true {
		t.Errorf("a result with nothing observed cites the interrupted request's report (observed1) and is complete: %s", fmt.Sprint(saved["storage"]))
	}
}

// audit A: a Stop whose input has no last message (a turn that ended on a tool call, or a host that sends none) still is a Stop.
func TestAuditAHandoverAfterAStopWithoutALastMessage(t *testing.T) {
	for _, last := range []string{"", "   "} {
		t.Run(fmt.Sprintf("last=%q", last), func(t *testing.T) {
			f := newAuditFlow(t)
			first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
			saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}, "result": map[string]any{"changed": "Edited the export.", "checked": "Ran it.", "evidence": []any{"new1"}}})
			if err != nil || saved["complete"] != true {
				t.Fatalf("the first work is recorded: %v %.400s", err, auditJSON(saved))
			}
			out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": last})
			reason, blocked := stopReason(t, out)
			t.Logf("stop: blocked=%v %.200s", blocked, reason)
			if blocked {
				t.Fatalf("the Stop passes (the work was recorded): %s", reason)
			}
			second := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
			if second["carried"] != nil {
				t.Errorf("the Stop of the first request came, yet the next preparation took its references over: %s", auditJSON(second["carried"]))
			}
		})
	}
}

// audit A: the routine check stops the Stop, the agent goes on working (it is asked to record), and the person sends a message.
func TestAuditANoHandoverAfterABlockedStop(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}}})
	if err != nil {
		t.Fatalf("the observation is saved: %v", err)
	}
	t.Logf("saved: %.300s", auditJSON(saved["saved"]))
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	reason, blocked := stopReason(t, out)
	t.Logf("stop: blocked=%v %.300s", blocked, reason)
	if !blocked {
		t.Skip("the Stop was not stopped in this fixture")
	}
	// the agent is still working (the block told it to record); the person sends a message
	second := f.prepare(f.ask("Also check the totals of the CSV export."), "CSV export totals")
	if second["carried"] == nil {
		t.Errorf("the agent was still working after a stopped Stop, and the next preparation took nothing over")
	} else {
		t.Logf("carried: %s", auditJSON(second["carried"]))
	}
}

// audit A: what the new receipt can do with a page it took over: edit it (carried says so), review it, quote it.
func TestAuditACarriedPageOperations(t *testing.T) {
	f := newAuditFlow(t)
	source, err := f.b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := f.b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	refOf := func(prepared map[string]any) string {
		for _, raw := range prepared["knowledge"].([]any) {
			if entry := raw.(map[string]any); entry["id"] == page.Page.ID {
				return entry["ref"].(string)
			}
		}
		return ""
	}
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	ref := refOf(first)
	if ref == "" {
		t.Fatalf("the first preparation reads the page: %s", auditJSON(first["knowledge"]))
	}
	// the person interrupts; the new request is about something else, so the page is not delivered again
	second := f.prepare(f.ask("Also rename the invoice numbering option, please do it now."), "invoice numbering option")
	t.Logf("carried: %s; knowledge now: %d entries, page again as %q", auditJSON(second["carried"]), len(second["knowledge"].([]any)), refOf(second))
	if second["carried"] == nil {
		t.Fatal("precondition: the page is taken over")
	}
	if got := auditJSON(second["carried"].(map[string]any)["articles"]); got != `["`+ref+`"]` {
		t.Errorf("the page is taken over under the reference the interrupted work read it by (%s): %s", ref, got)
	}
	// what the reply saved under a kind, and the refusal it gave for a kind
	savedAs := func(reply map[string]any, kind string) bool {
		items, _ := reply["saved"].([]any)
		for _, raw := range items {
			if item, _ := raw.(map[string]any); item["kind"] == kind {
				return true
			}
		}
		return false
	}
	refusal := func(reply map[string]any, kind string) string {
		items, _ := reply["errors"].([]any)
		for _, raw := range items {
			if item, _ := raw.(map[string]any); item["kind"] == kind {
				text, _ := item["error"].(string)
				return text
			}
		}
		return ""
	}
	// edit: carried says "edit the pages that work did not edit", and the edit is saved under the reference taken over
	saved, err := f.record(second, map[string]any{
		"observations": []any{map[string]any{"kind": "test", "title": "Totals", "content": "The totals match."}},
		"notes":        []any{map[string]any{"target": ref, "sources": []any{"new1"}, "change_reason": "Add the totals.", "edits": []any{map[string]any{"field": "body", "before": "The export keeps the filters.", "after": "The export keeps the filters and the totals."}}}}})
	t.Logf("edit of the carried page: err=%v %.700s", err, auditJSON(saved))
	if err != nil || saved["errors"] != nil || !savedAs(saved, "note") {
		t.Errorf("a page the interrupted work read and did not edit is edited with the new receipt: err=%v errors=%s", err, auditJSON(saved["errors"]))
	}
	if live, lerr := f.b.LiveWhere(func(p notebook.Page) bool { return p.ID == page.Page.ID }); lerr != nil || len(live) != 1 || !strings.Contains(live[0].Body, "and the totals.") {
		t.Errorf("the edit reached the page: %v %v", lerr, live)
	}
	// confirm: what the interrupted work had seen of the page was not taken over, so the page is read again first; the refusal says so
	saved, err = f.record(second, map[string]any{"reviewed": []any{ref}})
	t.Logf("reviewed carried page: err=%v %.500s", err, auditJSON(saved))
	if got := refusal(saved, "collective_review"); err != nil || saved["complete"] == true || savedAs(saved, "collective_review") || !strings.Contains(got, "read this article again") {
		t.Errorf("confirming a page that was taken over is refused with what to do first: err=%v %s", err, auditJSON(saved))
	}
	// quote: the passages the interrupted work prepared for quoting were not taken over; the refusal names the reference
	saved, err = f.record(second, map[string]any{"agreed": map[string]any{"summary": "Keep the filters.", "quotes": []any{map[string]any{"ref": ref, "text": "Keep the filters when exporting CSV."}}}})
	t.Logf("quote of the carried page: err=%v %.500s", err, auditJSON(saved))
	if got := refusal(saved, "plan"); err != nil || saved["complete"] == true || savedAs(saved, "plan") || !strings.Contains(got, "quotation reference") || !strings.Contains(got, `"`+ref+`"`) {
		t.Errorf("quoting a page that was taken over is refused, and the refusal names the reference: err=%v %s", err, auditJSON(saved))
	}
}
