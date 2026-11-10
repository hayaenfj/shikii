package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// gc --apply removes an old unlabeled decision. The original text kept for that decision
// (records/text/<trace_id>.json, up to 64 KB per field) must go with it: nothing reads it once the record is gone,
// and the person asked for the old decision to be removed. Text of kept decisions stays readable.
func TestGCRemovesTheOriginalTextOfRemovedRecords(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	record := func(trace string, at time.Time) Record {
		inputs := map[string]string{"request": "short opening"}
		return Record{TraceID: trace, SpecID: "prompt-kind", SpecHash: "h", Type: "noul", Model: "m",
			Inputs: inputs, InputsHash: HashInputs(inputs), Raw: map[string]float64{"yes": 0.6, "no": 0.4}, P: 0.6, Band: "ask", At: at}
	}
	removed := record("aaaaaaaaaaaaaaaa", old)
	labeled := record("bbbbbbbbbbbbbbbb", old)
	recent := record("cccccccccccccccc", now)
	for _, r := range []Record{removed, labeled, recent} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveRecordText(r, map[string]string{"request": "short opening and the long original a person typed"}); err != nil {
			t.Fatal(err)
		}
	}
	l, err := LabelFor(&labeled, "yes", 1, "ui")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(l); err != nil {
		t.Fatal(err)
	}
	textPath := func(r Record) string { return filepath.Join(st.Dir, "records", "text", r.TraceID+".json") }
	exists := func(r Record) bool { _, err := os.Lstat(textPath(r)); return err == nil }
	cutoff := now.Add(-24 * time.Hour)

	// a dry run counts and removes nothing, the text included
	res, err := st.GC("prompt-kind", cutoff, false)
	if err != nil || res.Removed != 1 {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	if !exists(removed) {
		t.Fatal("a dry run removed the original text")
	}

	res, err = st.GC("prompt-kind", cutoff, true)
	if err != nil || res.Removed != 1 || res.KeptLabeled != 1 || res.KeptRecent != 1 {
		t.Fatalf("gc: %+v %v", res, err)
	}
	if _, err := st.FindRecord("prompt-kind", removed.TraceID); err == nil {
		t.Fatal("the old unlabeled record must be removed")
	}
	if exists(removed) {
		data, _ := os.ReadFile(textPath(removed))
		t.Fatalf("gc removed the decision but left its original text on disk: %s", data)
	}
	for _, kept := range []Record{labeled, recent} {
		if originals, _ := st.RecordText(kept); originals["request"] != "short opening and the long original a person typed" {
			t.Fatalf("the original text of kept decision %s must stay readable: %v", kept.TraceID, originals)
		}
	}
}

func textRecord(spec, trace string, at time.Time) Record {
	inputs := map[string]string{"request": "short opening for " + spec}
	return Record{TraceID: trace, SpecID: spec, SpecHash: "h", Type: "noul", Model: "m",
		Inputs: inputs, InputsHash: HashInputs(inputs), Raw: map[string]float64{"yes": 0.6, "no": 0.4}, P: 0.6, Band: "ask", At: at}
}

// A trace with an old line and a recent line: the recent line stays, so its original text stays.
func TestGCKeepsTheTextWhenAnotherLineOfTheSameTraceIsKept(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	now := time.Now()
	old, recent := textRecord("prompt-kind", "aaaaaaaaaaaaaaaa", now.Add(-48*time.Hour)), textRecord("prompt-kind", "aaaaaaaaaaaaaaaa", now)
	for _, r := range []Record{old, recent} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	long := recent.Inputs["request"] + " and the long original a person typed"
	if err := st.SaveRecordText(recent, map[string]string{"request": long}); err != nil {
		t.Fatal(err)
	}
	res, err := st.GC("prompt-kind", now.Add(-24*time.Hour), true)
	if err != nil || res.Removed != 1 || res.KeptRecent != 1 {
		t.Fatalf("gc: %+v %v", res, err)
	}
	if got, _ := st.RecordText(recent); got["request"] != long {
		t.Fatalf("the kept line lost its original text: %v", got)
	}
}

// The text folder is shared by every question. A decision of another question under the same trace keeps its text when the
// first question's old decision is removed; the removed decision's own text still goes.
func TestGCKeepsTheTextOfAnotherQuestionsDecisionWithTheSameTrace(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	now := time.Now()
	gone := textRecord("prompt-kind", "aaaaaaaaaaaaaaaa", now.Add(-48*time.Hour))
	kept := textRecord("needs-plan", "aaaaaaaaaaaaaaaa", now)
	own := textRecord("prompt-kind", "bbbbbbbbbbbbbbbb", now.Add(-48*time.Hour))
	for _, r := range []Record{gone, kept, own} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	long := kept.Inputs["request"] + " and the long original a person typed"
	if err := st.SaveRecordText(kept, map[string]string{"request": long}); err != nil {
		t.Fatal(err)
	}
	ownLong := own.Inputs["request"] + " and its own long original"
	if err := st.SaveRecordText(own, map[string]string{"request": ownLong}); err != nil {
		t.Fatal(err)
	}
	if res, err := st.GC("prompt-kind", now.Add(-24*time.Hour), true); err != nil || res.Removed != 2 {
		t.Fatalf("gc: %+v %v", res, err)
	}
	if got, _ := st.RecordText(kept); got["request"] != long {
		t.Errorf("gc of prompt-kind removed the text of the kept needs-plan decision with the same trace: %v", got)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "records", "text", "bbbbbbbbbbbbbbbb.json")); !os.IsNotExist(err) {
		t.Errorf("the removed decision's own text is removed with it: %v", err)
	}
}
