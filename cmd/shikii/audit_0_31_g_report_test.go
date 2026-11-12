package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// An observation the agent itself sends, with kind agent (the default when kind is left out) and a title that is one of the
// titles shikii gives its reports. It is what the agent checked, yet it can no longer be evidence, and the refusal calls it a
// report a work wrote about itself.
func TestAuditGAnObservationTitledLikeAReport(t *testing.T) {
	for _, title := range []string{workResultTitle, workReviewTitle, workRecoveryTitle, workProcedureName} {
		t.Run(title, func(t *testing.T) {
			f := newAuditFlow(t)
			first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
			saved, err := f.record(first, map[string]any{"observations": []any{map[string]any{"title": title, "content": "Ran make test: 412 passed, 0 failed."}}})
			if err != nil {
				t.Fatal(err)
			}
			_ = saved
			done, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Ran make test.", "evidence": []any{"observed1"}}})
			t.Logf("title %q: err=%v complete=%v errors=%.300s", title, err, done["complete"], auditJSON(done["errors"]))
			if err != nil || strings.Contains(auditJSON(done["errors"]), "wrote about itself") {
				t.Errorf("the agent's own check result, sent with the title %q, is refused as evidence: %v %.300s", title, err, auditJSON(done["errors"]))
			}
			// control: the same observation with another title is evidence
			g := newAuditFlow(t)
			p := g.prepare(g.ask(planningTestRequest), "CSV export filters")
			if _, err := g.record(p, map[string]any{"observations": []any{map[string]any{"title": "make test", "content": "Ran make test: 412 passed, 0 failed."}}}); err != nil {
				t.Fatal(err)
			}
			c, err := g.record(p, map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Ran make test.", "evidence": []any{"observed1"}}})
			t.Logf("control, title \"make test\": err=%v complete=%v errors=%.200s", err, c["complete"], auditJSON(c["errors"]))
		})
	}
}

// Sources shikii writes about a work besides the four titles: the archives it keeps when a preparation is replaced with wiki work
// pending, when a required note is kept, and at the session's capacity boundary. They wait to be sorted like any source, so the
// next preparation lists them as evidenceN. A result that observed nothing cites one and is complete.
func TestAuditGAnArchiveOfTheWorkIsEvidence(t *testing.T) {
	for _, title := range []string{"Pending wiki work before replacing preparation", "Required work and bindings at the session capacity boundary", "Required wiki note and original prepared bindings"} {
		t.Run(title, func(t *testing.T) {
			f := newAuditFlow(t)
			content, _ := json.Marshal(map[string]any{"work": map[string]any{"result": map[string]any{"changed": "Edited the export.", "sufficient": true}, "sources": map[string]string{"observed1": "0123456789abcdef"}}})
			archive, err := f.b.Capture(notebook.Observation{Kind: "agent", Title: title, Content: string(content), Reference: "work capacity recovery for turn 0123456789abcdef", Version: "work-capacity", ObservedAt: time.Now().UTC()})
			if err != nil {
				t.Fatal(err)
			}
			prepared := f.prepare(f.ask(planningTestRequest), "CSV export filters")
			ref := ""
			for _, raw := range prepared["observations"].([]any) {
				o := raw.(map[string]any)
				if strings.Contains(auditJSON(o["source"]), archive.ID) {
					ref, _ = o["ref"].(string)
				}
			}
			if ref == "" {
				t.Skipf("precondition: the archive is listed among the observations: %.300s", auditJSON(prepared["observations"]))
			}
			done, err := f.record(prepared, map[string]any{"result": map[string]any{"changed": "Edited the export.", "checked": "Nothing new.", "sufficient": true, "evidence": []any{ref}}})
			t.Logf("%s as %s: err=%v complete=%v", title, ref, err, done["complete"])
			if err == nil && done["complete"] == true {
				t.Errorf("a result that observed nothing is complete with %s as its only evidence: a source shikii wrote about a work (%q)", ref, title)
			}
		})
	}
}

// A trial's result is evidence a result can cite, like its plan. The plan's refusal of a report has a test; the result's has none.
func TestAuditGATrialResultCannotCiteAReport(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	if _, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{trialObservation("The totals match.")}}); err != nil {
		t.Fatal(err)
	}
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "observed1")}, nil)
	saved, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the totals.", "checked": "Compared the totals.", "evidence": []string{"observed1"}}})
	if err != nil || saved["storage"] == nil {
		t.Fatalf("precondition: the result is saved: %v %.300s", err, auditJSON(saved))
	}
	ref := trialSavedRef(t, plan, "trial")
	got, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "The totals match.", "evidence": []string{"result"}}}})
	raw := auditJSON(got) + " " + errString(err)
	t.Logf("a trial result citing the result of this work: %.300s", raw)
	if !(strings.Contains(raw, "wrote about itself") || strings.Contains(raw, "wrote about a work itself")) {
		t.Errorf("a trial result cites the result of this work as its evidence and is not refused: %.300s", raw)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
