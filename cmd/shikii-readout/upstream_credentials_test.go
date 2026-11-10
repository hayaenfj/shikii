package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The errors of a call to the upstream repeat its URL in the HTTP client's own encoding. Whatever goes back to the caller or into
// the log hides the credentials in it: the probe, the model lookup and a forwarded request.
func TestUpstreamErrorsHideTheCredentialsOfTheUpstreamURL(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(closed.URL, "http://")
	closed.Close() // nothing listens there: every call fails on this machine
	s := &server{cfg: config{Upstream: "http://sk-AbC%2FdEf%2B123@" + host, Model: "m", Timeout: 5 * time.Second}, http: http.DefaultClient}
	shown := []string{}
	if _, err := s.post(context.Background(), "/v1/systemone", map[string]any{"state": "x"}, nil); err == nil {
		t.Fatal("the upstream answered")
	} else {
		shown = append(shown, err.Error())
	}
	if _, err := s.upstreamModel(context.Background()); err == nil {
		t.Fatal("the upstream answered")
	} else {
		shown = append(shown, err.Error())
	}
	if err := s.detectUpstream(context.Background()); err == nil {
		t.Fatal("the upstream answered")
	} else {
		shown = append(shown, err.Error())
	}
	rec := httptest.NewRecorder()
	s.forward(rec, httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader([]byte(`{}`))), []byte(`{}`))
	if rec.Code != 502 {
		t.Fatalf("a forward to an upstream that is down is a 502: %d %s", rec.Code, rec.Body.String())
	}
	shown = append(shown, rec.Body.String())
	for _, text := range shown {
		if !strings.Contains(text, host) {
			t.Errorf("the error still names the host: %s", text)
		}
		for _, secret := range []string{"sk-AbC", "dEf", "123@"} {
			if strings.Contains(text, secret) {
				t.Errorf("the error shows %q of the credentials: %s", secret, text)
			}
		}
	}
}

// echoing answers every request with the status and a body that names the host and path it was asked for, as a proxy's error
// page does.
type echoing struct{ status int }

func (e echoing) RoundTrip(req *http.Request) (*http.Response, error) {
	body := "bad gateway: cannot reach " + req.URL.Host + " for " + req.URL.Path
	return &http.Response{StatusCode: e.status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
}

// An upstream URL with an @ after its first slash is read with another host than the one after the @, and that host may be a
// part of credentials. What it answers is not repeated: not in the error of a call and not in a forwarded response. An upstream
// URL the client reads as written keeps the upstream's words.
func TestUpstreamStatusErrorsLeaveOutTheResponseOfAMisreadUpstream(t *testing.T) {
	misread := &server{cfg: config{Upstream: "http://sk-abc/def-s3cr3t-token@127.0.0.1:8080", Model: "m", Timeout: 5 * time.Second}, http: &http.Client{Transport: echoing{status: 502}}}
	shown := []string{}
	_, err := misread.post(context.Background(), "/tokenize", map[string]any{"content": "x"}, nil)
	if err == nil {
		t.Fatal("the upstream answered 502")
	}
	shown = append(shown, err.Error())
	rec := httptest.NewRecorder()
	misread.forward(rec, httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader([]byte(`{}`))), []byte(`{}`))
	if rec.Code != 502 {
		t.Fatalf("the status is passed on: %d", rec.Code)
	}
	shown = append(shown, rec.Body.String())
	for _, text := range shown {
		for _, piece := range []string{"sk-abc", "s3cr3t", "def-"} {
			if strings.Contains(text, piece) {
				t.Errorf("the upstream's answer is repeated with %q of the credentials: %s", piece, text)
			}
		}
		if !strings.Contains(text, "the response is left out") {
			t.Errorf("the error says that the response is left out: %s", text)
		}
	}
	plain := &server{cfg: config{Upstream: "http://127.0.0.1:8080", Model: "m", Timeout: 5 * time.Second}, http: &http.Client{Transport: echoing{status: 502}}}
	if _, err := plain.post(context.Background(), "/tokenize", map[string]any{"content": "x"}, nil); err == nil || !strings.Contains(err.Error(), "/tokenize: 502 bad gateway: cannot reach 127.0.0.1:8080 for /tokenize") {
		t.Errorf("an upstream read as written keeps its status error: %v", err)
	}
	rec = httptest.NewRecorder()
	plain.forward(rec, httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader([]byte(`{}`))), []byte(`{}`))
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "cannot reach 127.0.0.1:8080") {
		t.Errorf("a forwarded error of an upstream read as written is passed on as it is: %d %s", rec.Code, rec.Body.String())
	}
}

// Only an error answer of a misread upstream is left out: what it answers with a 2xx status is the decision and is passed on.
func TestForwardPassesOnASuccessfulAnswerOfAMisreadUpstream(t *testing.T) {
	s := &server{cfg: config{Upstream: "http://sk-abc/def-token@127.0.0.1:8080", Model: "m", Timeout: 5 * time.Second}, http: &http.Client{Transport: echoing{status: 200}}}
	rec := httptest.NewRecorder()
	s.forward(rec, httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader([]byte(`{}`))), []byte(`{}`))
	if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "bad gateway: cannot reach") {
		t.Errorf("a 2xx answer is passed on as it is: %d %q", rec.Code, rec.Body.String())
	}
}
