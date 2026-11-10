package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hayaenfj/shikii/internal/systemone"
)

func (s *server) post(ctx context.Context, path string, body any, out any) (int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Upstream+path, bytes.NewReader(buf))
	if err != nil {
		return 0, systemone.ScrubError(err, s.cfg.Upstream) // these errors repeat the URL: never its credentials
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return 0, systemone.ScrubError(err, s.cfg.Upstream)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("%s: %d %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("%s: bad json: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

func (s *server) upstreamModel(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.Upstream+"/v1/models", nil)
	if err != nil {
		return "", systemone.ScrubError(err, s.cfg.Upstream)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", systemone.ScrubError(err, s.cfg.Upstream)
	}
	defer resp.Body.Close()
	var v struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if len(v.Models) > 0 && v.Models[0].Name != "" {
		return v.Models[0].Name, nil
	}
	if len(v.Data) > 0 && v.Data[0].ID != "" {
		return v.Data[0].ID, nil
	}
	return "", errors.New("no model in /v1/models")
}

// checkLabels checks with /tokenize that every label is a single token.
func (s *server) checkLabels(ctx context.Context) error {
	var bad []string
	for _, l := range labels {
		var out struct {
			Tokens []json.RawMessage `json:"tokens"`
		}
		if _, err := s.post(ctx, "/tokenize", map[string]any{"content": l}, &out); err != nil {
			return err
		}
		if len(out.Tokens) != 1 {
			bad = append(bad, fmt.Sprintf("%s=%d", l, len(out.Tokens)))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("labels are not single tokens: %s", strings.Join(bad, " "))
	}
	return nil
}

// applyTemplate builds the prompt with the model's chat template. It tries enable_thinking=false,
// and if that is rejected (an upstream that does not accept kwargs) stops sending it.
func (s *server) applyTemplate(ctx context.Context, msgs []message) (string, error) {
	s.mu.Lock()
	kw := s.thinkingKwarg
	s.mu.Unlock()
	body := map[string]any{"messages": msgs}
	if kw {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	var out struct {
		Prompt string `json:"prompt"`
	}
	code, err := s.post(ctx, "/apply-template", body, &out)
	if err != nil && kw && code >= 400 && code < 500 {
		s.mu.Lock()
		s.thinkingKwarg = false
		s.mu.Unlock()
		delete(body, "chat_template_kwargs")
		_, err = s.post(ctx, "/apply-template", body, &out)
	}
	if err != nil {
		return "", err
	}
	if out.Prompt == "" {
		return "", errors.New("/apply-template returned an empty prompt")
	}
	return out.Prompt, nil
}

type completion struct {
	Content string `json:"content"`
	Probs   []struct {
		Top []tokenProb `json:"top_probs"`
	} `json:"completion_probabilities"`
	Timings struct {
		PromptN int `json:"prompt_n"`
		CacheN  int `json:"cache_n"`
	} `json:"timings"`
}
