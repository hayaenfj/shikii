package notebook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// What each kind of work source is called in a refusal, and whether evidence may cite it: the reports and the archives a work wrote
// about itself are no evidence; the records of what the agent did are.
func TestEachWorkSourceSaysWhatItIs(t *testing.T) {
	want := map[WorkSource][2]string{
		WorkResult:          {"an earlier result", "no"},
		WorkReview:          {"an ending review", "no"},
		WorkRecovery:        {"the source that binds a result to its evidence", "no"},
		WorkProcedure:       {"the record of a procedure a result closed", "no"},
		WorkNoteIntent:      {"an archive of the work's state", "no"},
		WorkCapacity:        {"an archive of the work's state", "no"},
		WorkPending:         {"an archive of the work's state", "no"},
		WorkTrialPlan:       {"a record of what the agent did", "yes"},
		WorkTrialExecution:  {"a record of what the agent did", "yes"},
		WorkTrialComparison: {"a record of what the agent did", "yes"},
		WorkReading:         {"a record of what the agent did", "yes"},
		WorkOutcome:         {"a record of what the agent did", "yes"},
	}
	if len(WorkSources()) != len(want) {
		t.Fatalf("%d kinds, %d described here", len(WorkSources()), len(want))
	}
	for _, k := range WorkSources() {
		w := want[k]
		if !strings.Contains(k.Describe(), w[0]) || k.Citable() != (w[1] == "yes") || k.Title() == "" {
			t.Errorf("%q: described %q, citable %v; want %q, citable %s", k.Title(), k.Describe(), k.Citable(), w[0], w[1])
		}
	}
	if (WorkSource{}).Title() != "" || (WorkSource{}).Citable() || !(WorkSource{}).IsZero() {
		t.Errorf("the zero value is a kind")
	}
}

// A reading execution is read back only from a source shikii wrote as one (kind agent): an observation of another kind under the
// same title and with the same content is refused (the kind is part of what a record is, not only the title).
func TestAReadingIsReadOnlyFromAWorkSource(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/reading-kind"}
	o, err := b.capture(Observation{Kind: "test", Title: "Quality result", Content: "The condition was recovered.", Reference: "synthetic", Version: "fixture", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := b.OutcomeSources([]string{o.ID})
	if err != nil {
		t.Fatal(err)
	}
	d := ReadingDetails{Topic: "kind", Case: "0000000000000001", Work: "0000000000000002", Check: "Keep the condition.", Depth: "brief", Bytes: 100, Reads: 1, Sufficient: true, Articles: []ArticleReference{{ID: "0000000000000003", Revision: "0000000000000004"}}, Evidence: evidence}
	data, _ := json.Marshal(d)
	read := func(kind string) error {
		o, err := b.capture(Observation{Kind: kind, Title: WorkReading.Title(), Content: string(data), Reference: "synthetic execution " + kind, Version: "fixture", ObservedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		source, err := b.OutcomeSources([]string{o.ID})
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.checkedReading(ReadingSample{ReadingDetails: d, Snapshot: source[0]})
		return err
	}
	if err := read("agent"); err != nil {
		t.Fatalf("control: the reading shikii wrote is read: %v", err)
	}
	if err := read("test"); err == nil {
		t.Errorf("an observation of kind test under the title of a reading execution is read as one")
	}
}

// An execution diagnostic is settled automatically only when shikii wrote it (kind agent): an observation of another kind under the
// title of a result is refused.
func TestOnlyAWorkSourceIsSettledAsAnExecutionDiagnostic(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/diagnostic-kind"}
	o, err := b.capture(Observation{Kind: "test", Title: WorkResult.Title(), Content: `{"changed":"Edited."}`, Reference: "synthetic", Version: "fixture", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := b.OutcomeSources([]string{o.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RetainExecutionSources(refs, nil); err == nil || !strings.Contains(err.Error(), "only checked machine execution diagnostics") {
		t.Errorf("an observation of kind test under the title of a result is settled as an execution diagnostic: %v", err)
	}
}
