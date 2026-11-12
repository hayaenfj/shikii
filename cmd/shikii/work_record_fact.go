package main

import (
	"errors"
	"fmt"
	"strings"
)

func resolveWorkFact(f *workFact, w *workState) (*planningFact, error) {
	if f != nil && f.Unknown && (f.Summary != "" || len(f.Quotes) > 0) {
		return nil, errors.New("unknown facts must not include a summary or quotations")
	}
	if f == nil || f.Unknown {
		return &planningFact{Unknown: true}, nil
	}
	if strings.TrimSpace(f.Summary) == "" || len(f.Quotes) == 0 || len(f.Quotes) > 3 {
		// the shape and the references, which the refusal of the shared check did not name (audit of 0.31.0)
		return nil, fmt.Errorf("agreed and open take {summary, quotes:[{ref, text}]} with 1 to 3 quotes of the person's words (ref one of %s, text copied exactly), or {unknown:true} alone", strings.Join(sortedRefs(w.Keys), ", "))
	}
	p := &planningFact{Summary: f.Summary}
	for _, q := range f.Quotes {
		key := w.Keys[q.Ref]
		if key == "" {
			return nil, fmt.Errorf("unknown prepared quotation reference %q; the references here: %s", q.Ref, strings.Join(sortedRefs(w.Keys), ", "))
		}
		p.Evidence = append(p.Evidence, planningQuote{Source: key, Quote: q.Text})
	}
	return p, nil
}
