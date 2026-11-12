package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The titles of shikii's own sources at every entrance that saves what an agent observed (audits I and K of 0.31.0).

// wiki capture (the command line, MCP shikii_wiki, HTTP: all go through runNotebook) refuses an observation of the agent under any
// title of a source shikii writes, as work record does. Saved, a report would come in the next preparation as evidenceN and could
// never be cited, and a record of what the agent did (a trial, a reading) would be read back as shikii's record, with whatever
// content was sent. An observation of another kind keeps the title, and a title of the agent's own is saved.
func TestWikiCaptureRefusesTheTitleOfAReport(t *testing.T) {
	f := newAuditFlow(t)
	capture := func(kind, title string) (any, error) {
		return improvementCall(t, f.b, map[string]any{"operation": "capture", "observation": map[string]any{"kind": kind, "title": title, "content": "I ran the release procedure of the CSV export: make test passed (" + title + ").", "reference": "make test", "version": "audit-i", "observed_at": time.Now().UTC().Format(time.RFC3339)}})
	}
	for _, k := range workSources() {
		for _, title := range []string{k.Title(), "  " + k.Title() + " "} {
			saved, err := capture("agent", title)
			if err == nil || !strings.Contains(err.Error(), "is the one shikii gives its own reports") || !strings.Contains(err.Error(), "a title of its own") {
				t.Errorf("wiki capture of an observation titled %q is not refused with the reason and what to send: %v %.200s", title, err, auditJSON(saved))
			}
		}
	}
	for _, o := range [][2]string{{"agent", "Release procedure run"}, {"test", notebook.WorkProcedure.Title()}, {"human", notebook.WorkResult.Title()}} {
		if _, err := capture(o[0], o[1]); err != nil {
			t.Errorf("wiki capture of an observation of kind %s titled %q is refused: %v", o[0], o[1], err)
		}
	}
}

// A title is compared in one form, the spaces around it left out, where an observation is refused and where a saved source is read
// (the refusal trimmed the title and the reading did not: audit I of 0.31.0). Every title of a work source is refused for an
// observation of the agent; only the reports and archives are kept out of evidence.
func TestATitleIsComparedWithoutItsSpaces(t *testing.T) {
	for title, report := range map[string]bool{
		notebook.WorkProcedure.Title():               true,
		"  " + notebook.WorkProcedure.Title() + "\n": true,
		" " + notebook.WorkResult.Title():            true,
		notebook.WorkTrialExecution.Title() + " ":    false,
	} {
		if got := testWorkReport("agent", title); got != report {
			t.Errorf("a source titled %q is a report: %v, want %v", title, got, report)
		}
		if testTitleRefusal(title) == nil {
			t.Errorf("an observation titled %q is not refused", title)
		}
	}
	for _, title := range []string{"Procedure execution, by the way", "make test"} {
		if testWorkReport("agent", title) || testTitleRefusal(title) != nil {
			t.Errorf("a title of the agent's own (%q) is taken for shikii's", title)
		}
	}
	if testWorkReport("test", notebook.WorkProcedure.Title()) {
		t.Errorf("a source of another kind under the title is taken for a report")
	}
}
