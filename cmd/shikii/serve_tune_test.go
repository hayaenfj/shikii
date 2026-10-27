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

	"github.com/hayaenfj/shikii/internal/spec"
)

// tuneDone returns a channel that receives the spec id once a background tune has finished and updated its state (tuneFinished).
func tuneDone(t *testing.T) <-chan string {
	t.Helper()
	ch := make(chan string, 1)
	old := tuneFinished
	tuneFinished = func(spec string) { ch <- spec }
	t.Cleanup(func() { tuneFinished = old })
	return ch
}

// waitTune waits for the one signal of a finished tune. The bound only turns a tune that never finishes into a failure instead of a hang.
func waitTune(t *testing.T, done <-chan string, spec string) {
	t.Helper()
	select {
	case got := <-done:
		if got != spec {
			t.Fatalf("finished tune for %q, want %q", got, spec)
		}
	case <-time.After(time.Minute):
		t.Fatal("the background tune never finished")
	}
}

// Run tune from the page, read the latest result, and adopt on the person's "same meaning". On "different", it is not adopted and stays in the run.
func TestServeRunsTuneAndAdoptsOnlyWithHumanMeaningCheck(t *testing.T) {
	url, specsDir, wiki, _ := newTuneFixture(t)
	done := tuneDone(t)
	code, r := postJSON(t, url+"/v1/tune/run", map[string]any{"spec": "intent"})
	if code != 202 {
		t.Fatalf("tune/run must be accepted and run in the background: %d %v", code, r)
	}
	waitTune(t, done, "intent")
	var st map[string]any
	getJSON(t, url+"/v1/tune?spec=intent", &st)
	if st["running"] != false {
		t.Fatalf("a finished tune must not be reported as running: %v", st)
	}
	latest, _ := st["latest"].(map[string]any)
	if latest == nil || latest["adoptable"] != true || latest["winner"] != "strict" {
		t.Fatalf("latest tune result must be exposed with the winner and whether it may be adopted: %v", st)
	}
	if w, _ := latest["winner_wording"].(map[string]any); w == nil || !strings.Contains(w["question"].(string), "strict") {
		t.Fatalf("the page needs the winner's wording to let a person check its meaning: %v", latest)
	}
	// "Different meaning" → not adopted. The rejection stays in the run
	code, rej := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"], "meaning_same": false, "note": "it now asks about tone"})
	if code != 200 || rej["adopted"] != false {
		t.Fatalf("rejecting must not adopt: %d %v", code, rej)
	}
	if _, err := os.Stat(filepath.Join(specsDir, "intent-v2.yaml")); err == nil {
		t.Fatal("rejected wording must not be written")
	}
	runs, _ := filepath.Glob(filepath.Join(wiki, "runs", "*.md"))
	if b, _ := os.ReadFile(runs[0]); !strings.Contains(string(b), "meaning") || !strings.Contains(string(b), "it now asks about tone") {
		t.Fatalf("the person's rejection and note must be recorded in the run page:\n%s", b)
	}
	// "Same meaning" → adopted, a new version, calibrated
	code, ok := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"], "meaning_same": true})
	if code != 200 || ok["adopted"] != true {
		t.Fatalf("adopt: %d %v", code, ok)
	}
	v2, err := spec.Load(filepath.Join(specsDir, "intent-v2.yaml"))
	if err != nil || !strings.Contains(v2.Question, "strict") || v2.CalibrationFor("m") == nil {
		t.Fatalf("adoption must write v2 and calibrate it: %v %+v", err, v2)
	}
	// After adopting, the result says adopted. The same run cannot be adopted twice
	getJSON(t, url+"/v1/tune?spec=intent", &st)
	if st["latest"].(map[string]any)["adopted"] != true {
		t.Fatalf("after adoption the result must say so: %v", st)
	}
	if code, _ := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"], "meaning_same": true}); code != 409 {
		t.Fatalf("adopting twice must be 409, got %d", code)
	}
	// Adopting without a meaning check is refused
	if code, _ := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"]}); code != 400 {
		t.Fatalf("adopt without meaning_same must be 400, got %d", code)
	}
}

