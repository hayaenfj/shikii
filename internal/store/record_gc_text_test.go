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
