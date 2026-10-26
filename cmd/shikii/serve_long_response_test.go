package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 0.9.6 audit of the HTTP limits (eaf2c9b): the server now cuts every response after WriteTimeout (2 minutes). Answering on the page runs the
// automatic calibration in the same request, and it can ask the model again for every earlier truth, so a long run would cut the reply and the
// page would show a connection error although the answer was saved. Handlers that start long work extend their own write deadline first.
func TestLongHandlersOutliveTheWriteTimeout(t *testing.T) {
	slow := func(extend bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if extend {
				allowLongResponse(w)
			}
			time.Sleep(300 * time.Millisecond) // the long work
			io.WriteString(w, "done")
		})
	}
	for _, extend := range []bool{false, true} {
		ts := httptest.NewUnstartedServer(slow(extend))
		ts.Config = newHTTPServer(slow(extend))
		ts.Config.WriteTimeout = 100 * time.Millisecond
		ts.Start()
		resp, err := http.Get(ts.URL)
		var body []byte
		if err == nil {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		ts.Close()
		if extend && (err != nil || string(body) != "done") {
			t.Fatalf("a handler that extends its deadline answers after the write timeout: %v %q", err, body)
		}
		if !extend && err == nil && string(body) == "done" {
			t.Fatal("precondition: without extending, the write timeout cuts the reply")
		}
	}
}

// An answer that brings the truths to min_n calibrates in the same request, so it waits for the calibration lock. While a background tune holds
// the lock past the write timeout, that reply still arrives (the answer was saved; before 0.9.6 the page showed a connection error).
func TestAnswerThatCalibratesBehindATuneStillReplies(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "true") // min_n 2
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	s := newServerState(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true})
	h := s.handler()
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()
	label := func(trace any) (map[string]any, error) {
		resp, err := http.Post(ts.URL+"/v1/label", "application/json", strings.NewReader(fmt.Sprintf(`{"trace_id":%q,"truth":"change_request"}`, trace)))
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var out map[string]any
		return out, json.NewDecoder(resp.Body).Decode(&out)
	}
	var traces []any
	for _, m := range []string{"x", "y"} {
		code, d := postJSON(t, ts.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": m}})
		if code != 200 {
			t.Fatalf("decide: %d %v", code, d)
		}
		traces = append(traces, d["trace_id"])
	}
	if _, err := label(traces[0]); err != nil {
		t.Fatal(err)
	}
	release := s.lockCalibration(nil) // a background tune holds the lock
	time.AfterFunc(300*time.Millisecond, release)
	out, err := label(traces[1])
	if err != nil {
		t.Fatalf("the answer's reply arrives after waiting for the lock past the write timeout: %v", err)
	}
	if out["calibrated"] != true {
		t.Fatalf("precondition: the second answer calibrates (so it waited for the lock): %v", out)
	}
}

// 0.9.6 audit of the HTTP limits, middle 1, made structural in 0.9.7: a reply that waits for the calibration lock (a background tune holds it
// up to 40 minutes) is not cut by the write timeout, because taking the lock extends the reply's deadline. The test holds the lock past the
// write timeout, as a background tune would, and every route that waits for it still answers; status, which does not wait, is still cut.
func TestEveryRouteWaitingForTheLockOutlivesTheWriteTimeout(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "true")
	s := newServerState(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), autoCalibrate: true})
	h := s.handler()
	late := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			time.Sleep(300 * time.Millisecond)
		}
		h.ServeHTTP(w, r)
	})
	ts := httptest.NewUnstartedServer(late)
	ts.Config = newHTTPServer(late)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()
	reply := func(path, body string) error {
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		return err
	}
	for path, body := range map[string]string{
		"/v1/tune/sync":    `{"spec":"intent"}`,
		"/v1/tune/variant": `{"spec":"intent","name":"v2","question":"q"}`,
		"/v1/tune/adopt":   `{"spec":"intent","run":"r","meaning_same":true}`,
		"/v1/tune/review":  `{"spec":"intent","run":"r","keep":true}`,
	} {
		release := s.lockCalibration(nil) // a background tune holds the lock
		time.AfterFunc(300*time.Millisecond, release)
		if err := reply(path, body); err != nil {
			t.Errorf("%s answers after waiting for the lock past the write timeout: %v", path, err)
		}
	}
	if err := reply("/v1/status", ""); err == nil {
		t.Error("precondition: a reply that does not wait for the lock is still cut by the write timeout")
	}
}

// 0.9.6 recheck, low 2: a background tune holds the calibration lock for its model run and then a calibration that may ask the model again, so
// a reply waiting behind it needs longResponseLimit longer than both together (it was 30 minutes, the same as the tune alone).
func TestLongResponseLimitCoversATuneAndItsCalibration(t *testing.T) {
	if longResponseLimit <= tuneRunLimit+replayLimit {
		t.Fatalf("longResponseLimit %v must exceed a tune (%v) plus its calibration (%v)", longResponseLimit, tuneRunLimit, replayLimit)
	}
}
