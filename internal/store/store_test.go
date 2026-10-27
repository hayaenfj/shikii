package store

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func rec(specHash, model string) Record {
	return Record{
		TraceID: NewTraceID(), SpecID: "s", SpecHash: specHash, Type: "noul", Model: model,
		InputsHash: HashInputs(map[string]string{"a": "x"}),
		Raw:        map[string]float64{"yes": 0.8, "no": 0.2}, Top: "yes", P: 0.8, Band: "ask", At: time.Now(),
	}
}

// InputsByHash maps input hash → text from records that still have their text (material for re-asking).
func TestInputsByHashOnlyFromRecordsThatKeptText(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	withText := rec("h1", "m")
	withText.Inputs = map[string]string{"a": "x"}
	withoutText := rec("h1", "m")
	withoutText.InputsHash = HashInputs(map[string]string{"a": "y"})
	for _, r := range []Record{withText, withoutText} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.InputsByHash("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[withText.InputsHash].Inputs["a"] != "x" {
		t.Fatalf("want only the record that kept its text, got %v", got)
	}
	// labels calibrated from a file have no record. If the label side has the text, that is material too
	fromFile := Label{TraceID: NewTraceID(), SpecID: "s", SpecHash: "h1", Type: "noul", Model: "m",
		InputsHash: HashInputs(map[string]string{"a": "z"}), Inputs: map[string]string{"a": "z"},
		Raw: map[string]float64{"yes": 0.7, "no": 0.3}, Truth: "yes", Weight: 1, Source: "file", At: time.Now()}
	if err := st.AppendLabel(fromFile); err != nil {
		t.Fatal(err)
	}
	got, _ = st.InputsByHash("s")
	if len(got) != 2 || got[fromFile.InputsHash].Inputs["a"] != "z" {
		t.Fatalf("labels that carry text must count too, got %v", got)
	}
}

