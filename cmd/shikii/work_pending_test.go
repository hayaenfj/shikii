package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"testing"
	"time"
)

func TestIndependentCodePendingRejectsIgnoredFields(t *testing.T) {
	for _, mode := range []string{"correction", "replaces", "kind"} {
		t.Run(mode, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			o, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic input", Content: "Synthetic baseline evidence.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			p := loopPage(t, b, o.ID, map[string]any{"title": "Invoice routing", "lesson": "Check totals before invoice routing.", "applies_to": "invoice routing"})
			prep, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing"})
			if err != nil {
				t.Fatal(err)
			}
			first, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "observations": []any{map[string]any{"title": "New checked input", "content": "Synthetic new routing evidence."}}, "notes": []any{map[string]any{"title": "Invoice routing procedures", "lesson": "Check totals and route invoices.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{"new1"}}}})
			if err != nil {
				t.Fatal(err)
			}
			pending, ok := first["pending"].([]any)
			if !ok || len(pending) != 1 {
				t.Fatalf("pending setup failed: %v", first)
			}
			n := map[string]any{"ref": pending[0].(map[string]any)["ref"], "target": "new"}
			switch mode {
			case "correction":
				n["correction"] = map[string]any{"source_id": "missing-source", "quote": "not present", "action": "check synthetic routing"}
			case "replaces":
				n["replaces"] = []string{p.ID}
			case "kind":
				n["kind"] = "invalid-kind"
			}
			got, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "notes": []any{n}})
			pages, _ := b.Live()
			if err == nil && got["complete"] == true {
				t.Fatalf("unsupported %s silently accepted: complete=true pages=%d; wanted explicit rejection", mode, len(pages))
			}
		})
	}
}

func TestIndependentCodePendingInputsRetainBothRetryOrigins(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	o, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic input", Content: "Synthetic invoice baseline.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, o.ID, map[string]any{"title": "Invoice routing", "lesson": "Check totals before routing.", "applies_to": "invoice routing"})
	prep, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	n := map[string]any{"title": "Invoice routing procedures", "lesson": "Check totals and route invoices.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{"new1"}}
	input := map[string]any{"receipt": prep["receipt"], "observations": []any{map[string]any{"title": "New checked input", "content": "Synthetic new routing evidence."}}, "notes": []any{n}}
	first, err := workCall(t, b, "record", input)
	if err != nil || first["complete"] != false {
		t.Fatal(first, err)
	}
	n["target"] = "new"
	second, err := workCall(t, b, "record", input)
	if err != nil || second["complete"] != false {
		t.Fatal(second, err)
	}
	ref := second["pending"].([]any)[0].(map[string]any)["ref"]
	saved, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	retry, err := workCall(t, b, "record", input)
	if err != nil || retry["complete"] != true {
		t.Fatalf("second original input cannot replay its saved note: %v err=%v", retry, err)
	}
}

func TestIndependentCodePendingReferencesBindResolvedEvidence(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	o, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic input", Content: "Synthetic invoice baseline.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, o.ID, map[string]any{"title": "Invoice routing", "lesson": "Check totals before routing.", "applies_to": "invoice routing"})
	prep, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	obs := map[string]any{"title": "New checked input", "content": "SYNTHETIC_EVIDENCE_A"}
	input := map[string]any{"receipt": prep["receipt"], "observations": []any{obs}, "notes": []any{map[string]any{"title": "Invoice routing procedures", "lesson": "Check totals and route invoices.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{"new1"}}}}
	first, err := workCall(t, b, "record", input)
	if err != nil {
		t.Fatal(err)
	}
	refA := first["pending"].([]any)[0].(map[string]any)["ref"]
	obs["content"] = "SYNTHETIC_EVIDENCE_B"
	second, err := workCall(t, b, "record", input)
	if err != nil {
		t.Fatal(err)
	}
	refB := second["pending"].([]any)[0].(map[string]any)["ref"]
	saved, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "notes": []any{map[string]any{"ref": refB, "target": "new"}}})
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	pages, _ := b.Live()
	var body string
	for _, p := range pages {
		if p.Title == "Invoice routing procedures" {
			src, e := b.ReadSources([]string{p.Sources[0].ID})
			if e != nil {
				t.Fatal(e)
			}
			body = src["observations"].([]notebook.Observation)[0].Content
		}
	}
	if refA == refB || body != "SYNTHETIC_EVIDENCE_B" {
		t.Fatalf("changed resolved source reused pending ref: A=%v B=%v published=%s", refA, refB, body)
	}
}

