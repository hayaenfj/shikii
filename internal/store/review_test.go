package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestReviewAuditRateFollowsHumanChecks(t *testing.T) {
	reviews, labels := map[string]Review{}, map[string]Label{}
	start := time.Now().Add(-time.Hour)
	for n := 0; n < 20; n++ {
		trace := fmt.Sprintf("%016x", n)
		reviews[trace] = Review{TraceID: trace, InputsHash: trace, Truth: "yes", Sampled: true, At: start.Add(time.Duration(n) * time.Second)}
		labels[trace] = Label{TraceID: trace, Truth: "yes", Source: "audit", At: time.Now()}
	}
	if got := ReviewQuality(reviews, nil); got.Every != 5 || got.Checked != 0 {
		t.Fatal("the amount of AI scoring does not reduce spot checks")
	}
	if got := ReviewQuality(reviews, labels); got.Every != 20 || got.RandomChecked != 20 || got.RandomCorrect != 20 {
		t.Fatalf("lowers the frequency once a person's random checks are stable: %+v", got)
	}
	reviews["unknown"] = Review{TraceID: "unknown", InputsHash: "unknown", Truth: "unknown", Sampled: true, At: start}
	labels["unknown"] = Label{TraceID: "unknown", Truth: "yes", Source: "audit", At: time.Now()}
	if got := ReviewQuality(reviews, labels); got.Uncertain != 1 || got.UncertainChecked != 1 || got.Checked != 20 || got.RandomChecked != 20 {
		t.Fatalf("keeps held checks apart from scoring right/wrong: %+v", got)
	}
	delete(reviews, "unknown")
	delete(labels, "unknown")
	reviews["focused"] = Review{TraceID: "focused", InputsHash: "focused", Truth: "yes", At: start}
	labels["focused"] = Label{TraceID: "focused", Truth: "no", Source: "ui", At: time.Now()}
	if got := ReviewQuality(reviews, labels); got.Every != 5 || got.RandomChecked != 20 || got.RandomCorrect != 20 {
		t.Fatalf("raises the frequency on a mismatch even in a non-random check, and keeps the random results: %+v", got)
	}
	delete(reviews, "focused")
	delete(labels, "focused")
	l := labels["0000000000000001"]
	l.Truth = "no"
	labels[l.TraceID] = l
	if got := ReviewQuality(reviews, labels); got.Every != 5 || got.RandomCorrect != 19 {
		t.Fatalf("kinds that had a mismatch get more checks: %+v", got)
	}
	// even many checks of deliberately chosen hard cases do not replace the random slot's results.
	for n := 20; n < 50; n++ {
		trace := fmt.Sprintf("%016x", n)
		reviews[trace] = Review{TraceID: trace, InputsHash: trace, Truth: "yes", At: time.Now()}
		labels[trace] = Label{TraceID: trace, Truth: "yes", Source: "ui", At: time.Now()}
	}
	if got := ReviewQuality(reviews, labels); got.Every != 5 || got.RandomChecked != 20 {
		t.Fatalf("does not mix how checks are chosen: %+v", got)
	}
	for n := 0; n < 200; n++ {
		trace := fmt.Sprintf("%016x", n)
		if ReviewSample(trace, 5) != ReviewSample(trace, 5) {
			t.Fatal("the spot check for the same decision does not change")
		}
		if ReviewSample(trace, 20) && !ReviewSample(trace, 5) {
			t.Fatal("raising the frequency still includes the original spot checks")
		}
	}
}

func TestReviewKeepsFirstScoreAndHumanTruth(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	r := Review{TraceID: "0000000000000001", Spec: "done-as-asked", Scope: "/work/a", InputsHash: "same-input", Truth: "no", Reason: "the requested steps are unfinished", Source: Feedback{ID: "0000000000000002", Request: "update the steps", Summary: "changed it", Reply: "something is missing"}}
	dir := filepath.Join(st.Dir, "records", "reviews")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	guard, err := os.OpenFile(filepath.Join(dir, r.Spec+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := syscall.Flock(int(guard.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
	finished := make(chan error, 1)
	go func() { _, err := st.SaveReview(r); finished <- err }()
	select {
	case <-finished:
		t.Fatal("while another score is being saved, an update to the same history waits")
	case <-time.After(75 * time.Millisecond):
	}
	_ = syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.SaveReview(r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	path := filepath.Join(st.Dir, "records", "reviews", r.Spec+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, c := range data {
		if c == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("parallel saves of the same provisional score happen only once: %d", lines)
	}
	r.Truth = "yes"
	if _, err := st.SaveReview(r); err == nil {
		t.Fatal("the AI cannot later overwrite scores to improve its results")
	}
	human := Label{TraceID: r.TraceID, SpecID: r.Spec, SpecHash: "test-hash", Model: "test-model", Truth: "yes", Source: "audit", At: time.Now()}
	machine := human
	machine.Truth, machine.Source, machine.At = "no", "judge", time.Now().Add(time.Second)
	if err := st.AppendLabel(human); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(machine); err != nil {
		t.Fatal(err)
	}
	latest, err := st.LabeledTraces(r.Spec)
	if err != nil || latest[r.TraceID].Source != "audit" {
		t.Fatal("a person's answer takes priority over a later AI label")
	}
}
