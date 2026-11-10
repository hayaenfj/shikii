package main

import "github.com/hayaenfj/shikii/internal/store"

// specTruths reads one spec's records and labels at most once for checks that run back to back in one process (at Stop: should a line
// be drawn, should the wordings be scored). Each check used to decode the whole records file, and the whole labels file once per model
// and once more for the outcome agreement. The answers are the ones the store gives: records() is Records(spec, 0), allLabels() is the
// labels of Counts (the records file is scanned too, so its error is still seen), and labelsFor() keeps the lines Labels keeps, in
// file order. A read that failed is not remembered as an answer for the per-model labels: they fall back to the store's own read.
// The slices are shared between callers and must not be changed. forget drops everything once something that may write was started.
type specTruths struct {
	st     store.Store
	specID string

	recordsRead bool
	recs        []store.Record
	recsErr     error

	labelsRead bool
	labels     []store.Label
	labelsErr  error
}

func newSpecTruths(st store.Store, specID string) *specTruths {
	return &specTruths{st: st, specID: specID}
}

func (t *specTruths) records() ([]store.Record, error) {
	if !t.recordsRead {
		t.recs, t.recsErr = t.st.Records(t.specID, 0)
		t.recordsRead = true
	}
	return t.recs, t.recsErr
}

func (t *specTruths) allLabels() ([]store.Label, error) {
	if !t.labelsRead {
		_, t.labels, t.labelsErr = t.st.Counts(t.specID)
		t.labelsRead = true
	}
	return t.labels, t.labelsErr
}

func (t *specTruths) labelsFor(specHash, model string) ([]store.Label, error) {
	all, err := t.allLabels()
	if err != nil {
		return t.st.Labels(t.specID, specHash, model)
	}
	var out []store.Label
	for _, l := range all {
		if l.SpecHash == specHash && l.Model == model {
			out = append(out, l)
		}
	}
	return out, nil
}

func (t *specTruths) forget() {
	*t = specTruths{st: t.st, specID: t.specID}
}