func workPendingModeFixture(t *testing.T, extras ...map[string]any) (notebook.Book, string, string) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Mode baseline", Content: "Synthetic baseline.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, source.ID, map[string]any{"title": "Invoice mode procedure", "lesson": "Check totals before invoice routing.", "applies_to": "invoice routing", "body": "Keep existing context."})
	prep, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := prep["receipt"].(string)
	note := map[string]any{"title": "Invoice mode detail", "lesson": "Check totals and route invoices.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{"new1"}}
	for _, extra := range extras {
		for k, v := range extra {
			note[k] = v
		}
	}
	first, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "observations": []any{map[string]any{"title": "Mode evidence", "content": "Synthetic checked routing."}}, "notes": []any{note}})
	if err != nil || first["complete"] != false {
		t.Fatal(first, err)
	}
	return b, receipt, first["pending"].([]any)[0].(map[string]any)["ref"].(string)
}

func TestWorkNoteModesRejectUnusedEditsAndMissingTarget(t *testing.T) {
	for _, mode := range []string{"new draft edits", "pending new edits", "pending missing target"} {
		t.Run(mode, func(t *testing.T) {
			b, receipt, ref := workPendingModeFixture(t)
			note := map[string]any{"ref": ref, "target": "new"}
			edit := []any{map[string]any{"field": "lesson", "before": "Check totals", "after": "Discard totals"}}
			switch mode {
			case "new draft edits":
				note = map[string]any{"target": "new", "title": "Independent invoice mode", "lesson": "Check totals.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{"observed1"}, "edits": edit}
			case "pending new edits":
				note["edits"] = edit
			case "pending missing target":
				delete(note, "target")
			}
			result, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "notes": []any{note}})
			if err == nil {
				if result["complete"] == true || func() bool { items, _ := result["errors"].([]any); return len(items) == 0 }() {
					t.Error("unsupported note mode must explicitly reject its content", result)
				}
			}
			pages, err := b.Live()
			if err != nil || len(pages) != 1 {
				t.Fatal("an invalid mode published an article", pages, err)
			}
			valid, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
			if err != nil || valid["complete"] != true {
				t.Fatal("the original pending note must remain resolvable", valid, err)
			}
		})
	}
}

func TestWorkPendingMergeDoesNotDropStructuralMetadata(t *testing.T) {
	b, receipt, ref := workPendingModeFixture(t, map[string]any{"related": []string{"knowledge1"}})
	before, err := b.Live()
	if err != nil || len(before) != 1 {
		t.Fatal(before, err)
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "notes": []any{map[string]any{"ref": ref, "target": "knowledge1", "edits": []any{map[string]any{"field": "lesson", "before": "Check totals before invoice routing.", "after": "Check totals before routing and keep related knowledge."}}}}})
	if err == nil {
		items, _ := result["errors"].([]any)
		if result["complete"] == true || len(items) == 0 {
			t.Error("a merge must not silently discard the pending draft's relationships", result)
		}
	}
	after, err := b.Live()
	if err != nil || len(after) != 1 || after[0].Revision != before[0].Revision {
		t.Fatal("unsupported structural metadata changed the existing article", after, err)
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 2 {
		t.Fatal(pages, err)
	}
	for _, p := range pages {
		if p.Title == "Invoice mode detail" && (len(p.Related) != 1 || p.Related[0] != before[0].ID) {
			t.Fatal("new publication must retain the structural metadata", p)
		}
	}
}
