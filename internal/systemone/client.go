// Package systemone is a thin client for the System One format (TypeSafe Jev / llama.cpp /v1/systemone, etc.).
// It does not own the format. It only absorbs differences in the base URL.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	// HasNoul reports whether the response carried a noul value (the field was there and was not null). An answer
	// without a value is not an answer of zero: Noul is 0 in both cases, and only this tells them apart.
	HasNoul bool `json:"-"`
}

// UnmarshalJSON reads an answer and records whether it carried a noul value.
func (a *Answer) UnmarshalJSON(data []byte) error {
	type fields Answer // the same fields without the methods, so the standard decoding is used
	var in struct {
		fields
		Noul *float64 `json:"noul"` // shadows fields.Noul: nil when the field is missing or null
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*a = Answer(in.fields)
	a.Noul, a.HasNoul = 0, in.Noul != nil
	if a.HasNoul {
		a.Noul = *in.Noul
	}
	return nil
}

// MarshalJSON writes an answer. A yes/no answer is written with its value even when the value is zero (the
// sending side: test mocks). Left out, a zero would arrive as an answer without a value.
func (a Answer) MarshalJSON() ([]byte, error) {
	type fields Answer
	out := struct {
		fields
		Noul *float64 `json:"noul,omitempty"`
	}{fields: fields(a)}
	if a.Type == "noul" || a.HasNoul || a.Noul != 0 {
		out.Noul = &a.Noul
	}
	return json.Marshal(out)
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
// RedactURL hides credentials embedded in a URL (user:token@host, or a token alone before the @). Used for logs and
// error messages. Everything between the scheme and the last @ is hidden, whether or not the URL parses: a token with a
// raw "/" or "?" in it, a URL without its scheme and a URL with a broken port all parse differently from what was meant.
// (An @ in a path is rare in a model URL; there the host is hidden too, which loses a detail and leaks nothing.)
func RedactURL(raw string) string {
	start, at := credentials(raw)
	if at < 0 {
		return raw
	}
	return raw[:start] + "xxxxx" + raw[at:]
}

// credentials finds what RedactURL hides: raw[start:at] is the part between the scheme and the last @ (at is -1 without an @).
// A "://" ends the scheme only when a scheme stands before it (letters, digits, + - .): in a URL written without its scheme a
// later "://" (in a query, say) is not the start of the credentials.
func credentials(raw string) (start, at int) {
	if i := strings.Index(raw, "://"); i > 0 && isScheme(raw[:i]) {
		start = i + len("://")
	}
	at = strings.LastIndex(raw[start:], "@")
	if at < 0 {
		return start, -1
	}
	return start, start + at
}

func isScheme(s string) bool {
	for i, r := range s {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !letter && (i == 0 || !((r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.')) {
			return false
		}
	}
	return s != ""
}

// scrubbed is an error whose text was written anew without the credentials of a URL; it still unwraps to the original (a
// deadline, a refused connection), so errors.Is and errors.As keep working.
type scrubbed struct {
	msg string
	err error
}

func (e scrubbed) Error() string { return e.msg }
func (e scrubbed) Unwrap() error { return e.err }

// ScrubError returns the error of a request to the URL raw without the URL's credentials in its text. The URL parser and the HTTP
// client repeat the URL, or a piece of it (the "port" after a colon, the "scheme" before one), in their own words and their own
// encoding, and no rewriting of such a text hides every spelling. So for a URL with credentials the text is written anew: the
// operation, the URL without its credentials, and the cause as the network reports it. The parser's words are left out, and so is
// a cause that names a piece of the credentials. A URL without credentials keeps its error as it is.
func ScrubError(err error, raw string) error {
	if err == nil {
		return nil
	}
	start, at := credentials(raw)
	if at <= start { // no @, or nothing before it
		return err
	}
	shown := RedactURL(raw)
	msg := "the request to " + shown + " failed"
	var uerr *url.Error
	if errors.As(err, &uerr) {
		switch {
		case uerr.Op == "parse":
			msg = "the URL " + shown + " does not parse as a URL"
		case uerr.Err == nil:
		case strings.Contains(uerr.Err.Error(), "unsupported protocol scheme"): // it quotes what stands before the first colon
			msg = uerr.Op + " " + shown + ": unsupported protocol scheme (write the URL with http:// or https://)"
		case quotableCause(uerr.Err, raw, start, at):
			msg = uerr.Op + " " + shown + ": " + uerr.Err.Error()
		}
	}
	return scrubbed{msg: msg, err: err}
}

// quotableCause reports whether the cause of a failed request may be quoted next to the URL without its credentials. Not when
// it names a piece of the credentials (the URL's own host is taken out first: a user name may be part of it), and not when the
// client looked up another name than the URL's host: credentials with a raw "/", "?" or "#" end early for the client, and what it
// then takes for the host is a piece of them, in whatever spelling the name lookup gives it.
func quotableCause(cause error, raw string, start, at int) bool {
	host := raw[at+1:]
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	var lookup *net.DNSError
	if errors.As(cause, &lookup) && !strings.EqualFold(strings.TrimSuffix(lookup.Name, "."), name) {
		return false
	}
	text := cause.Error()
	for _, looked := range lookedUp.FindAllStringSubmatch(text, -1) { // the same, read from the words of a dial error
		if !strings.EqualFold(strings.TrimSuffix(looked[1], "."), name) {
			return false
		}
	}
	if name != "" {
		text = strings.ReplaceAll(text, name, "")
	}
	return !namesCredentials(text, raw[start:at])
}

// lookedUp finds the name a dial error says it looked up ("lookup <name>: no such host", "lookup <name> on <server>").
var lookedUp = regexp.MustCompile(`lookup ([^\s:]+)`)

// namesCredentials reports whether text contains the credentials or one of their parts (three characters or more), as given or
// percent-decoded. It only decides whether a cause is quoted; nothing is rewritten.
func namesCredentials(text, secret string) bool {
	pieces := append([]string{secret}, strings.FieldsFunc(secret, func(r rune) bool { return strings.ContainsRune(":/?#@", r) })...)
	for _, piece := range pieces {
		forms := []string{piece}
		if decoded, err := url.PathUnescape(piece); err == nil && decoded != piece {
			forms = append(forms, decoded)
		}
		for _, form := range forms {
			if len(form) >= 3 && strings.Contains(strings.ToLower(text), strings.ToLower(form)) { // a host is written in lower case
				return true
			}
		}
	}
	return false
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
		return nil, ScrubError(err, c.BaseURL+path) // the parser's error repeats the URL; the whole URL as it was given: the credentials may stand after the first slash
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, ScrubError(err, c.BaseURL+path) // and so does the client's
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
	// Only an http or https URL with a host can answer. "localhost:8080" parses as the scheme "localhost": a request can be built
	// from it but never sent, so it would be polled until the deadline.
	if u, err := url.Parse(baseURL); err == nil && (u.Host == "" || (u.Scheme != "http" && u.Scheme != "https")) {
		return fmt.Errorf("the model URL %s cannot be requested: it is not an http or https URL with a host (write it with its scheme, e.g. http://127.0.0.1:8080)", RedactURL(baseURL))
	}
	url := strings.TrimRight(baseURL, "/") + "/health"
	client := &http.Client{Timeout: 3 * time.Second}
	for i := 0; ; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			// A URL that cannot become a request never answers: say so instead of waiting (or calling Do with no request).
			cause := ScrubError(err, baseURL) // a URL with credentials: the parser's words are left out
			if cause == err {
				if inner := errors.Unwrap(err); inner != nil {
					cause = inner // without credentials the parser's own words are kept (the outer error only repeats the URL)
				}
			}
			return fmt.Errorf("the model URL %s cannot be requested: %w (write it with its scheme, e.g. http://127.0.0.1:8080)", RedactURL(baseURL), cause)
		}
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
