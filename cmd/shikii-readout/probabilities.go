package main

import (
	"context"
	"fmt"
)

// readProbsFor picks the probability for each key (a letter, or in the plain layout the first token of an option id) from the top list and normalizes them.
func readProbsFor(top []tokenProb, keys []string) (probs []float64, missing int) {
	byTok := map[string]float64{}
	for _, t := range top {
		if _, seen := byTok[t.Token]; !seen {
			byTok[t.Token] = t.Prob
		}
	}
	probs = make([]float64, len(keys))
	var sum float64
	for i, k := range keys {
		p, ok := byTok[k]
		if !ok || p <= 0 {
			p = probFloor
			missing++
		}
		probs[i] = p
		sum += p
	}
	for i := range probs {
		probs[i] /= sum
	}
	return probs, missing
}

// optionKeys returns the tokens to read for the layout: letters (A, B, …) by default; for plain, the first token of each id (looked up with /tokenize and remembered).
// If two ids share the same first token they cannot be told apart, so it is an error.
func (s *server) optionKeys(ctx context.Context, q question, layout string) ([]string, error) {
	if layout != "plain" {
		return labels[:len(q.Options)], nil
	}
	keys := make([]string, len(q.Options))
	seen := map[string]string{}
	for i, o := range q.Options {
		piece, err := s.firstPiece(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		if other, dup := seen[piece]; dup {
			return nil, fmt.Errorf("plain layout: options %q and %q share the first token %q and cannot be told apart; use letters (state-last) or rename an option", other, o.ID, piece)
		}
		seen[piece] = o.ID
		keys[i] = piece
	}
	return keys, nil
}

// firstPiece is the text of a string's first token (/tokenize with_pieces).
func (s *server) firstPiece(ctx context.Context, text string) (string, error) {
	s.mu.Lock()
	if p, ok := s.pieces[text]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()
	var out struct {
		Tokens []struct {
			ID    int    `json:"id"`
			Piece string `json:"piece"`
		} `json:"tokens"`
	}
	if _, err := s.post(ctx, "/tokenize", map[string]any{"content": text, "with_pieces": true}, &out); err != nil {
		return "", err
	}
	if len(out.Tokens) == 0 || out.Tokens[0].Piece == "" {
		return "", fmt.Errorf("/tokenize returned no pieces for %q (with_pieces unsupported?)", text)
	}
	s.mu.Lock()
	if s.pieces == nil {
		s.pieces = map[string]string{}
	}
	s.pieces[text] = out.Tokens[0].Piece
	s.mu.Unlock()
	return out.Tokens[0].Piece, nil
}

// requestLayout picks the request's layout: if empty, the startup default; an unknown name is an error.
func (s *server) requestLayout(req string) (string, error) {
	if req == "" {
		return s.cfg.layout(), nil
	}
	if !layouts[req] {
		return "", fmt.Errorf("unknown layout %q (one of state-last, state-first, plain)", req)
	}
	return req, nil
}

// responseModel is the response's model name: as configured for the default layout, otherwise the base name + "+" + the layout (so calibration keys stay separate).
func (s *server) responseModel(layout string) string {
	if layout == s.cfg.layout() {
		return s.cfg.Model
	}
	return s.base + "+" + layout
}

// answerFor assembles the System One answer per type (the same shape as llama.cpp's decision models).
func answerFor(q question, probs []float64) map[string]any {
	best, bp := 0, -1.0
	for i, p := range probs {
		if p > bp {
			best, bp = i, p
		}
	}
	switch q.Type {
	case "noul":
		return map[string]any{"type": "noul", "noul": probs[0]}
	case "score":
		pm := map[string]float64{}
		legend := map[string]string{}
		var exp float64
		for i, o := range q.Options {
			pm[o.ID] = probs[i]
			legend[o.ID] = o.Description
			exp += float64(i) * probs[i]
		}
		return map[string]any{"type": "score", "score": exp, "probabilities": pm, "legend": legend, "confidence": bp}
	default:
		pm := map[string]float64{}
		for i, o := range q.Options {
			pm[o.ID] = probs[i]
		}
		n := float64(len(probs))
		conf := 0.0
		if n > 1 {
			conf = (bp - 1/n) / (1 - 1/n)
		}
		return map[string]any{"type": "choice", "choice": q.Options[best].ID, "probabilities": pm, "confidence": conf}
	}
}
