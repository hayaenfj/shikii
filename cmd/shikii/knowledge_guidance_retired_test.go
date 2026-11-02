package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGuidanceSupersededProcedureReview(t *testing.T) {
	b, source := loopBook(t)
	old := loopPage(t, b, source, map[string]any{"title": "Quartz earlier workflow", "lesson": "Check Quartz exports.", "applies_to": "Quartz exports", "kind": "howto", "body": "Use the raw parser for Quartz exports."})
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + old.ID, "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := prepared["knowledge"].([]any)[0].(map[string]any)["ref"]
	guidanceFailedRun(t, b, ref, prepared["receipt"], "The structured parser preserved all quoted fields.")
	newSource, err := improvementCall(t, b, map[string]any{"operation": "capture", "observation": map[string]any{"kind": "test", "title": "Replacement check", "content": "The replacement was checked separately.", "reference": "synthetic", "version": "1", "observed_at": "2026-11-01T00:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	sourceID := loopObject(t, newSource)["id"].(string)
	current := loopPage(t, b, sourceID, map[string]any{"title": "Quartz replacement workflow", "lesson": "Use the structured parser.", "applies_to": "Quartz exports", "kind": "howto", "body": "Use the structured parser for Quartz exports.", "replaces": []string{old.ID}})
	reviews, err := b.OutcomeMaintenance([]string{old.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 0 {
		t.Errorf("a superseded procedure must not return as a current outcome review: %v", reviews)
	}
	active, err := b.Live()
	if err != nil || len(active) != 1 || active[0].ID != current.ID {
		t.Fatal("control: only the replacement is live", active, err)
	}
	historical, err := b.Query("lesson:" + old.ID + "@" + old.Revision)
	if err != nil || len(historical.Lessons) != 1 {
		t.Fatal("control: earlier evidence remains readable explicitly", historical, err)
	}
	paths, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", sourceID+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var changed map[string]any
	if err = json.Unmarshal(data, &changed); err != nil {
		t.Fatal(err)
	}
	changed["content"] = "Changed replacement evidence."
	data, _ = json.Marshal(changed)
	if err = os.WriteFile(paths[0], data, 0600); err != nil {
		t.Fatal(err)
	}
	if reviews, err = b.OutcomeMaintenance([]string{old.ID}); err != nil || len(reviews) != 0 {
		t.Fatal("invalid replacement evidence cannot revive a superseded procedure", reviews, err)
	}
}
