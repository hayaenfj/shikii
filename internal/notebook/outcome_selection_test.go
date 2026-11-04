package notebook

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func outcomeSelectionBook(t *testing.T) (Book, Page, func(string, string) string) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/synthetic/selection"}
	source, err := b.Capture(Observation{Kind: "test", Title: "Synthetic evidence", Content: "Checked the bounded workflow.", Reference: "synthetic", Version: "fixture", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := b.Ingest(Draft{Title: "Quartz workflow", Lesson: "Check the input conditions.", AppliesTo: "Quartz workflow", Kind: "howto", Scene: "checking an input", Sources: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	page := *result.Page
	evidence, err := b.OutcomeSources([]string{source.ID})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	add := func(conditions, failure string) string {
		t.Helper()
		n++
		details := OutcomeDetails{PageID: page.ID, Revision: page.Revision, Conditions: conditions, Action: "Check a bounded input", Failure: failure, Result: fmt.Sprintf("Synthetic result %d", n), Evidence: evidence}
		if failure != "" {
			details.Correction = "Use the checked parser correction."
		}
		content, _ := json.Marshal(details)
		snapshot, err := b.Capture(Observation{Kind: "agent", Title: "Synthetic execution", Content: string(content), Reference: "synthetic", Version: "fixture", ObservedAt: time.Unix(int64(n+2), 0)})
		if err != nil {
			t.Fatal(err)
		}
		refs, err := b.OutcomeSources([]string{snapshot.ID})
		if err != nil {
			t.Fatal(err)
		}
		if err = b.RecordOutcome(ProcedureOutcome{OutcomeDetails: details, Snapshot: refs[0]}); err != nil {
			t.Fatal(err)
		}
		return snapshot.ID
	}
	return b, page, add
}

func TestProcedureOutcomesSelectEarlierMatchingConditions(t *testing.T) {
	for _, conditions := range []string{"Linux arm64 quoted CSV", "Linux \u8acb\u6c42\u66f8 \u5f15\u7528\u7b26"} {
		t.Run(conditions, func(t *testing.T) {
			b, p, add := outcomeSelectionBook(t)
			wanted := add(conditions, "Quoted input was truncated.")
			add("Windows amd64 unquoted input", "")
			add("Darwin amd64 compressed input", "")
			got, err := b.ProcedureOutcomes(p, conditions)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].SourceID != wanted || got[0].Correction == "" {
				t.Fatal("the current conditions must retain their earlier checked correction", got)
			}
			fallback, err := b.ProcedureOutcomes(p, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(fallback) != 2 || fallback[0].Conditions != "Darwin amd64 compressed input" {
				t.Fatal("without current evidence, keep recency rather than infer applicability", fallback)
			}
		})
	}
}

func TestProcedureOutcomesKeepMatchingFailureAlongsideRecentResult(t *testing.T) {
	b, p, add := outcomeSelectionBook(t)
	failure := add("Linux arm64 bounded input", "The owner condition was lost.")
	add("Linux arm64 bounded input", "")
	latest := add("Linux arm64 bounded input", "")
	got, err := b.ProcedureOutcomes(p, "Linux arm64 bounded input")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatal(got)
	}
	seen := map[string]bool{}
	for _, o := range got {
		seen[o.SourceID] = true
	}
	if !seen[failure] || !seen[latest] {
		t.Fatal("recent results must not hide the matching failure and correction", got)
	}
	failures, err := b.procedureOutcomes(p, "Linux arm64 bounded input", true)
	if err != nil || len(failures) != 1 || failures[0].SourceID != failure {
		t.Fatal(failures, err)
	}
}