// A person can write candidate wording on the page: POST /v1/tune/variant adds it to the spec's variants (touching nothing else), GET /v1/tune lists it, and the next tune scores it.
// No name or no content → 400, same name → 409, a choice id not in the spec → 400 (checked before writing).
func TestServeAddsAPersonsRewordingToVariants(t *testing.T) {
	url, specsDir, _, _ := newTuneFixture(t)
	path := filepath.Join(specsDir, "intent.yaml")
	before, _ := os.ReadFile(path)
	code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "plain-words", "question": "What does this message want?", "options": map[string]string{"other": "greetings and thanks only", "": "ignored"}})
	if code != 200 || r["added"] != "plain-words" {
		t.Fatalf("adding a rewording: %d %v", code, r)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "name: plain-words") || !strings.Contains(string(after), "greetings and thanks only") || !strings.Contains(string(after), "name: strict") {
		t.Fatalf("the rewording must be saved into variants next to the existing ones:\n%s", after)
	}
	if !strings.Contains(string(after), "question: What is asked?") {
		t.Fatalf("the spec's own wording must be untouched:\n%s\nbefore:\n%s", after, before)
	}
	var st map[string]any
	getJSON(t, url+"/v1/tune?spec=intent", &st)
	vs, _ := st["variants"].([]any)
	names := ""
	for _, v := range vs {
		names += v.(map[string]any)["name"].(string) + ","
	}
	if !strings.Contains(names, "plain-words,") || !strings.Contains(names, "strict,") {
		t.Fatalf("GET /v1/tune must list the candidates: %v", st["variants"])
	}
	// Errors: no name, no content, same name, a choice that does not exist
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "question": "x"}); code != 400 {
		t.Fatalf("no name must be 400: %d %v", code, r)
	}
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "empty"}); code != 400 {
		t.Fatalf("no question and no options must be 400: %d %v", code, r)
	}
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "plain-words", "question": "again"}); code != 409 {
		t.Fatalf("a duplicate name must be 409: %d %v", code, r)
	}
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "bad-id", "options": map[string]string{"nope": "not an option"}}); code != 400 || !strings.Contains(fmt.Sprint(r["error"]), "nope") {
		t.Fatalf("an unknown option id must be refused before writing: %d %v", code, r)
	}
	// Names are only letters, digits, hyphens and underscores (they go into wiki/runs file names): separators and line breaks are 400
	for _, bad := range []string{"x/../../outside", "with space", "a\nb", strings.Repeat("n", 41)} {
		if code, _ := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": bad, "question": "q"}); code != 400 {
			t.Fatalf("name %q must be refused: %d", bad, code)
		}
	}
	if code, _ := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "too-long", "question": strings.Repeat("q", 2001)}); code != 400 {
		t.Fatalf("a question over 2000 bytes must be refused: %d", code)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "bad-id") || strings.Contains(string(b), "name: empty") {
		t.Fatalf("refused rewordings must not be written:\n%s", b)
	}
	if code, _ := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "nope", "name": "x", "question": "y"}); code != 404 {
		t.Fatalf("unknown spec must be 404: %d", code)
	}
	// The next tune scores the person's candidate too
	done := tuneDone(t)
	if code, r := postJSON(t, url+"/v1/tune/run", map[string]any{"spec": "intent"}); code != 202 {
		t.Fatalf("tune/run: %d %v", code, r)
	}
	waitTune(t, done, "intent")
	getJSON(t, url+"/v1/tune?spec=intent", &st)
	if st["running"] != false {
		t.Fatalf("a finished tune must not be reported as running: %v", st)
	}
	latest, _ := st["latest"].(map[string]any)
	cands, _ := latest["candidates"].([]any)
	scored := ""
	for _, c := range cands {
		scored += c.(map[string]any)["name"].(string) + ","
	}
	if !strings.Contains(scored, "plain-words,") {
		t.Fatalf("the next tune must score the person's rewording: %v", scored)
	}
}

