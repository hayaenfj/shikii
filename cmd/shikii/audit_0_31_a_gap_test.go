package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// audit A (run in the copy and in the copy with the checks taken out): a saved report repaired with itself as its evidence.
func TestAuditAGapRepairCitesItself(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	record := func(fields map[string]any) map[string]any {
		t.Helper()
		fields["receipt"] = prepared["receipt"]
		saved, err := workCall(t, b, "record", fields)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	first := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"I looked at it"}}})
	id, _ := first["storage"].(map[string]any)["result_source"].(string)
	if id == "" || first["complete"] == true {
		t.Fatalf("precondition: the report is saved and waits for its evidence: %v", first)
	}
	for _, ref := range []string{"result", "observed1", id} {
		saved := record(map[string]any{"result": map[string]any{"ref": id, "evidence": []any{ref}}})
		raw, _ := json.Marshal(saved["errors"])
		t.Logf("repair with %q: complete=%v errors=%.200s", ref, saved["complete"], raw)
		if saved["complete"] == true || !strings.Contains(string(raw), "is this result itself") {
			t.Errorf("a saved report repaired with itself (%s) as its evidence is not refused: complete=%v %s", ref, saved["complete"], raw)
		}
	}
}

// audit A (run in both copies): the candidates shown and the lessons delivered, bound by bound.
func TestAuditAGapCandidatesOfDeliveredLessons(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
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
	for bound := 8192; bound <= 16000; bound += 97 {
		var out string
		var cerr error
		withStdin(t, fmt.Sprintf(`{"query":"CSV export filters","request":"Check the CSV export","max_bytes":%d}`, bound), func() { out, cerr = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
		if cerr != nil {
			continue
		}
		var reply map[string]any
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			t.Fatal(err)
		}
		delivered := map[string]bool{}
		for _, raw := range reply["knowledge"].([]any) {
			delivered[raw.(map[string]any)["id"].(string)] = true
		}
		context, _ := reply["knowledge_context"].(map[string]any)
		search, _ := context["search"].(map[string]any)
		candidates, _ := search["candidates"].([]any)
		if len(candidates) == 0 {
			continue
		}
		shown := map[string]bool{}
		for _, raw := range candidates {
			shown[raw.(map[string]any)["id"].(string)] = true
		}
		missing := 0
		for id := range delivered {
			if !shown[id] {
				missing++
			}
		}
		if missing > 0 {
			t.Errorf("at %d bytes: %d candidates shown, and %d of the %d delivered lessons are after the cut (the next page sends them again)", bound, len(candidates), missing, len(delivered))
		}
	}
}
