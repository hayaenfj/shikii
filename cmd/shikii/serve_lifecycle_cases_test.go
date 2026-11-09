package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// When truths reach min_n, the server draws the line itself and writes it into the spec. The person never types calibrate.
func TestServeAutoCalibratesWhenEnoughLabels(t *testing.T) {
	url, specPath, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	for i := 0; i < 2; i++ {
		_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix " + string(rune('a'+i))}})
		code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request"})
		if code != 200 {
			t.Fatalf("label %d: %v", i, l)
		}
		if i == 0 && l["calibrated"] == true {
			t.Fatal("must not calibrate below min_n")
		}
		if i == 1 && l["calibrated"] != true {
			t.Fatalf("second label reaches min_n=2: server must calibrate and say so: %v", l)
		}
	}
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if c := sp.CalibrationFor("m"); c == nil || c.Method != "identity" || c.SpecHash != sp.Hash() {
		t.Fatalf("spec must now carry a calibration for model m: %+v", sp.Calibration)
	}
	// After the line is drawn, decisions are act
	_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix z"}})
	if d["band"] != "act" {
		t.Fatalf("after auto-calibration the band must reach act, got %v", d["band"])
	}
}

func TestHTTPServerLimitsSlowConnections(t *testing.T) {
	srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 || srv.MaxHeaderBytes <= 0 {
		t.Fatalf("the HTTP server must bound headers, reads, writes, idle connections and header size: %+v", srv)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv.ReadHeaderTimeout = 50 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	defer srv.Close()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nX-Slow:"); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != io.EOF {
		t.Fatalf("an incomplete header must be closed after ReadHeaderTimeout, got %v", err)
	}
}