// Records returns limit records newest first. LabeledTraces is the set of labeled traces (used for the queue's "done" check).
func TestRecentRecordsAndLabeledTraces(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	var recs []Record
	for i := 0; i < 3; i++ {
		r := rec("h1", "m")
		r.At = time.Now().Add(time.Duration(i) * time.Second)
		recs = append(recs, r)
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Records("s", 2)
	if err != nil || len(got) != 2 || got[0].TraceID != recs[2].TraceID || got[1].TraceID != recs[1].TraceID {
		t.Fatalf("want the 2 newest first, got %v %v", got, err)
	}
	l, _ := LabelFor(&recs[1], "yes", 1, "ui")
	if err := st.AppendLabel(l); err != nil {
		t.Fatal(err)
	}
	labeled, err := st.LabeledTraces("s")
	if err != nil || len(labeled) != 1 || labeled[recs[1].TraceID].Truth != "yes" {
		t.Fatalf("labeled traces: %v %v", labeled, err)
	}
	if none, err := st.Records("no-such", 5); err != nil || len(none) != 0 {
		t.Fatalf("missing spec must be empty, got %v %v", none, err)
	}
}

// audit S2: the writer had no limit while the reader had a 1 MiB limit, so one huge line made the whole store unreadable.
// The writer rejects it, and the reader has headroom.
func TestOversizedRecordIsRejectedNotWritten(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	r := rec("h1", "m")
	r.Inputs = map[string]string{"a": strings.Repeat("x", MaxLineBytes)}
	if err := st.AppendRecord(r); err == nil {
		t.Fatal("a record larger than MaxLineBytes must be rejected")
	}
	ok := rec("h1", "m")
	ok.Inputs = map[string]string{"a": strings.Repeat("x", 1<<20+100)} // a little over 1 MiB (still readable)
	if err := st.AppendRecord(ok); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Records("s", 0); err != nil || len(got) != 1 {
		t.Fatalf("a record just above 1 MiB must still be readable: %d %v", len(got), err)
	}
}

// audit S9: a store holding text is readable only by its owner (0600 / 0700).
func TestStoreFilesAreOwnerOnly(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "s")}
	if err := st.AppendRecord(rec("h1", "m")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(st.recordsPath("s"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("record file must be 0600, got %o", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Join(st.Dir, "records"))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("store dirs must be 0700, got %o", di.Mode().Perm())
	}
}

func TestHashInputsIsOrderIndependent(t *testing.T) {
	a := HashInputs(map[string]string{"x": "1", "y": "2"})
	b := HashInputs(map[string]string{"y": "2", "x": "1"})
	c := HashInputs(map[string]string{"x": "1", "y": "3"})
	if a != b || a == c {
		t.Fatalf("hash: a=%s b=%s c=%s", a, b, c)
	}
}

func TestRecordRoundTripAndLookup(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	r1, r2 := rec("h1", "m"), rec("h1", "m")
	for _, r := range []Record{r1, r2} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.FindRecord("", r2.TraceID)
	if err != nil || got.TraceID != r2.TraceID || got.Raw["yes"] != 0.8 {
		t.Fatalf("find: %v %+v", err, got)
	}
	if _, err := st.FindRecord("s", "nope"); err == nil {
		t.Fatal("unknown trace must be an error")
	}
}

func TestLabelForValidatesTruthByType(t *testing.T) {
	r := rec("h1", "m")
	if _, err := LabelFor(&r, "maybe", 1, "ask"); err == nil {
		t.Fatal("noul truth other than yes/no must be rejected")
	}
	l, err := LabelFor(&r, "no", 0, "")
	if err != nil || l.Truth != "no" || l.Weight != 1 || l.Source != "manual" || l.SpecHash != "h1" || l.Model != "m" {
		t.Fatalf("label: %v %+v", err, l)
	}

	ch := r
	ch.Type = "choice"
	ch.Raw = map[string]float64{"change_request": 1, "question": 0}
	if _, err := LabelFor(&ch, "operation", 1, "ask"); err == nil {
		t.Fatal("choice truth must be one of the recorded options")
	}
	if _, err := LabelFor(&ch, "question", 1, "ask"); err != nil {
		t.Fatal(err)
	}
	// partial label: only accepts the negation of a known option
	if l, err := LabelFor(&ch, "!change_request", 0.7, "outcome"); err != nil || l.Truth != "!change_request" || l.Weight != 0.7 {
		t.Fatalf("partial label must be accepted: %v %+v", err, l)
	}
	if _, err := LabelFor(&ch, "!operation", 1, "outcome"); err == nil {
		t.Fatal("partial label of an unknown option must be rejected")
	}
	if _, err := LabelFor(&ch, "!", 1, "outcome"); err == nil {
		t.Fatal("bare '!' must be rejected")
	}
	if !Correct("question", "!change_request") || Correct("change_request", "!change_request") || !Correct("a", "a") || Correct("a", "b") {
		t.Fatal("Correct must treat !x as 'anything but x'")
	}

	sc := r
	sc.Type = "score"
	sc.Raw = map[string]float64{"0": 0.5, "1": 0.5}
	if _, err := LabelFor(&sc, "2", 1, "ask"); err == nil {
		t.Fatal("score truth must be a recorded level index")
	}
}

// Labels used for calibration are only those whose (spec_hash, model) match.
func TestLabelsFilterByHashAndModel(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	add := func(hash, model, truth string) {
		r := rec(hash, model)
		l, err := LabelFor(&r, truth, 1, "ask")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	add("h1", "jev-1.13.0", "yes")
	add("h1", "jev-1.13.0", "no")
	add("stale", "jev-1.13.0", "yes") // a label from before the question changed
	add("h1", "jev-1.14.0", "yes")    // a label from another version

	got, err := st.Labels("s", "h1", "jev-1.13.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want exactly the 2 labels for (h1, jev-1.13.0), got %d: %+v", len(got), got)
	}
	none, err := st.Labels("s", "h9", "jev-1.13.0")
	if err != nil || len(none) != 0 {
		t.Fatalf("no labels for unknown hash, got %v %v", none, err)
	}
	// a missing file is not an error, just empty
	if l, err := st.Labels("other-spec", "h1", "m"); err != nil || len(l) != 0 {
		t.Fatalf("missing label file must be empty, got %v %v", l, err)
	}
}

// GC removes only "old records without labels". Labeled ones stay regardless of age, and labels themselves are untouched.
// Unreadable lines are not removed (unreadable things are not thrown away). By default it only shows; it rewrites only with apply.
func TestGCRemovesOnlyOldUnlabeledRecords(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	now := time.Now()
	oldUnlabeled, oldLabeled, fresh := rec("h1", "m"), rec("h1", "m"), rec("h1", "m")
	oldUnlabeled.At = now.Add(-40 * 24 * time.Hour)
	oldLabeled.At = now.Add(-40 * 24 * time.Hour)
	fresh.At = now.Add(-1 * time.Hour)
	for _, r := range []Record{oldUnlabeled, oldLabeled, fresh} {
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
	}
	l, err := LabelFor(&oldLabeled, "yes", 1, "ask")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(l); err != nil {
		t.Fatal(err)
	}
	// mix in one broken line
	f, err := os.OpenFile(st.recordsPath("s"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cutoff := now.Add(-30 * 24 * time.Hour)
	dry, err := st.GC("s", cutoff, false)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Removed != 1 || dry.KeptLabeled != 1 || dry.KeptRecent != 1 || dry.KeptUnparsed != 1 {
		t.Fatalf("dry run counts: %+v", dry)
	}
	if n, _, _ := st.Counts("s"); n != 4 {
		t.Fatalf("dry run must not touch the file, records=%d", n)
	}

	got, err := st.GC("s", cutoff, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 1 {
		t.Fatalf("apply counts: %+v", got)
	}
	if n, labs, _ := st.Counts("s"); n != 3 || len(labs) != 1 {
		t.Fatalf("after apply: records=%d labels=%d (want 3 and 1)", n, len(labs))
	}
	if _, err := st.FindRecord("s", oldUnlabeled.TraceID); err == nil {
		t.Fatal("old unlabeled record must be gone")
	}
	for _, keep := range []Record{oldLabeled, fresh} {
		if _, err := st.FindRecord("s", keep.TraceID); err != nil {
			t.Fatalf("record %s must survive: %v", keep.TraceID, err)
		}
	}
	b, _ := os.ReadFile(st.recordsPath("s"))
	if !strings.Contains(string(b), "{not json") {
		t.Fatal("unparsable line must be kept, not silently dropped")
	}
	// the second run removes nothing (idempotent)
	again, err := st.GC("s", cutoff, true)
	if err != nil || again.Removed != 0 {
		t.Fatalf("second gc must remove nothing: %+v %v", again, err)
	}
	// a missing spec is not an error, just 0
	none, err := st.GC("no-such-spec", cutoff, true)
	if err != nil || none.Removed != 0 {
		t.Fatalf("missing spec must be a no-op: %+v %v", none, err)
	}
}

func TestAppendLabelRequiresKeys(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if err := st.AppendLabel(Label{TraceID: "t", SpecID: "s", SpecHash: "h"}); err == nil {
		t.Fatal("label without model must be rejected; it could not be keyed for calibration")
	}
}

// Spot check: 1 in 20 of the decisions that acted automatically is shown to a person. It is decided by trace_id, so the same decision always gives the same answer.
func TestAuditSampleIsOneInTwentyAndStable(t *testing.T) {
	// Fixed ids, so the count is exact on every run: fnv-1a (32 bits) of the id is a multiple of 20 for 218 of these 4000 ids.
	n := 0
	for i := 0; i < 4000; i++ {
		id := fmt.Sprintf("%016x", i)
		h := fnv.New32a()
		h.Write([]byte(id))
		if want := h.Sum32()%20 == 0; AuditSample(id) != want {
			t.Fatalf("AuditSample(%q) = %v, want %v (fnv-1a of the id modulo 20 is 0)", id, !want, want)
		}
		if AuditSample(id) {
			n++
		}
	}
	if n != 218 {
		t.Fatalf("218 of the 4000 fixed ids are sampled (about 1 in 20), got %d", n)
	}
	if AuditSample("") {
		t.Fatalf("an empty trace is never sampled")
	}
}

// Separate people's truths from machine truths: the only machine-decided truth is the upper step's answer (context). Truths from re-asking (replay) and files (file) were given by people.
func TestHumanSourceExcludesMachineTruths(t *testing.T) {
	for _, s := range []string{"ui", "manual", "ask", "audit", "mcp", "outcome", "replay", "file", ""} {
		if !HumanSource(s) {
			t.Fatalf("%q is a truth a person (or the outcome) decided", s)
		}
	}
	for _, s := range []string{"context", "judge"} {
		if HumanSource(s) {
			t.Fatalf("%q is a machine truth (the model's own answer, or another model's)", s)
		}
	}
}

// audit round 4, item 2: if one input has both a person's truth and a machine truth (judge / context), the person's wins regardless of age. Between people's truths, the newer one.
func TestLatestPerInputPrefersHumanTruths(t *testing.T) {
	in := HashInputs(map[string]string{"a": "x"})
	t0 := time.Now()
	human := Label{InputsHash: in, Truth: "yes", Source: "ui", At: t0}
	judge := Label{InputsHash: in, Truth: "no", Source: "judge", At: t0.Add(time.Hour)}
	if got := LatestPerInput([]Label{human, judge}); len(got) != 1 || got[0].Source != "ui" {
		t.Fatalf("a later machine truth must not push out a human truth: %+v", got)
	}
	if got := LatestPerInput([]Label{judge, human}); len(got) != 1 || got[0].Source != "ui" {
		t.Fatalf("order of appearance must not matter: %+v", got)
	}
	fix := Label{InputsHash: in, Truth: "no", Source: "ui", At: t0.Add(2 * time.Hour)}
	if got := LatestPerInput([]Label{human, fix}); len(got) != 1 || got[0].Truth != "no" {
		t.Fatalf("a later human correction wins over an earlier human truth: %+v", got)
	}
	ctx := Label{InputsHash: in, Truth: "no", Source: "context", At: t0.Add(3 * time.Hour)}
	if got := LatestPerInput([]Label{ctx}); len(got) != 1 || got[0].Source != "context" {
		t.Fatalf("with no human truth the machine truth stands: %+v", got)
	}
}

// Writing the first records concurrently from the same process into a location with no records/ yet loses none of them (the hook asks 2
// specs in parallel, and wiki insertion points are asked in parallel per article). When two goroutines created the same new file at once,
// darwin's open with create returned ENOENT (2026-10-23) and one record was lost. Checked in one run, without waiting on a race: the open
// of the new record file says ENOENT once, as it did then, and the record is still written.
func TestFirstRecordSurvivesACreateThatSaysNotThere(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	old := openInRoot
	t.Cleanup(func() { openInRoot = old })
	var opens []int
	openInRoot = func(r *os.Root, name string, flag int, perm os.FileMode) (*os.File, error) {
		opens = append(opens, flag)
		if len(opens) == 1 {
			return nil, &os.PathError{Op: "openat", Path: name, Err: syscall.ENOENT}
		}
		return old(r, name, flag, perm)
	}
	if err := st.AppendRecord(Record{TraceID: "t0", SpecID: "spec-0"}); err != nil {
		t.Fatalf("the first record is written even when the create says ENOENT once: %v", err)
	}
	if len(opens) != 2 || opens[0]&os.O_CREATE == 0 {
		t.Fatalf("one create that says ENOENT, then one that works: %v", opens)
	}
	openInRoot = old
	if recs, err := st.Records("spec-0", 0); err != nil || len(recs) != 1 || recs[0].TraceID != "t0" {
		t.Fatalf("the record is there: %v %v", recs, err)
	}
}

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

// 0.9.3: an outcome truth is written once per (trace, truth, source) even when two hooks race past the session lock (the original
// session's hook gives up the lock after 2 seconds) or a save failure makes a hook try again. A different truth for the same trace
// (a kept change later reverted) is still written.
func TestAppendLabelsOnceWritesAnOutcomeTruthOnce(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	l := Label{TraceID: "0000000000000001", SpecID: "done-as-asked", SpecHash: "h", Model: "m", Truth: "yes", Weight: 0.5, Source: "outcome", At: time.Now()}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.AppendLabelsOnce([]Label{l}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := st.AppendLabelsOnce([]Label{l}); err != nil {
		t.Fatal(err)
	}
	other := l
	other.Truth, other.Weight = "no", 1
	if err := st.AppendLabelsOnce([]Label{other}); err != nil {
		t.Fatal(err)
	}
	n, truths := 0, map[string]int{}
	if err := st.scanJSONL(st.labelsPath("done-as-asked"), func(line []byte) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	all, _ := st.Labels("done-as-asked", "h", "m")
	for _, x := range all {
		truths[x.Truth]++
	}
	if n != 2 || truths["yes"] != 1 || truths["no"] != 1 {
		t.Fatalf("the same outcome truth is written once and a different one still is: %d lines %v", n, truths)
	}
}
