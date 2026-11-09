package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// warmPrefixFor sends the prompt's prefix (up to just before the state) to the upstream with n_predict=0 when it differs from last time,
// so that a checkpoint is made. In the state-first layout the prefix contains the state, so it does nothing.
func (s *server) warmPrefixFor(ctx context.Context, prompt, layout string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	marker := stateMarker
	switch layout {
	case "state-first":
		return nil
	case "plain":
		marker = plainMarker
	}
	idx := strings.LastIndex(prompt, marker)
	if idx <= 0 {
		return nil
	}
	prefix := prompt[:idx]
	s.mu.Lock()
	if prefix == s.warmPrefix {
		s.mu.Unlock()
		return nil
	}
	flight := s.warmups[prefix]
	if flight == nil {
		limit := s.cfg.Timeout
		if limit <= 0 {
			limit = 120 * time.Second
		}
		shared, cancel := context.WithTimeout(context.Background(), limit)
		flight = &prefixWarmup{done: make(chan struct{}), cancel: cancel}
		if s.warmups == nil {
			s.warmups = map[string]*prefixWarmup{}
		}
		s.warmups[prefix] = flight
		go s.runPrefixWarmup(shared, prefix, flight)
	}
	flight.waiters++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		flight.waiters--
		if flight.waiters == 0 && s.warmups[prefix] == flight {
			delete(s.warmups, prefix)
			flight.cancel()
		}
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-flight.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return flight.err
	}
}

// A waiter's cancellation affects only that waiter. The upstream request ends
// when all waiters leave or its fixed timeout expires. Failures are never cached.
func (s *server) runPrefixWarmup(ctx context.Context, prefix string, flight *prefixWarmup) {
	defer flight.cancel()
	var out completion
	_, err := s.post(ctx, "/completion", map[string]any{"prompt": prefix, "n_predict": 0, "cache_prompt": true}, &out)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmups[prefix] == flight {
		if err == nil {
			s.warmPrefix = prefix
			s.warmTokens = out.Timings.PromptN + out.Timings.CacheN
		}
		delete(s.warmups, prefix)
	}
	flight.err = err
	close(flight.done)
}

// noteCache looks at the response's cache_n and, if prefix reuse broke, marks it to warm up again next time.
func (s *server) noteCache(c *completion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warmTokens > 0 && c.Timings.CacheN < s.warmTokens*9/10 {
		s.warmPrefix = ""
	}
}

// complete runs a single token and returns the top tokens' probabilities. The sampler is neutral (temperature 1, no truncation).
func (s *server) complete(ctx context.Context, prompt string) (*completion, error) {
	body := map[string]any{
		"prompt": prompt, "n_predict": 1, "temperature": 1.0, "top_k": 0, "top_p": 1.0, "min_p": 0.0,
		"n_probs": s.cfg.NProbs, "post_sampling_probs": true, "cache_prompt": true,
	}
	var out completion
	if _, err := s.post(ctx, "/completion", body, &out); err != nil {
		return nil, err
	}
	if len(out.Probs) == 0 {
		return nil, errors.New("/completion returned no completion_probabilities (n_probs unsupported?)")
	}
	return &out, nil
}
