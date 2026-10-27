package systemone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The response shown in the Quick Start on docs.typesafe.ai can be read as is.
func TestParsesDocumentedResponse(t *testing.T) {
	const raw = `{
  "model": "jev-1.13.0",
  "answers": {
    "department": {"type": "choice", "choice": "technical", "confidence": 1.0,
      "probabilities": {"technical": 1.0, "billing": 0.0, "sales": 0.0}},
    "frustration": {"type": "score", "score": 0.67, "confidence": 0.5,
      "legend": {"0": "Calm, just stating facts", "1": "Frustrated but civil", "2": "Very angry, strong language"},
      "probabilities": {"0": 0.33, "1": 0.67, "2": 0.0}},
    "is_urgent": {"type": "noul", "noul": 0.99}
  },
  "usage": {"input_tokens": 428, "output_tokens": 73}
}`
	var r Response
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Model != "jev-1.13.0" || r.Usage.InputTokens != 428 {
		t.Fatalf("header: %+v", r)
	}
	if a := r.Answers["department"]; a.Choice != "technical" || a.Probabilities["technical"] != 1.0 {
		t.Fatalf("choice: %+v", a)
	}
	if a := r.Answers["frustration"]; a.Score != 0.67 || a.Legend["2"] != "Very angry, strong language" {
		t.Fatalf("score: %+v", a)
	}
	if a := r.Answers["is_urgent"]; a.Noul != 0.99 {
		t.Fatalf("noul: %+v", a)
	}
}

func TestAskSendsDocumentedRequest(t *testing.T) {
	var got Request
	var auth, ctype, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, ctype = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":{"type":"noul","noul":0.5}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "secret", "jev-latest")
	resp, err := c.Ask(context.Background(), "state text", map[string]Question{
		"q": {Type: "choice", Instructions: "which", Criteria: map[string]string{"a": "A", "b": "B"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/systemone" || auth != "Bearer secret" || ctype != "application/json" {
		t.Fatalf("path=%s auth=%s ctype=%s", path, auth, ctype)
	}
	if got.State != "state text" || got.Model != "jev-latest" || got.Questions["q"].Instructions != "which" {
		t.Fatalf("request: %+v", got)
	}
	if crit, ok := got.Questions["q"].Criteria.(map[string]any); !ok || crit["b"] != "B" {
		t.Fatalf("criteria: %+v", got.Questions["q"].Criteria)
	}
	if resp.Answers["q"].Noul != 0.5 {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestAskOmitsAuthWhenNoKeyAndSurfacesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("local endpoint must not receive Authorization, got %q", r.Header.Get("Authorization"))
		}
		http.Error(w, `{"error":"model not loaded"}`, 503)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "", "kev-4b").Ask(context.Background(), "s", map[string]Question{"q": {Type: "noul", Instructions: "x"}})
	if err == nil || !contains(err.Error(), "503") || !contains(err.Error(), "model not loaded") {
		t.Fatalf("HTTP error must carry status and body, got %v", err)
	}
}

// OrderedCriteria keeps the order of the JSON object (Go maps are sorted).
func TestOrderedCriteriaKeepsWireOrder(t *testing.T) {
	oc := OrderedCriteria{{"zeta", "Z"}, {"alpha", "A"}, {"mid", "M \"quoted\""}}
	b, err := json.Marshal(oc)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"zeta":"Z","alpha":"A","mid":"M \"quoted\""}`
	if string(b) != want {
		t.Fatalf("got %s want %s", b, want)
	}
	var back OrderedCriteria
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 || back[0].ID != "zeta" || back[2].Description != `M "quoted"` {
		t.Fatalf("round trip lost order or content: %+v", back)
	}
	// comparison: with a map the order changes
	m, _ := json.Marshal(map[string]string{"zeta": "Z", "alpha": "A"})
	if string(m) != `{"alpha":"A","zeta":"Z"}` {
		t.Fatalf("premise: Go map must sort keys, got %s", m)
	}
}

// If the endpoint URL has a path, it is used (OpenRouter's /api/alpha/decisions). Otherwise /v1/systemone.
func TestEndpointPathIsHonored(t *testing.T) {
	cases := map[string][2]string{
		"https://api.typesafe.ai":                   {"https://api.typesafe.ai", "/v1/systemone"},
		"http://127.0.0.1:8080/":                    {"http://127.0.0.1:8080", "/v1/systemone"},
		"https://openrouter.ai/api/alpha/decisions": {"https://openrouter.ai", "/api/alpha/decisions"},
		"https://example.com/v1/systemone/":         {"https://example.com", "/v1/systemone"},
	}
	for in, want := range cases {
		base, path := SplitEndpoint(in)
		if base != want[0] || path != want[1] {
			t.Errorf("%s → (%s, %s), want (%s, %s)", in, base, path, want[0], want[1])
		}
	}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1.13","answers":{"q":{"type":"noul","noul":0.5}}}`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/api/alpha/decisions", "k", "typesafe/jev-1.13")
	if _, err := c.Ask(context.Background(), "s", map[string]Question{"q": {Type: "noul", Instructions: "x"}}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/alpha/decisions" {
		t.Fatalf("request must go to the given path, got %s", gotPath)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// shikii itself waits for the model endpoint to come up (without relying on external commands such as curl / wget).
// It retries until /health returns 200 and gives up at the deadline. Progress goes to logf.
func TestWaitReadyPollsUntilHealthy(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			http.Error(w, "loading", 503)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, format) }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitReady(ctx, srv.URL, 10*time.Millisecond, logf); err != nil {
		t.Fatalf("must become ready on the 3rd poll: %v", err)
	}
	if calls != 3 {
		t.Fatalf("polled %d times, want 3", calls)
	}
	// Deadline: if ctx ends while the endpoint is still down, it is an error (with a message saying what it was waiting for)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 503) }))
	t.Cleanup(dead.Close)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel2()
	err := WaitReady(ctx2, dead.URL, 10*time.Millisecond, logf)
	if err == nil || !strings.Contains(err.Error(), dead.URL) {
		t.Fatalf("a model that never comes up must be reported with its URL: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("waiting must say what it waits for")
	}
}
