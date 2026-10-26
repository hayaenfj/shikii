package store

import (
	"sync"
	"testing"
	"time"
)

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
