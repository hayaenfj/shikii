package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func statusLabelsFixture(n, keys int) []store.Label {
	labs := make([]store.Label, n)
	for i := range labs {
		labs[i] = store.Label{TraceID: fmt.Sprintf("%016x", i), Model: fmt.Sprint(i % keys), SpecHash: "synthetic", InputsHash: fmt.Sprint(i), Source: "ui", Truth: "yes", At: time.Unix(int64(i), 0)}
	}
	return labs
}
func TestStatusLineCountsMatchLegacy(t *testing.T) {
	labs := statusLabelsFixture(120, 3)
	for i := 0; i < 60; i++ {
		labs[i].Source = "outcome"
		labs[i].Truth = "no"
	}
	for i := 60; i < 120; i++ {
		labs[i].TraceID = labs[i-60].TraceID
		labs[i].Truth = "yes"
		labs[i].InputsHash = labs[i-60].InputsHash
	}
	labs = append(labs, store.Label{Model: "2", SpecHash: "synthetic", Source: "replay", InputsHash: "1", Truth: "no"}, store.Label{Model: "1", SpecHash: "other", Source: "ui", Truth: "yes"})
	// Outcome checks in another model still govern unchecked outcomes in this model.
	for i := range 200 {
		l := store.Label{TraceID: fmt.Sprintf("global-%d", i), Model: "isolated", SpecHash: "synthetic", InputsHash: fmt.Sprintf("global-%d", i), Source: "outcome", Truth: "yes", At: time.Unix(1, 0)}
		labs = append(labs, l)
		if i < 100 {
			l.Model = "checks"
			l.Source = "ui"
			l.At = time.Unix(2, 0)
			if i < 20 {
				l.Truth = "no"
			}
			labs = append(labs, l)
		}
	}
	c := newStatusLineCounts(labs)
	for _, act := range []float64{.5, .95, .5} {
		for _, kind := range []spec.Type{spec.Noul, spec.Choice} {
			sp := &spec.Spec{Type: kind}
			sp.Thresholds.Act = act
			for _, key := range []string{"0", "1", "2", "isolated", "checks", "missing"} {
				for _, hash := range []string{"synthetic", "other"} {
					for _, tried := range []bool{false, true} {
						got := c.state(sp, 10, key, hash, tried)
						want := lineStateOf(sp, 10, key, hash, labs, tried)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("\u96c6\u8a08\u306e\u610f\u5473\u304c\u7570\u306a\u308b: act=%v key=%s got=%+v want=%+v", act, key, got, want)
						}
					}
				}
			}
		}
	}
}

func BenchmarkStatusLineCounts(b *testing.B) {
	labs := statusLabelsFixture(5000, 50)
	sp := &spec.Spec{Type: spec.Noul}
	sp.Thresholds.Act = .75
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c := newStatusLineCounts(labs)
		for key := range 50 {
			c.state(sp, 30, fmt.Sprint(key), "synthetic", false)
		}
	}
}