// 0.9.6 audit of the HTTP limits, middle 1, made structural in 0.9.7: a reply that waits for the calibration lock (a background tune holds it
// up to 40 minutes) is not cut by the write timeout, because taking the lock extends the reply's deadline. The test holds the lock past the
// write timeout, as a background tune would, and every route that waits for it still answers; status, which does not wait, is still cut.
// Each reply first runs past the write timeout (its deadline is set in the past), and the test runs the replies' check by hand while
// the lock is held.
func TestEveryRouteWaitingForTheLockOutlivesTheWriteTimeout(t *testing.T) {
	tick := tickReplies(t)
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "true")
	s := newServerState(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), autoCalibrate: true})
	h := s.handler()
	waiting, goOn := make(chan struct{}), make(chan struct{})
	late := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expireWrite(t, w) // waiting behind the tune has run past the write timeout
		waiting <- struct{}{}
		<-goOn
		h.ServeHTTP(w, r)
	})
	ts := httptest.NewUnstartedServer(late)
	ts.Config = newHTTPServer(late)
	ts.Start()
	defer ts.Close()
	reply := func(path, body string, calibrating bool) error {
		release := func() {}
		if calibrating {
			release = s.lockCalibration() // a background tune holds the lock
		}
		got := fetchAsync(func() (*http.Response, error) {
			return http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		})
		<-waiting
		tick()
		goOn <- struct{}{}
		release()
		return (<-got).err
	}
	for path, body := range map[string]string{
		"/v1/tune/sync":    `{"spec":"intent"}`,
		"/v1/tune/variant": `{"spec":"intent","name":"v2","question":"q"}`,
		"/v1/tune/adopt":   `{"spec":"intent","run":"r","meaning_same":true}`,
		"/v1/tune/review":  `{"spec":"intent","run":"r","keep":true}`,
	} {
		if err := reply(path, body, true); err != nil {
			t.Errorf("%s answers after waiting for the lock past the write timeout: %v", path, err)
		}
	}
	if err := reply("/v1/status", "", false); err == nil {
		t.Error("precondition: a reply while no calibration is active is still cut by the write timeout")
	}
}

// For wording adopted automatically, the person can choose "keep as is" or "revert" on the page. Reverting goes back to the previous version, and that run can no longer be adopted.
func TestWordingReviewKeepsOrReverts(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	adopted := got.AdoptedPath
	if code, _ := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run}); code != 400 {
		t.Fatalf("400 without keep (true/false): %d", code)
	}
	if code, r := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": false}); code != 200 || r["reverted"] != true {
		t.Fatalf("revert: %d %v", code, r)
	}
	if _, err := os.Stat(adopted); err == nil {
		t.Fatal("after reverting, rename the adopted version's file to a name that is not used")
	}
	if _, err := os.Stat(adopted + ".reverted"); err != nil {
		t.Fatal("keep the adopted version instead of deleting it")
	}
	if p := hookSpecPath(agent, "changes-in-scope"); p != res.SpecPath {
		t.Fatalf("after reverting, read the previous version: %s", p)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": true}); code != 409 {
		t.Fatalf("a reverted run can no longer be chosen: %d", code)
	}
	if code, _ := postJSON(t, srv.URL+"/v1/tune/adopt", map[string]any{"spec": res.SpecID, "run": res.Run, "meaning_same": true}); code != 409 {
		t.Fatalf("a reverted run cannot be adopted again: %d", code)
	}
	// Another run: keep as is → keep the person's confirmation and do not notify again
	agent2, _, res2 := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res2, "same")); err != nil {
		t.Fatal(err)
	}
	srv2 := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent2, "decisions"), storeDir: agent2}))
	t.Cleanup(srv2.Close)
	if code, r := postJSON(t, srv2.URL+"/v1/tune/review", map[string]any{"spec": res2.SpecID, "run": res2.Run, "keep": true}); code != 200 || r["reverted"] != false {
		t.Fatalf("keep as is: %d %v", code, r)
	}
	kept, _ := loadTuneResult(filepath.Join(agent2, "wiki"), res2.Run)
	if !kept.Adopted || len(kept.MeaningChecks) != 2 || kept.MeaningChecks[1].By != "page" || !kept.MeaningChecks[1].Same {
		t.Fatalf("keep the person's \"keep as is\": %+v", kept)
	}
}

