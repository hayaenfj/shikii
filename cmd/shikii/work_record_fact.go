package main

import (
	"errors"
	"fmt"
)

func resolveWorkFact(f *workFact, w *workState) (*planningFact, error) {
	if f != nil && f.Unknown && (f.Summary != "" || len(f.Quotes) > 0) {
		return nil, errors.New("unknown facts must not include a summary or quotations")
	}
	if f == nil || f.Unknown {
		return &planningFact{Unknown: true}, nil
	}
	p := &planningFact{Summary: f.Summary}
	for _, q := range f.Quotes {
		key := w.Keys[q.Ref]
		if key == "" {
			return nil, fmt.Errorf("unknown prepared quotation reference %q", q.Ref)
		}
		p.Evidence = append(p.Evidence, planningQuote{Source: key, Quote: q.Text})
	}
	return p, nil
}