// Audit 5 item 8: serve's startup waits for the model's endpoint when there is --local, and does not wait without it (hosted only). A failure to wait is a startup failure.
func TestServeWaitsForTheLocalModelOnlyWhenThereIsOne(t *testing.T) {
	var asked []string
	fake := func(ctx context.Context, url string, every time.Duration, logf func(string, ...any)) error {
		asked = append(asked, url)
		if dl, ok := ctx.Deadline(); !ok || time.Until(dl) < 14*time.Minute {
			t.Fatalf("the wait must have a deadline of about 15 minutes")
		}
		if url == "http://127.0.0.1:1" {
			return errors.New("not ready")
		}
		return nil
	}
	if err := waitForLocalModel("", false, fake); err != nil || len(asked) != 0 {
		t.Fatalf("hosted only: nothing to wait for, got %v %v", err, asked)
	}
	// When there is a key and Jev is used, it does not wait even with --local (the rule is not to start a local model, so waiting would keep the page from opening; 2026-10-23)
	if err := waitForLocalModel("http://127.0.0.1:1", true, fake); err != nil || len(asked) != 0 {
		t.Fatalf("with a key the page must open without a local model: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:8080", false, fake); err != nil || len(asked) != 1 || asked[0] != "http://127.0.0.1:8080" {
		t.Fatalf("a local model is waited for at its URL: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:1", false, fake); err == nil {
		t.Fatalf("a model that never answers must fail the start")
	}
}

// D1: a serve pointed at the same project with another case spelling (--port 0, store named .shikii) took another lock and kept running
// as a second page while the page the hook started held the lock (case-insensitive disk).
func TestAuditFinalD1OnePageAcrossCaseSpellings(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	initAt(t, repo)
	other := strings.ToUpper(repo)
	a, errA := os.Stat(repo)
	b, errB := os.Stat(other)
	if other == repo || errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Skip("on a case-sensitive disk the two spellings are not the same folder")
	}
	takePageLock(t, common, repo) // the page the hook started (the recorded spelling) is running
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(other, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		rec, ok := readPageRecord(common, other)
		t.Fatalf("D1: a second page of the same project (%s and %s are the same folder) keeps running (record under the other spelling %v: %+v)", repo, other, ok, rec)
	}
}

// E2: once the lock is released, a reply in flight is cut by the write timeout again - not kept for longResponseLimit.
// The test runs the reply's check by hand and moves its clock, so the write timeout after the release is already over when the handler writes.
func TestAuditE2DeadlineReturnsAfterRelease(t *testing.T) {
	tick := tickReplies(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	working, finish := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(working)
		<-finish // long work that does not use the lock
		io.WriteString(w, "done")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	release := s.lockCalibration() // a short calibration elsewhere
	got := fetchAsync(func() (*http.Response, error) { return http.Get(ts.URL) })
	<-working
	tick() // during the calibration the reply is kept alive
	release()
	back := pastReplyClock(t)
	tick() // the write timeout the reply gets back at this check is over before the handler writes
	back()
	close(finish)
	if r := <-got; r.err == nil && r.body == "done" {
		t.Fatal("E2: the lock was free past the write timeout, yet the reply was not cut: the 45-minute deadline set during the calibration stayed")
	}
}

// eaf2c9b: a slow reader must not hold a connection past the write timeout. While a calibration is active, it was held without bound:
// once the handler starts writing, the write timeout runs from then on, even though the calibration had extended the reply's deadline.
// The reader's slowness is the clock: when the handler writes, the write timeout from its first byte is already over.
func TestAuditSlowReaderHeldDuringCalibration(t *testing.T) {
	tick := tickReplies(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	working, finish := make(chan struct{}), make(chan struct{})
	wrote := make(chan error, 1)
	big := bytes.Repeat([]byte("x"), 1<<20)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(working)
		<-finish
		_, err := w.Write(big)
		wrote <- err
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	release := s.lockCalibration() // a background tune
	defer release()
	got := fetchAsync(func() (*http.Response, error) { return http.Get(ts.URL) })
	<-working
	tick() // the calibration extends the reply's deadline
	pastReplyClock(t)
	close(finish)
	if err := <-wrote; err == nil {
		t.Fatal("a reply that started writing during a calibration kept the calibration's deadline: a client that reads slowly holds it without bound")
	}
	<-got
}

// R5 (E): trackedWriter hides http.Flusher and io.ReaderFrom of the connection's writer.
func TestRe2TrackedWriterKeepsFlusher(t *testing.T) {
	var flusher, readerFrom bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flusher = w.(http.Flusher)
		_, readerFrom = w.(io.ReaderFrom)
		io.WriteString(w, "ok")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Start()
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !flusher || !readerFrom {
		t.Fatalf("E: the writer handed to handlers lost http.Flusher=%v io.ReaderFrom=%v", flusher, readerFrom)
	}
}

func TestAutoCalibrationReportsQualityWithoutLosingAnswers(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			url, path, dir := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
			for i := 0; i < 2; i++ {
				_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": fmt.Sprint(i)}})
				truth := "change_request"
				if i == 1 && !success {
					truth = "question"
				}
				code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": truth})
				if code != 200 || (i == 1 && l["calibrated"] != success) {
					t.Fatalf("label: %v", l)
				}
			}
			var status struct {
				Specs []map[string]any `json:"specs"`
			}
			getJSON(t, url+"/v1/status", &status)
			s := status.Specs[0]
			result, ok := s["calibration_result"].(map[string]any)
			want := "quality"
			if success {
				want = "written"
			}
			if !ok || result["reason"] != want || result["human_truths"] != float64(2) || result["min_n"] != float64(2) {
				t.Fatalf("structured calibration result: %v", s)
			}
			if result["hash"] != s["hash"] || result["model"] != "m" || result["at"] == "" {
				t.Fatalf("calibration identity: %v", result)
			}
			if len(readLabels(t, dir, "intent")) != 2 || (len(mustLoad(t, path).Calibration) > 0) != success {
				t.Fatal("answers or calibration changed incorrectly")
			}
		})
	}
}

// 0.9.7: a second page of the same project exits while the first holds the page lock, even when the first answered /v1/status too slowly
// for the hook's probe (two pages per project were found running after the 0.9.6 swap). It neither listens nor writes the page record.
func TestSecondPageOfTheSameProjectExitsWhileTheFirstHoldsTheLock(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	takePageLock(t, common, repo)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a second page exits quietly: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a second page of the same project kept running while the first held the page lock")
	}
	if rec, ok := readPageRecord(common, repo); ok {
		t.Fatalf("the second page must not overwrite the first page's record: %+v", rec)
	}
	if len(opened) != 0 {
		t.Fatalf("the second page must not open the browser: %v", opened)
	}
}