// Test hole: /v1/tune/review undoes only a wording the agent adopted. A wording the person adopted on the page is not undone there (409).
func TestWordingReviewRefusesWhatThePersonAdopted(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	adopted := filepath.Join(agent, "decisions", res.SpecID+"-v2.yaml")
	b, _ := os.ReadFile(res.SpecPath)
	if err := os.WriteFile(adopted, b, 0o600); err != nil {
		t.Fatal(err)
	}
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.Adopted, r.AutoAdopted, r.AdoptedPath = true, false, adopted
		r.MeaningChecks = []meaningCheck{{Same: true, By: "page"}}
	})
	srv := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent}))
	t.Cleanup(srv.Close)
	if code, r := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": false}); code != 409 {
		t.Fatalf("a wording the person adopted is not undone by review: %d %v", code, r)
	}
	if _, err := os.Stat(adopted); err != nil {
		t.Fatal("the person's adopted version stays in use")
	}
}

// Audit 0.9.2 (behavior), low 2: the agent's adoption and the page's adoption of the same run share one lock.

// Low 2: the agent's adoption (hook variants check, another process) and the page's adoption could run at the same time.
// While the agent adopts (calibration takes time), the page's adopt and review are refused with 409.
func TestAuditPageAdoptDuringAgentAdoption(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	srv := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent}))
	t.Cleanup(srv.Close)
	inner := wordingAdopt
	var adoptCode, reviewCode int
	wordingAdopt = func(r *tuneResult, ef endpointFlags, storeDir, labeled string, reports ...io.Writer) (string, error) {
		adoptCode, _ = postJSON(t, srv.URL+"/v1/tune/adopt", map[string]any{"spec": res.SpecID, "run": res.Run, "meaning_same": true})
		reviewCode, _ = postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": true})
		return inner(r, ef, storeDir, labeled, reports...)
	}
	if out, err := submitWordingCheck(t, checkBody(res, "same")); err != nil || out["adopted"] != true {
		t.Fatalf("the agent's adoption: %v %v", out, err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	if adoptCode != 409 || reviewCode != 409 {
		t.Fatalf("while the agent adopts, the page is refused: adopt %d, review %d (checks %d, adopted=%v)", adoptCode, reviewCode, len(got.MeaningChecks), got.Adopted)
	}
	if !got.Adopted || len(got.MeaningChecks) != 1 {
		t.Fatalf("only the agent's adoption is kept: %+v", got)
	}
}

// Low 1 (and the UI audit's medium 2): undo on the page for a wording the agent adopted before the folder moved returned 500
// (it renamed the old absolute path).
func TestAuditRevertAfterMove(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	if _, err := submitWordingCheck(t, checkBody(res, "same")); err != nil {
		t.Fatal(err)
	}
	got, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run)
	real := got.AdoptedPath
	rewriteRunJSON(t, agent, res.Run, func(r *tuneResult) {
		r.AdoptedPath = filepath.Join(movedStore, "decisions", filepath.Base(r.AdoptedPath))
		r.SpecPath = filepath.Join(movedStore, "decisions", filepath.Base(r.SpecPath))
		r.RunPage = filepath.Join(movedStore, "wiki", "runs", filepath.Base(r.RunPage))
	})
	srv := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(agent, "decisions"), storeDir: agent}))
	t.Cleanup(srv.Close)
	if code, r := postJSON(t, srv.URL+"/v1/tune/review", map[string]any{"spec": res.SpecID, "run": res.Run, "keep": false}); code != 200 || r["reverted"] != true {
		t.Fatalf("undo after the folder moved: %d %v (in use: %s)", code, r, hookSpecPath(agent, res.SpecID))
	}
	if _, err := os.Stat(real); err == nil {
		t.Fatal("the adopted file in this project is no longer used")
	}
	if _, err := os.Stat(real + ".reverted"); err != nil {
		t.Fatal("the adopted file is kept as .reverted")
	}
	if after, _ := loadTuneResult(filepath.Join(agent, "wiki"), res.Run); !after.Reverted || after.AdoptedPath != real {
		t.Fatalf("the undo is kept in this wiki's result, with this project's path: %+v", after)
	}
}
