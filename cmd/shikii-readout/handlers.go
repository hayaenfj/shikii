package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

func (s *server) handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": s.cfg.Model, "object": "model"}}})
}

func (s *server) handleSystemOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "POST only")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if s.passThrough {
		s.forward(w, r, body)
		return
	}
	state, qs, layoutReq, err := parseRequest(body)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	layout, err := s.requestLayout(layoutReq)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()
	answers := map[string]any{}
	inputTokens := 0
	for _, q := range qs {
		keys, err := s.optionKeys(ctx, q, layout)
		if err != nil {
			writeErr(w, 422, err.Error())
			return
		}
		prompt, err := s.applyTemplate(ctx, buildMessages(s.cfg.System, state, q, layout))
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		if err := s.warmPrefixFor(ctx, prompt, layout); err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		c, err := s.complete(ctx, prompt)
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		s.noteCache(c)
		probs, missing := readProbsFor(c.Probs[0].Top, keys)
		s.mu.Lock()
		s.missingLabels += missing
		s.totalQuestions++
		s.mu.Unlock()
		if missing > 0 {
			log.Printf("question %s: %d/%d labels below n_probs=%d (floored)", q.Key, missing, len(q.Options), s.cfg.NProbs)
		}
		answers[q.Key] = answerFor(q, probs)
		inputTokens += c.Timings.PromptN
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":   s.responseModel(layout),
		"answers": answers,
		"usage":   map[string]int{"input_tokens": inputTokens, "output_tokens": len(qs)},
	})
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
