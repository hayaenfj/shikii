// Package systemone is a thin client for the System One format (TypeSafe Jev / llama.cpp /v1/systemone, etc.).
// It does not own the format. It only absorbs differences in the base URL.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Question is one question. Criteria is OrderedCriteria (id→description, order kept) for choice, and []string for score.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// OrderedCriteria are the options of a choice. They are sent as a JSON object **in this order**
// (Go maps sort their keys, which would make it impossible to measure order bias or reproduce the spec's order).
type OrderedCriteria []Criterion

// Criterion is one option.
type Criterion struct {
	ID          string
	Description string
}

// MarshalJSON writes {"id":"desc",…} in order.
func (oc OrderedCriteria) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, c := range oc {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(c.ID)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(c.Description)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON is for the receiving side (test mocks). It reads while keeping the JSON order.
func (oc *OrderedCriteria) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("criteria: expected object")
	}
	var out OrderedCriteria
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var v string
		if err := dec.Decode(&v); err != nil {
			return err
		}
		out = append(out, Criterion{ID: kt.(string), Description: v})
	}
	*oc = out
	return nil
}

// Request is the body of /v1/systemone.
type Request struct {
	State     string              `json:"state"`
	Model     string              `json:"model,omitempty"`
	Questions map[string]Question `json:"questions"`
	// Layout is the name of the prompt layout (state-last | state-first | plain). shikii-readout follows it and adds it to the model name in the response.
	// Decision models ignore it as an unknown field. If empty, the server's default applies.
	Layout string `json:"layout,omitempty"`
}

// Answer is the answer to one question. Which fields are used depends on the type.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Response is the response. Model is the model that actually answered (e.g. "jev-1.13.0").
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Client is a connection to one endpoint.
type Client struct {
	BaseURL string // e.g. https://api.typesafe.ai, http://127.0.0.1:8080
	Path    string // default /v1/systemone. OpenRouter uses /api/alpha/decisions
	APIKey  string // if empty, no Authorization header is sent (local)
	Model   string // e.g. jev-latest, typesafe/jev-1.13 (OpenRouter)
	HTTP    *http.Client
}

// DefaultPath is the standard endpoint of the System One format, used by TypeSafe, llama.cpp and many compatible servers.
const DefaultPath = "/v1/systemone"

// New creates a client. If baseURL contains a path (e.g. https://openrouter.ai/api/alpha/decisions),
// it is used as is; otherwise /v1/systemone is added.
// RedactURL hides credentials embedded in a URL (user:token@host). Used for logs and error messages.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Redacted()
}

func New(baseURL, apiKey, model string) *Client {
	base, path := SplitEndpoint(baseURL)
	return &Client{
		BaseURL: base,
		Path:    path,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// SplitEndpoint splits a URL into "up to the host" and "the path". If there is no path (only "/"), DefaultPath.
func SplitEndpoint(raw string) (base, path string) {
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, DefaultPath
	}
	if u.Path == "" {
		return raw, DefaultPath
	}
	u2 := *u
	u2.Path, u2.RawPath, u2.RawQuery = "", "", ""
	return u2.String(), u.Path
}

// Ask evaluates a set of questions against state in one call.
func (c *Client) Ask(ctx context.Context, state string, qs map[string]Question) (*Response, error) {
	return c.AskLayout(ctx, state, qs, "")
}

// AskLayout is Ask with a layout name (the spec's layout; if empty, the server's default).
func (c *Client) AskLayout(ctx context.Context, state string, qs map[string]Question, layout string) (*Response, error) {
	body, err := json.Marshal(Request{State: state, Model: c.Model, Questions: qs, Layout: layout})
	if err != nil {
		return nil, err
	}
	path := c.Path
	if path == "" {
		path = DefaultPath
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("systemone %s: HTTP %d: %s", RedactURL(c.BaseURL), resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("systemone: bad response: %w", err)
	}
	return &out, nil
}

// BindHost is the address to listen on. On the host, only 127.0.0.1 (not exposed to the LAN). Inside a container (Docker's /.dockerenv, podman's /run/.containerenv,
// k8s's KUBERNETES_SERVICE_HOST) it listens on all interfaces, and publishing the port decides whether it is exposed. There is no setting to choose.
func BindHost() string {
	return BindHostFor([]string{"/.dockerenv", "/run/.containerenv"}, "KUBERNETES_SERVICE_HOST")
}

func BindHostFor(markers []string, kubeEnv string) string {
	for _, m := range markers {
		if _, err := os.Stat(m); err == nil {
			return "0.0.0.0"
		}
	}
	if kubeEnv != "" && os.Getenv(kubeEnv) != "" {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// WaitReady waits until the model endpoint (the baseURL of llama-server or shikii-readout) returns 200 on /health.
// When a model is started first, as in Docker, this lets shikii wait by itself instead of relying on a healthcheck with an external command (curl / wget).
// It retries every `every`, and when ctx ends it returns an error saying what it was waiting for. Progress goes to logf (once every 10 tries).
func WaitReady(ctx context.Context, baseURL string, every time.Duration, logf func(string, ...any)) error {
	url := strings.TrimRight(baseURL, "/") + "/health"
	client := &http.Client{Timeout: 3 * time.Second}
	for i := 0; ; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				if i > 0 && logf != nil {
					logf("model at %s is ready", RedactURL(baseURL))
				}
				return nil
			}
		}
		if i == 0 && logf != nil {
			logf("waiting for the model at %s (GET /health)", RedactURL(baseURL))
		} else if i%10 == 0 && logf != nil {
			logf("still waiting for the model at %s", RedactURL(baseURL))
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the model at %s did not come up: %w (is it still loading, or is the URL wrong?)", RedactURL(baseURL), ctx.Err())
		case <-time.After(every):
		}
	}
}
