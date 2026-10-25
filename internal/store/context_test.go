package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// When a person answers a record decided at the context step, the truth goes to both steps: the final answer (context hash, context raw) and the minimal question (its hash, its raw).
func TestLabelsForReachBothRungs(t *testing.T) {
	r := rec("ctx-hash", "m")
	r.Type, r.Rung = "choice", "context"
	r.Raw = map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02}
	r.Top, r.P, r.Band = "change_request", 0.9, "act"
	r.Minimal = &Pass{SpecHash: "min-hash", Raw: map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}, Top: "change_request", P: 0.65, Band: "ask"}
	labels, err := LabelsFor(&r, "question", 1, "ui")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 {
		t.Fatalf("a context decision yields a truth for each rung, got %d", len(labels))
	}
	final, minimal := labels[0], labels[1]
	if final.SpecHash != "ctx-hash" || final.Raw["change_request"] != 0.9 || final.Rung != "context" || final.Truth != "question" {
		t.Fatalf("first label is the final answer under the context hash: %+v", final)
	}
	if minimal.SpecHash != "min-hash" || minimal.Raw["change_request"] != 0.65 || minimal.Rung != "minimal" || minimal.Truth != "question" || minimal.TraceID != r.TraceID || minimal.InputsHash != r.InputsHash {
		t.Fatalf("second label teaches the minimal rung the same truth: %+v", minimal)
	}
	// a record with only the minimal step gets one
	plain := rec("min-hash", "m")
	if ls, err := LabelsFor(&plain, "yes", 1, "ui"); err != nil || len(ls) != 1 || ls[0].Rung != "minimal" {
		t.Fatalf("a minimal-only record yields one label: %v %+v", err, ls)
	}
}

// Counts per step: decided by minimal / decided by context / sent to a person. "Sent to a person" is the same rule as decide's "unsure":
// ask always, and discard only for types whose band comes from the highest option (choice, score without level) (audit round 3, item 4).
func TestFunnelCountsWhereDecisionsSettle(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "s")}
	add := func(rung, band string) {
		r := rec("h", "m")
		r.Rung, r.Band, r.At = rung, band, time.Now()
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	add("", "act") // an old record (no rung) is the minimal step
	add("minimal", "discard")
	add("minimal", "ask")
	add("context", "act")
	add("context", "ask")
	add("context", "discard")
	// noul: discard is a confident "no" = decided
	f, err := st.Funnel("s", false)
	if err != nil {
		t.Fatal(err)
	}
	if f.Records != 6 || f.Minimal != 2 || f.Context != 2 || f.Human != 2 {
		t.Fatalf("noul funnel: %+v", f)
	}
	// choice: discard is unsure too = to a person (README's "the tests … 0.45 → to a person" is this case)
	f, _ = st.Funnel("s", true)
	if f.Minimal != 1 || f.Context != 1 || f.Human != 4 {
		t.Fatalf("choice funnel: %+v", f)
	}
	if f.HumanPerThousand() != 667 {
		t.Fatalf("4 of 6 go to a person = 667 per 1000, got %v", f.HumanPerThousand())
	}
	if empty, _ := st.Funnel("none", true); empty.Records != 0 || empty.HumanPerThousand() != 0 {
		t.Fatalf("no records: zero, not a division error: %+v", empty)
	}
}

// audit 6: the 2 labels from one answer are appended in one write (a failure midway does not leave only one).
func TestAppendLabelsWritesAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	st := Store{Dir: filepath.Join(dir, "s")}
	r := rec("h", "m")
	a, b := LabelFor(&r, "yes", 1, "ui")
	if b != nil {
		t.Fatal(b)
	}
	m := a
	m.SpecHash = "h2"
	if err := st.AppendLabels([]Label{a, m}); err != nil {
		t.Fatal(err)
	}
	if ls, _ := st.Labels("s", "h", "m"); len(ls) != 1 {
		t.Fatalf("both labels land: %+v", ls)
	}
	if ls, _ := st.Labels("s", "h2", "m"); len(ls) != 1 {
		t.Fatalf("both labels land: %+v", ls)
	}
	// a location that cannot be written: nothing is left
	bad := Store{Dir: filepath.Join(dir, "file")}
	if err := os.WriteFile(bad.Dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bad.AppendLabels([]Label{a, m}); err == nil {
		t.Fatalf("an unwritable store must fail")
	}
}
