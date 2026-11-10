package main

import (
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

type statusLineKey struct{ model, hash string }

// This view belongs to one status request. Agreement is computed before keys
// are separated; minimal and context may use different act thresholds.
type statusLineCounts struct {
	labels    []store.Label
	agreement store.OutcomeReport
	byAct     map[float64]map[statusLineKey]lineState
}

func newStatusLineCounts(labels []store.Label) *statusLineCounts {
	return &statusLineCounts{labels: labels, agreement: store.OutcomeAgreement(labels), byAct: map[float64]map[statusLineKey]lineState{}}
}

func (c *statusLineCounts) state(rs *spec.Spec, minN int, model, hash string, tried bool) lineState {
	counts, ok := c.byAct[rs.Thresholds.Act]
	if !ok {
		groups := map[statusLineKey][]store.Label{}
		for _, l := range store.WithoutUntrustedOutcome(c.labels, c.agreement, rs.Thresholds.Act) {
			key := statusLineKey{l.Model, l.SpecHash}
			groups[key] = append(groups[key], l)
		}
		counts = make(map[statusLineKey]lineState, len(groups))
		for key, labels := range groups {
			s := lineState{Truths: map[string]int{}}
			for _, l := range store.LatestPerInput(labels) {
				if store.HumanSource(l.Source) {
					s.N++
					s.Truths[l.Truth]++
				}
			}
			counts[key] = s
		}
		c.byAct[rs.Thresholds.Act] = counts
	}
	s, ok := counts[statusLineKey{model, hash}]
	if !ok {
		s.Truths = map[string]int{}
	}
	return lineStateWithCounts(rs, minN, model, tried, s)
}
