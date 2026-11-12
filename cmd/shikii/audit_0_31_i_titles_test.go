package main

import (
	"strings"
	"testing"
	"time"
)

// Audit I of 0.31.0 (fifth round): the titles of shikii's own sources at every entrance that saves the agent's observation.

// wiki capture (the command line, MCP shikii_wiki, HTTP: all go through runNotebook) refuses an observation under a title shikii
// gives its own reports, as work record does: saved, it came in the next preparation as evidenceN and could never be cited, and the
// refusal then said shikii had written it. A title of the agent's own, and the title of a record of what the agent did, are saved.
func TestWikiCaptureRefusesTheTitleOfAReport(t *testing.T) {
	f := newAuditFlow(t)
	capture := func(title string) (any, error) {
		return improvementCall(t, f.b, map[string]any{"operation": "capture", "observation": map[string]any{"kind": "agent", "title": title, "content": "I ran the release procedure of the CSV export: make test passed (" + title + ").", "reference": "make test", "version": "audit-i", "observed_at": time.Now().UTC().Format(time.RFC3339)}})
	}
	for _, title := range []string{workProcedureName, workResultTitle, "  " + workReviewTitle + " "} {
		saved, err := capture(title)
		if err == nil || !strings.Contains(err.Error(), "is the one shikii gives its own reports") || !strings.Contains(err.Error(), "a title of its own") {
			t.Errorf("wiki capture of an observation titled %q is not refused with the reason and what to send: %v %.200s", title, err, auditJSON(saved))
		}
	}
	for _, title := range []string{"Release procedure run", "Work trial execution"} {
		if _, err := capture(title); err != nil {
			t.Errorf("wiki capture of an observation titled %q is refused: %v", title, err)
		}
	}
}

// The title is compared with the table in one form, the spaces around it left out, both where an observation is refused and where a
// saved source is read (the refusal trimmed the title and the reading did not: audit I of 0.31.0).
func TestATitleIsComparedWithoutItsSpaces(t *testing.T) {
	for title, report := range map[string]bool{
		workProcedureName:                 true,
		"  " + workProcedureName + "\n":   true,
		" " + workResultTitle:             true,
		"Work trial execution ":           false,
		"Procedure execution, by the way": false,
	} {
		if got := workReport("agent", title); got != report {
			t.Errorf("workReport(agent, %q) = %v, want %v", title, got, report)
		}
		if got := reportTitleRefusal(title) != nil; got != report {
			t.Errorf("reportTitleRefusal(%q) refuses: %v, want %v", title, got, report)
		}
	}
	if workReport("test", workProcedureName) {
		t.Errorf("a source of another kind under the title is taken for a report")
	}
}
