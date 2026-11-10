package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// A procedure's reported failure cites a saved source. When that one file can no longer be read, the operations
// that list what waits for a review must still open: the article list, the reflection, the notice before a
// request and the work preparation, for unrelated work and for the procedure itself. The report is named as
// unreadable, nothing of it is shown, and its article cannot be marked as reviewed.
func TestOperationsOpenWhenOutcomeEvidenceIsUnreadable(t *testing.T) {
	b, source := loopBook(t)
	procedure := loopPage(t, b, source, map[string]any{"title": "Quartz export workflow", "lesson": "Check Quartz exports.", "applies_to": "Quartz exports", "kind": "howto", "body": "Use the raw parser for Quartz exports."})
	loopPage(t, b, source, map[string]any{"title": "Commit order", "lesson": "Run the focused test before a commit.", "applies_to": "commit order", "kind": "rule", "scene": "when committing"})
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + procedure.ID, "request": "Check Quartz exports"})
	if err != nil {
		t.Fatal(err)
	}
	ref := prepared["knowledge"].([]any)[0].(map[string]any)["ref"]
	const result = "The structured parser preserved all quoted fields."
	guidanceFailedRun(t, b, ref, prepared["receipt"], result)
	// Settle the capture queue: the sources of the report wait there until they are sorted.
	pending, err := b.PendingObservations()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, o := range pending {
		ids = append(ids, o.ID)
	}
	if len(ids) > 0 {
		if _, err = b.Ingest(notebook.Draft{Sources: ids}); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := b.Maintenance()
	if err != nil || len(tasks) != 2 || tasks[0].PageID != procedure.ID || len(tasks[0].Outcomes) != 1 || len(tasks[0].Outcomes[0].Outcome.Evidence) != 1 {
		t.Fatalf("the fixture changed: the procedure waits for a review with its report, before the unrelated rule: %+v %v", tasks, err)
	}
	evidence := tasks[0].Outcomes[0].Outcome.Evidence[0].SourceID
	paths, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", evidence+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	if err = os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	// What an operation returns must name the unreadable report and its source, and show none of the report.
	check := func(name string, v any) {
		t.Helper()
		data, _ := json.Marshal(v)
		if !strings.Contains(string(data), "cannot be read") || !strings.Contains(string(data), evidence+" (missing)") {
			t.Errorf("%s does not name the unreadable report and its source %s: %s", name, evidence, data)
		}
		for _, secret := range []string{result, "The raw parser loses quoted fields.", "Use the structured parser and retain quoted fields.", "Quartz exports on Linux"} {
			if strings.Contains(string(data), secret) {
				t.Errorf("%s shows %q, content of a report that could not be checked: %s", name, secret, data)
			}
		}
	}

	listed, err := runNotebook(b.Root, b.Scope, notebookRequest{Operation: "query"})
	if err != nil {
		t.Errorf("wiki query (the article list) fails: %v", err)
	} else {
		check("wiki query", loopObject(t, listed)["maintenance"])
	}
	reflection, err := reflectQuery(b.Root, b.Scope)
	if err != nil {
		t.Errorf("reflect query fails: %v", err)
	} else {
		check("reflect query", reflection.Maintenance)
	}
	if _, _, err = reflectContext(b.Root, b.Scope); err != nil {
		t.Errorf("the reflection notice before a request fails: %v", err)
	}
	if _, err = workCall(t, b, "prepare", map[string]any{"query": "commit order", "request": "Check the commit order"}); err != nil {
		t.Errorf("work prepare for unrelated work fails: %v", err)
	}
	// The preparation that finds the procedure itself opens too, and offers its review with the unreadable report.
	again, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + procedure.ID, "request": "Check Quartz exports"})
	if err != nil {
		t.Fatalf("work prepare for the procedure itself fails: %v", err)
	}
	reviews, _ := again["maintenance"].([]any)
	if len(reviews) != 1 {
		t.Fatalf("the preparation must offer the review of the procedure: %v", again["maintenance"])
	}
	check("work prepare for the procedure", reviews)
	// Recording that review is refused: the report could not be checked.
	recorded, err := workCall(t, b, "record", map[string]any{"receipt": again["receipt"], "maintenance": []any{map[string]any{"ref": reviews[0].(map[string]any)["ref"], "reason": "Read the report."}}})
	if err == nil && recorded["complete"] == true {
		t.Errorf("an article whose report cannot be checked was marked as reviewed: %v", recorded)
	}
	after, err := b.Maintenance()
	if err != nil || len(after) != 2 || after[0].PageID != procedure.ID {
		t.Errorf("after the refused review the procedure must still wait for one: %+v %v", after, err)
	}
}