// The page that holds the lock starts even when the old record's pid is alive (now another process): its own lock must not make that record look up.
func TestPageStartsWhenTheOldRecordsPidWasReused(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	if err := writePageRecord(common, repo, 1, os.Getpid()); err != nil { // a live pid, a port that does not answer
		t.Fatal(err)
	}
	ticks := pageOwnerTicksByHand(t)
	up := pageListening(t)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-up:
	case err := <-done:
		t.Fatalf("the page did not start: the old record with a reused pid looked up through the page's own lock (%v)", err)
	}
	if rec, ok := readPageRecord(common, repo); !ok || rec.Port == 1 {
		t.Fatalf("the page that started writes its own record: %+v", rec)
	}
	if err := os.Remove(pagePath(common, repo)); err != nil { // stop the page
		t.Fatal(err)
	}
	ticks <- time.Now()
	select {
	case <-done:
	case <-time.After(3 * time.Second): // only turns a page that never stops into a failure
		t.Fatal("the page does not stop once its record is gone")
	}
}

// 0.9.7 audit B-b: a page whose record no longer points at it (reconnecting a moved project removes the old path's record, uninstall too)
// or whose store is gone stops by itself instead of running on as an orphan.
func TestOrphanPageStopsByItself(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	ticks := pageOwnerTicksByHand(t)
	up := pageListening(t)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case <-up:
	case err := <-done:
		t.Fatalf("precondition: the page started: %v", err)
	}
	if err := os.Remove(pagePath(common, repo)); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now() // the owner check runs now
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a page whose record is gone keeps running")
	}
}

// 0.9.6 audit of the HTTP limits (eaf2c9b): the server cuts every response after WriteTimeout (2 minutes). While no calibration is active a
// slow reply is still cut (the limit stays); while a calibration holds or waits for the lock, replies in flight are kept alive (0.9.7).
func TestSlowRepliesAreCutUnlessACalibrationIsActive(t *testing.T) {
	tick := tickReplies(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	for _, active := range []bool{false, true} {
		working, finish := make(chan struct{}), make(chan struct{})
		slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			expireWrite(t, w) // the long work has run past the write timeout
			close(working)
			<-finish
			io.WriteString(w, "done")
		})
		ts := httptest.NewUnstartedServer(slow)
		ts.Config = newHTTPServer(slow)
		ts.Start()
		var release func()
		if active {
			release = s.lockCalibration()
		}
		got := fetchAsync(func() (*http.Response, error) { return http.Get(ts.URL) })
		<-working
		tick()
		if release != nil {
			release()
		}
		close(finish)
		r := <-got
		ts.Close()
		err, body := r.err, r.body
		if active && (err != nil || body != "done") {
			t.Fatalf("while a calibration is active the reply arrives after the write timeout: %v %q", err, body)
		}
		if !active && err == nil && body == "done" {
			t.Fatal("while no calibration is active the write timeout still cuts a slow reply")
		}
	}
}

// An answer that brings the truths to min_n calibrates in the same request, so it waits for the calibration lock. While a background tune holds
// the lock past the write timeout, that reply still arrives (the answer was saved; before 0.9.6 the page showed a connection error).
func TestAnswerThatCalibratesBehindATuneStillReplies(t *testing.T) {
	tick := tickReplies(t)
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
	var behind atomic.Bool // the next answer waits behind the tune
	waiting, goOn := make(chan struct{}), make(chan struct{})
	late := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if behind.Load() {
			expireWrite(t, w) // waiting behind the tune has run past the write timeout
			close(waiting)
			<-goOn
		}
		h.ServeHTTP(w, r)
	})
	ts := httptest.NewUnstartedServer(late)
	ts.Config = newHTTPServer(late)
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
	release := s.lockCalibration() // a background tune holds the lock
	behind.Store(true)
	type labeled struct {
		out map[string]any
		err error
	}
	got := make(chan labeled, 1)
	go func() { out, err := label(traces[1]); got <- labeled{out, err} }()
	<-waiting
	tick() // the reply's check sees the tune
	close(goOn)
	release()
	r := <-got
	out, err := r.out, r.err
	if err != nil {
		t.Fatalf("the answer's reply arrives after waiting for the lock past the write timeout: %v", err)
	}
	if out["calibrated"] != true {
		t.Fatalf("precondition: the second answer calibrates (so it waited for the lock): %v", out)
	}
}

