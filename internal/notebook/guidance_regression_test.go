package notebook

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestGuidanceCounterexampleUsesIndividualEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := Book{Root: t.TempDir(), Scope: "/synthetic/audit"}
	src, err := b.Capture(Observation{Kind: "test", Title: "Synthetic evidence", Content: "Synthetic failures were observed.", Reference: "synthetic-only", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	body := "Run the raw parser for Quartz exports."
	r, err := b.Ingest(Draft{Title: "Quartz exports raw parser quoted fields check use the for", Lesson: "Check parser outputs.", AppliesTo: "Parser workflow", Kind: "howto", Scene: "Parser work", Body: &body, Sources: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	p := *r.Page
	evidence, err := b.OutcomeSources([]string{src.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Hash ordering is the production tie breaker. Keep five immutable synthetic failures
	// under a shared parent title and require the relevant one in the first two. These are fixed finite synthetic variants, not a race loop.
	records := []ProcedureOutcome{}
	for i, fail := range []string{"Temporary network unavailable.", "Temporary socket unavailable.", "Quartz exports raw parser loses quoted fields.", "Temporary service unavailable.", "Temporary connection unavailable."} {
		d := OutcomeDetails{PageID: p.ID, Revision: p.Revision, Conditions: "Same synthetic environment", Action: "Run the workflow", Failure: fail, Correction: "Use the checked correction", Result: "Synthetic report", Evidence: evidence}
		data, _ := json.Marshal(d)
		o, err := b.Capture(Observation{Kind: "agent", Title: fmt.Sprintf("Synthetic execution %d", i), Content: string(data), Reference: "synthetic-only", Version: "v1", ObservedAt: time.Unix(int64(10+i), 0)})
		if err != nil {
			t.Fatal(err)
		}
		refs, err := b.OutcomeSources([]string{o.ID})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, ProcedureOutcome{OutcomeDetails: d, Snapshot: refs[0]})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Snapshot.ID < records[j].Snapshot.ID })
	for _, o := range records {
		if err := b.RecordOutcome(o); err != nil {
			t.Fatal(err)
		}
		t.Logf("indexed failure=%q source_order=%s", o.Failure, o.Snapshot.ID)
	}
	v, err := b.Assistance("Check Quartz exports quoted fields", "Use the raw parser for Quartz exports", "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.Checks {
		for _, o := range c.Outcomes {
			t.Logf("returned failure=%q", o.Failure)
			found = found || strings.Contains(o.Failure, "loses quoted")
		}
	}
	if !found {
		t.Fatal("the counterexample lane omitted the relevant failure despite retaining unrelated failures")
	}
}

func TestGuidanceOmittedPassagesKeepTheirRoles(t *testing.T) {
	for _, role := range []string{"current", "history", "recheck", "counterexample"} {
		t.Run(role, func(t *testing.T) {
			p := Page{ID: "0000000000000001", Revision: "0000000000000002", Lesson: "Current guidance", AppliesTo: "Synthetic workflow"}
			for i := 0; i < 4; i++ {
				quote := fmt.Sprintf("current-%d-", i) + strings.Repeat(string(rune('A'+i)), 540)
				p.Body += quote + "\n\n"
				p.Passages = append(p.Passages, KnowledgePassage{Role: "current", Quote: quote, Conditions: "Current condition", Reason: "Checked source"})
			}
			quote := "Omitted known quotation: " + strings.Repeat("z", 276)
			p.Body += quote + "\n\nIndependent unclassified detail."
			p.Passages = append(p.Passages, KnowledgePassage{Role: role, Quote: quote, Conditions: "Explicit condition", Reason: "Explicit source-backed interpretation"})
			if err := validatePassages(p.Body, p.Passages); err != nil {
				t.Fatal(err)
			}
			for _, budget := range []int{0, 1, 200, 2400} {
				v := Orient(p, "Omitted known quotation independent detail", budget)
				if !v.Omitted || !v.NeedsInterpretation {
					t.Fatal("omission must remain explicit", role, budget, v)
				}
				for _, e := range v.Unclassified {
					for _, known := range p.Passages {
						start := strings.Index(p.Body, known.Quote)
						if e.Start < start+len(known.Quote) && start < e.End {
							t.Fatal("an omitted interpreted range must not become unclassified", role, budget, e)
						}
					}
				}
			}
		})
	}
}
