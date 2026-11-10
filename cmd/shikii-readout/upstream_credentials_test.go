package main

import (
	"bytes"
	"context"
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
