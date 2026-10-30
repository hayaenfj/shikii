package store

import "time"

type KnowledgeReference struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}
type KnowledgeCitation struct {
	Source string `json:"source"`
	Quote  string `json:"quote"`
}

// KnowledgeLink records the article that led to a related criterion, separately from search matches.
type KnowledgeLink struct {
	From     string `json:"from"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// KnowledgeContext records retrieval and quotation, not the usefulness of a memory.
// Available fixes the permitted article versions for later comparisons.
type KnowledgeContext struct {
	Query              string               `json:"query"`
	Candidates         []KnowledgeReference `json:"candidates"`
	Related            []KnowledgeLink      `json:"related,omitempty"`
	Read               []KnowledgeReference `json:"read"`
	Used               []KnowledgeCitation  `json:"used"`
	Available          []KnowledgeReference `json:"available"`
	At                 time.Time            `json:"at"`
	LookupMilliseconds float64              `json:"lookup_milliseconds"`
	ContextBytes       int                  `json:"context_bytes"`
}

type KnowledgeStats struct {
	Window             int     `json:"window"`
	Prepared           int     `json:"prepared"`
	WithMatches        int     `json:"with_matches"`
	WithCitations      int     `json:"with_citations"`
	Citations          int     `json:"citations"`
	ContextBytes       int     `json:"context_bytes"`
	LookupMilliseconds float64 `json:"lookup_milliseconds"`
}

func (s Store) KnowledgeReport() (KnowledgeStats, error) {
	r := KnowledgeStats{Window: 256}
	records, err := s.Records("needs-plan", r.Window)
	if err != nil {
		return r, err
	}
	for _, rec := range records {
		k := rec.KnowledgeContext
		if k == nil {
			continue
		}
		r.Prepared++
		if len(k.Candidates) > 0 {
			r.WithMatches++
		}
		if len(k.Used) > 0 {
			r.WithCitations++
		}
		r.Citations += len(k.Used)
		r.ContextBytes += k.ContextBytes
		r.LookupMilliseconds += k.LookupMilliseconds
	}
	return r, nil
}
