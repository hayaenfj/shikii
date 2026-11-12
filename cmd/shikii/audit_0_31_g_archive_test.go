package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The same work, without a fabricated source: a note whose placement is uncertain waits (pending wiki work); the agent prepares
// the same request again; the preparation keeps the work it replaces in an archive source ("Pending wiki work before replacing
// preparation") and lists it among the observations. The agent settles the note, then records a result that cites the archive.
func TestAuditGTheArchiveOfTheSameWorkAsEvidence(t *testing.T) {
	f := newAuditFlow(t)
	check, err := f.b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "Keep the existing filters when the CSV columns change."
	if _, err := f.b.Ingest(notebook.Draft{Sources: []string{check.ID}, Title: "CSV export keeps its filters", Lesson: "Keep the existing filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
		t.Fatal(err)
	}
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	note := map[string]any{"title": "The CSV export keeps the filters", "lesson": "Keep the existing filters when exporting CSV.", "body": "Keep the existing filters when the CSV columns change.", "applies_to": "CSV export", "kind": "rule", "scene": "when exporting CSV", "sources": []any{"new1"}}
	saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Filters", "content": "The filters stay after the column change."}}, "notes": []any{note}})
	t.Logf("note: err=%v complete=%v pending=%.300s", err, saved["complete"], auditJSON(saved["pending"]))
	again := f.prepare(ids, "CSV export filters")
	ref, archive := "", ""
	for _, raw := range again["observations"].([]any) {
		o := raw.(map[string]any)
		text := auditJSON(o["source"])
		if strings.Contains(text, "Pending wiki work before replacing preparation") || strings.Contains(text, "Required wiki note and original prepared bindings") {
			ref, _ = o["ref"].(string)
			archive = text
		}
	}
	t.Logf("prepared again: the archive is %q; reasoning=%.300s", ref, auditJSON(again["reasoning"]))
	if ref == "" {
		t.Fatalf("precondition: an archive of the work is listed: %.400s", auditJSON(again["observations"]))
	}
	for _, title := range []string{"Pending wiki work before replacing preparation", "Required wiki note and original prepared bindings"} {
		if strings.Contains(archive, title) {
			t.Logf("the archive listed as %s is titled %q", ref, title)
		}
	}
	for _, raw := range auditGSlice(saved["pending"]) {
		if m, ok := raw.(map[string]any); ok {
			keys := []string{}
			for k, v := range m {
				if k != "candidates" {
					keys = append(keys, k+"="+auditJSON(v))
				}
			}
			t.Logf("pending item: %.600s", strings.Join(keys, " "))
		}
	}
	t.Logf("reasoning of the new receipt: %.900s", auditJSON(again["reasoning"]))
	// settle the waiting note (target new), then the result cites the archive only
	pendingRef := ""
	if r, ok := again["reasoning"].(map[string]any); ok {
		if notes, ok := r["pending_notes"].(map[string]any); ok {
			for k := range notes {
				pendingRef = k
			}
		}
	}
	fields := map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Nothing new.", "sufficient": true, "evidence": []any{ref}}}
	if pendingRef != "" {
		fields["notes"] = []any{map[string]any{"ref": pendingRef, "target": "new"}}
	}
	done, err := f.record(again, fields)
	t.Logf("result citing %s: err=%v complete=%v errors=%.300s storage=%.200s", ref, err, done["complete"], auditJSON(done["errors"]), auditJSON(done["storage"]))
	// the note waits again under note1: place it as a new page, then finish the saved result by its ref
	settle, err := f.record(again, map[string]any{"notes": []any{map[string]any{"ref": "note1", "target": "new"}}})
	t.Logf("note1 as a new page: err=%v complete=%v errors=%.200s", err, settle["complete"], auditJSON(settle["errors"]))
	if st, ok := done["storage"].(map[string]any); ok && st["result_source"] != nil {
		fin, ferr := f.record(again, map[string]any{"result": map[string]any{"ref": st["result_source"]}})
		t.Logf("result by its ref: err=%v complete=%v errors=%.300s reasoning=%.300s", ferr, fin["complete"], auditJSON(fin["errors"]), auditJSON(fin["reasoning"]))
		done = fin
	}
	if done["complete"] == true {
		t.Errorf("the result of the work is complete with %s as its only evidence: the archive shikii wrote of this same work while it prepared again", ref)
	}
}

func auditGSlice(v any) []any { s, _ := v.([]any); return s }

// The next request of the person: the archive of the earlier request's work is still waiting to be sorted, the preparation lists
// it among the observations, and a result that observed nothing cites it.
func TestAuditGTheArchiveOfTheEarlierRequestAsEvidence(t *testing.T) {
	f := newAuditFlow(t)
	check, err := f.b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "Keep the existing filters when the CSV columns change."
	if _, err := f.b.Ingest(notebook.Draft{Sources: []string{check.ID}, Title: "CSV export keeps its filters", Lesson: "Keep the existing filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
		t.Fatal(err)
	}
	ids := f.ask(planningTestRequest)
	first := f.prepare(ids, "CSV export filters")
	note := map[string]any{"title": "The CSV export keeps the filters", "lesson": "Keep the existing filters when exporting CSV.", "body": body, "applies_to": "CSV export", "kind": "rule", "scene": "when exporting CSV", "sources": []any{"new1"}}
	if _, err := f.record(first, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Filters", "content": "The filters stay after the column change."}}, "notes": []any{note}}); err != nil {
		t.Fatal(err)
	}
	again := f.prepare(ids, "CSV export filters") // the agent lost the receipt and prepares the same request again
	required := ""
	if r, ok := again["reasoning"].(map[string]any); ok {
		for k := range r["pending_notes"].(map[string]any) {
			required = k
		}
	}
	a, err := f.record(again, map[string]any{"notes": []any{map[string]any{"ref": required, "target": "new"}}})
	t.Logf("the required note again: err=%v errors=%.200s pending=%.120s", err, auditJSON(a["errors"]), auditJSON(a["pending"]))
	b, err := f.record(again, map[string]any{"notes": []any{map[string]any{"ref": "note1", "target": "new"}}})
	t.Logf("note1 as a new page: err=%v complete=%v errors=%.200s reasoning=%.300s", err, b["complete"], auditJSON(b["errors"]), auditJSON(b["reasoning"]))
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": t.TempDir(), "last_assistant_message": "Done."})
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	ref := ""
	for _, raw := range next["observations"].([]any) {
		o := raw.(map[string]any)
		text := auditJSON(o["source"])
		if strings.Contains(text, "Pending wiki work before replacing preparation") || strings.Contains(text, "Required wiki note and original prepared bindings") {
			ref, _ = o["ref"].(string)
		}
	}
	t.Logf("the next request: the archive of the earlier work is %q; its reasoning=%.200s", ref, auditJSON(next["reasoning"]))
	for _, raw := range next["observations"].([]any) {
		o := raw.(map[string]any)
		for _, title := range []string{"Pending wiki work before replacing preparation", "Required wiki note and original prepared bindings"} {
			if o["ref"] == ref && strings.Contains(auditJSON(o["source"]), title) {
				t.Logf("%s is titled %q", ref, title)
			}
		}
	}
	if ref == "" {
		t.Fatalf("precondition: the archive is listed: %.300s", auditJSON(next["observations"]))
	}
	done, err := f.record(next, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Nothing new.", "sufficient": true, "evidence": []any{ref}}})
	t.Logf("result citing %s: err=%v complete=%v errors=%.300s", ref, err, done["complete"], auditJSON(done["errors"]))
	if err == nil && done["complete"] == true {
		t.Errorf("INVARIANT 9/10: the next request's result observed nothing and is complete with %s as its only evidence: the archive shikii wrote of the earlier request's work", ref)
	}
}
