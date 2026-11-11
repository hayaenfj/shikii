package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// audit A (0.31.0): a result with nothing observed cites the report it sent just before (refused as itself), under the name the
// refusal left it with.
func TestAuditAResultCitesItsRefusedEarlierReport(t *testing.T) {
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
	first := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "evidence": []any{"observed1"}}})
	raw, _ := json.Marshal(first)
	t.Logf("first: %s", raw)
	if first["complete"] == true || !strings.Contains(string(raw), "is this result itself") {
		t.Fatalf("the first report is refused as its own evidence: %s", raw)
	}
	// the agent corrects the wording and sends it again with the same evidence: nothing was observed in between
	second := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "checked": "Looked at it.", "evidence": []any{"observed1"}}})
	raw, _ = json.Marshal(second)
	t.Logf("second: %s", raw)
	errs, _ := second["errors"].([]any)
	refusedResult := false
	for _, e := range errs {
		if item := e.(map[string]any); strings.HasPrefix(item["kind"].(string), "result") {
			refusedResult = true
		}
	}
	if !refusedResult {
		t.Errorf("a result whose only evidence is the agent's own earlier report (observed1, saved by the refused call, nothing observed) was accepted: %.300s", raw)
	}
	// the reply asks to repair the first report with result:{ref,evidence}: the agent names the second report (result)
	old := first["storage"].(map[string]any)["result_source"].(string)
	third := record(map[string]any{"result": map[string]any{"ref": old, "evidence": []any{"result"}}})
	raw, _ = json.Marshal(third)
	t.Logf("third: %s", raw)
	fourth := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "checked": "Looked at it.", "evidence": []any{"observed1"}}})
	raw, _ = json.Marshal(fourth)
	t.Logf("fourth: %s", raw)
	if third["complete"] == true || fourth["complete"] == true {
		t.Errorf("two reports that cite each other, with nothing observed, are recorded as complete: third=%v fourth=%v", third["complete"], fourth["complete"])
	}
}

// audit A: a report saved without evidence, then a corrected report that cites the first one or the ending review shikii itself wrote.
func TestAuditAResultCitesAnEarlierReportOrItsEndingReview(t *testing.T) {
	for _, cited := range []string{"observed1", "observed2"} { // observed1: the first report; observed2: the ending review of that report
		t.Run(cited, func(t *testing.T) {
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
			first := record(map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
			raw, _ := json.Marshal(first["saved"])
			t.Logf("first: complete=%v saved=%s", first["complete"], raw)
			second := record(map[string]any{"result": map[string]any{"changed": "Renamed the button.", "checked": "It works.", "sufficient": true, "evidence": []any{cited}}})
			raw, _ = json.Marshal(second)
			t.Logf("second: %.700s", raw)
			if errs, _ := second["errors"].([]any); len(errs) == 0 && second["complete"] == true {
				t.Errorf("a result (sufficient: true) whose only evidence is %s, a text the work itself reported and nothing it observed, is complete", cited)
			}
		})
	}
}