// 0.8.0: decisions a person does not answer (wiki-conflict / wiki-place) are not counted as waiting for confirmation; only the number waiting for an outcome is shown. Hook decisions keep the old behavior, since they have spot checks for the person.
func TestStatusDoesNotCountMachineOnlySpecsAsWaitingForAPerson(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	for i := 0; i < 3; i++ {
		rec := store.Record{TraceID: fmt.Sprintf("wc%d", i), SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "a", "new_lesson": "b"}, InputsHash: fmt.Sprintf("h%d", i), Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	// "Unsure" in decisions whose truth comes from an outcome waits for the outcome; "unsure" in decisions a person answers waits for the person
	st.AppendRecord(store.Record{TraceID: "pk1", SpecID: "prompt-kind", Type: "choice", Model: "m", Inputs: map[string]string{"request": "x"}, InputsHash: "pk1", Raw: map[string]float64{"edit": .5, "explain": .5}, Top: "edit", Band: "ask", At: time.Now()})
	// np1: the agent saw nothing open (no) while the model leans yes: a disagreement, which waits for the person (0.11)
	st.AppendRecord(store.Record{TraceID: "np1", SpecID: "needs-plan", Type: "noul", Model: "m", Inputs: map[string]string{"request": "x", "agreed": "a", "open": planningOpenUnknown}, InputsHash: "np1", Raw: map[string]float64{"yes": .5, "no": .5}, Top: "yes", Band: "ask", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "np2", SpecID: "needs-plan", Type: "noul", Model: "m", Inputs: map[string]string{"request": "y", "agreed": "a", "open": "o"}, InputsHash: "np2", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []map[string]any `json:"specs"`
		Model map[string]any   `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String()[:200])
	}
	if body.Model["name"] == nil {
		t.Fatalf("the AI's state: which model is deciding: %v", body.Model)
	}
	got := map[string]map[string]any{}
	for _, sp := range body.Specs {
		got[sp["id"].(string)] = sp
	}
	wc := got["wiki-conflict"]
	if wc == nil || wc["person_answers"] != false || wc["waiting_for_person"] != float64(0) || wc["awaiting_results"] != float64(3) || wc["unlabeled"] != float64(3) || wc["truth_source"] != "results" {
		t.Fatalf("decisions a person does not answer: 0 waiting for a person, 3 waiting for an outcome: %v", wc)
	}
	if pk := got["prompt-kind"]; pk["waiting_for_person"] != float64(0) || pk["awaiting_results"] != float64(1) {
		t.Fatalf("unsure decisions whose truth comes from an outcome wait for the outcome: %v", pk)
	}
	if np := got["needs-plan"]; np["waiting_for_person"] != float64(0) || np["person_answers"] != false || np["awaiting_results"] != float64(0) || np["settled_by_ai"] != float64(0) {
		t.Fatalf("unsure decisions a person answers wait for the person; act outside spot checks is not waiting for an outcome but decided by the AI: %v", np)
	}
	nq := httptest.NewRecorder()
	h.ServeHTTP(nq, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=needs-plan", nil))
	if strings.Contains(nq.Body.String(), `"np1"`) || strings.Contains(nq.Body.String(), `"np2"`) {
		t.Fatalf("the queue of questions a person gives truths to also shows act (the person can correct it; told apart by waits_for_person): %s", nq.Body.String()[:300])
	}
	q := httptest.NewRecorder()
	h.ServeHTTP(q, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=prompt-kind", nil))
	if !strings.Contains(strings.ReplaceAll(q.Body.String(), " ", ""), `"waits_for_person":false`) {
		t.Fatalf("queue items also say whether they wait for a person: %s", q.Body.String()[:200])
	}
}

// Audit 0.9.3 low 5: `shikii status` shows the same learning count as the page (keys[].n, lineStateOf's N) and one number per line.
// Machine truths (context / judge) and a re-label of the same input must not raise it; the page's AI view says it has "the same numbers as `shikii status`".
func TestStatusLineCountMatchesThePage(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	specPath := filepath.Join(root, "decisions", "needs-plan.yaml")
	sp := mustLoad(t, specPath)
	st := store.Store{Dir: root}
	lab := func(trace, inputs, truth, source string) store.Label {
		return store.Label{TraceID: trace, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: inputs, Truth: truth, Weight: 1, Source: source, At: time.Now()}
	}
	labels := []store.Label{lab("a", "a", "yes", "ui"), lab("b", "b", "no", "ask"), lab("c", "c", "no", "outcome"),
		lab("a", "a", "no", "ui"),                                     // a re-label of input a: still one truth
		lab("x", "x", "yes", "context"), lab("y", "y", "no", "judge")} // machine truths: not counted
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []struct {
			ID   string           `json:"id"`
			Keys []map[string]any `json:"keys"`
		} `json:"specs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	page := -1
	for _, s := range body.Specs {
		if s.ID == sp.ID && len(s.Keys) == 1 {
			page = int(s.Keys[0]["n"].(float64))
		}
	}
	if page != 3 {
		t.Fatalf("keys[].n is calibration's N: the latest truth per input from people and results (3), got %d: %s", page, rec.Body.String())
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", root, specPath}) })
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "m1 @ "+sp.Hash()) {
			line = l
		}
	}
	m := regexp.MustCompile(`^\s*m1 @ \S+\s+(\d+)(.*)$`).FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("no status line for m1:\n%s", out)
	}
	if m[1] != fmt.Sprint(page) {
		t.Fatalf("status says %s where the page says %d:\n%s", m[1], page, line)
	}
	if regexp.MustCompile(`\d+/\d+`).MatchString(m[2]) {
		t.Fatalf("the status line shows one count, not a second n/min_n beside it: %q", line)
	}
}

// 0.11: 1 in 10 decisions an outcome answered goes to the person while that outcome's agreement is unsettled, marked as an outcome
// check; the others stay out; once the person's answers settle it (here: clearly below act), none is asked any more.
func TestQueueKeepsInternalOutcomeChecksOffTheAnswerPage(t *testing.T) {
	root, sampled := outcomeFixture(t, 60)
	if len(sampled) < 4 {
		t.Fatalf("the fixed sample picks some of 60: %d", len(sampled))
	}
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions")})
	queue := func() []map[string]any {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/queue?spec=changes-in-scope&limit=1000", nil))
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Items
	}
	if items := queue(); len(items) != 0 {
		t.Fatal("internal outcome checks do not require a human grade", items)
	}
	st := store.Store{Dir: root}
	for _, r := range sampled[:3] { // the person says the outcome was wrong 3 times: 0 of 3 agree, the interval's upper end 0.56 < act 0.75
		labels, err := store.LabelsFor(&r, "no", 1, "audit")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	if items := queue(); len(items) != 0 {
		t.Fatalf("once settled, no more outcome checks are asked: %d", len(items))
	}
}

// The Answer screen loads only the questions whose /v1/status waiting_for_person is above 0, so the outcome checks the queue shows
// are counted there too (0.11: they were only in the queue, and the Answer screen said nothing was waiting).
func TestStatusCountsOutcomeChecksAsWaitingForThePerson(t *testing.T) {
	root, sampled := outcomeFixture(t, 60)
	r := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions")}).ServeHTTP(r, httptest.NewRequest("GET", "/v1/status", nil))
	var body struct {
		Specs []map[string]any `json:"specs"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, s := range body.Specs {
		if s["id"] == "changes-in-scope" {
			if s["waiting_for_person"] != float64(0) {
				t.Fatalf("the outcome checks wait for the person on the Answer screen: %v, want %d", s["waiting_for_person"], len(sampled))
			}
			return
		}
	}
	t.Fatal("changes-in-scope is in the status")
}

// The outcome's rare answer is shown whole: with 60 "yes" (1 in 10 shown) and 5 "no", all 5 "no" wait for the person, and the "yes"
// side's agreement does not stop them (0.11: 12 of 12 "yes" settled changes-in-scope while its "no" were never checked).
func TestQueueKeepsRareInternalOutcomesOffTheAnswerPage(t *testing.T) {
	root, sampled := outcomeFixture(t, 60)
	sp := mustLoad(t, filepath.Join(root, "decisions", "changes-in-scope.yaml"))
	st := store.Store{Dir: root}
	var rare []string
	for i := 0; i < 5; i++ {
		inputs := map[string]string{"request": fmt.Sprint("odd change ", i), "files": "b.go"}
		r := store.Record{TraceID: fmt.Sprintf("%016x", 1000+i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Inputs: inputs, InputsHash: store.HashInputs(inputs), Model: "m", Raw: map[string]float64{"yes": .6, "no": .4}, Top: "yes", P: .6, Band: "ask", At: time.Now()}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		labels, err := store.LabelsFor(&r, "no", 1, "outcome")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
		rare = append(rare, r.TraceID)
	}
	for _, r := range sampled { // the person agrees with every sampled "yes"
		labels, err := store.LabelsFor(&r, "yes", 1, "audit")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions")}).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/queue?spec=changes-in-scope&limit=1000", nil))
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	shown := map[string]bool{}
	for _, it := range body.Items {
		shown[it["trace_id"].(string)] = true
	}
	if len(shown) != 0 {
		t.Fatal("even rare internal grades stay with agents", shown, rare)
	}

}
