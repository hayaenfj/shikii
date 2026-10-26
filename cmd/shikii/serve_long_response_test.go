package main

import (
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

// The answer handler (with the automatic calibration) and the synchronous tune extend their deadline. A middleware waits past the write
// timeout before the handler runs, standing in for long work: only a handler that extends its own deadline still gets its reply through.
func TestAnswerAndSyncTuneExtendTheirDeadline(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "true")
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true})
	late := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/label" || r.URL.Path == "/v1/tune/sync" {
			time.Sleep(300 * time.Millisecond)
		}
		h.ServeHTTP(w, r)
	})
	ts := httptest.NewUnstartedServer(late)
	ts.Config = newHTTPServer(late)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()
	post := func(path, body string) (int, error) {
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		return resp.StatusCode, err
	}
	code, d := postJSON(t, ts.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "x"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	if code, err := post("/v1/label", fmt.Sprintf(`{"trace_id":%q,"truth":"change_request"}`, d["trace_id"])); err != nil || code != 200 {
		t.Fatalf("the answer's reply arrives after the write timeout: %d %v", code, err)
	}
	if code, err := post("/v1/tune/sync", `{"spec":"intent"}`); err != nil || code == 0 {
		t.Fatalf("the synchronous tune's reply arrives after the write timeout: %d %v", code, err)
	}
}
