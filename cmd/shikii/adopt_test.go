package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
)

// Fixtures for serve + tune: a fake model whose answer changes with the wording, a spec with candidates, a truth file, a wiki.
func newTuneFixture(t *testing.T) (srvURL, specsDir, wiki, labeledPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir = filepath.Join(dir, "decisions")
	specWithVariants(t, dir) // decisions/intent.yaml with variants strict / worse; the wiki is expected next to decisions/, with no flag
	labeledPath = filepath.Join(dir, "rows.json")
	rows := bankRows()
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write eval.labeled into the spec (serve's tune cannot take it as a flag)
	p := filepath.Join(specsDir, "intent.yaml")
	b, _ := os.ReadFile(p)
	src := strings.Replace(string(b), "eval:\n  min_n: 2\n", "eval:\n  min_n: 2\n  labeled: "+labeledPath+"\n", 1)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	wiki = filepath.Join(dir, ".shikii", "wiki")
	model := wordingSensitiveModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL, specsDir, wiki, labeledPath
}

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

// MCP can run tune and read results, but has no adopt tool (checking meaning is the person's job).
func TestMCPCanTuneButCannotAdopt(t *testing.T) {
	_, specsDir, _, _ := newTuneFixture(t)
	model := wordingSensitiveModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(filepath.Dir(specsDir), ".shikii"), ef: ef})
	var in, out bytes.Buffer
	send := func(req map[string]any) map[string]any {
		in.Reset()
		out.Reset()
		b, _ := json.Marshal(req)
		in.Write(append(b, '\n'))
		if err := runMCP(bufioReader(&in), &out, h); err != nil {
			t.Fatal(err)
		}
		var resp map[string]any
		_ = json.Unmarshal(out.Bytes(), &resp)
		return resp
	}
	r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}))
	names := map[string]bool{}
	for _, tl := range r["tools"].([]any) {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if !names["shikii_tune"] {
		t.Fatalf("shikii_tune must exist: %v", names)
	}
	for n := range names {
		if strings.Contains(n, "adopt") {
			t.Fatalf("no adopt tool may be exposed to agents: %v", names)
		}
	}
	r = result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_tune", "arguments": map[string]any{"spec": "intent"}}}))
	sc, _ := r["structuredContent"].(map[string]any)
	if r["isError"] == true || sc == nil || sc["winner"] != "strict" || sc["adoptable"] != true {
		t.Fatalf("shikii_tune must run synchronously and return the result: %v", r)
	}
}

// The page has a "wording" section: read results, run tune, and a person checks meaning and adopts / rejects. Both languages.
func TestServeUIHasWordingSection(t *testing.T) {
	url, _, _, _ := newTuneFixture(t)
	html := uiText(t, url)
	for _, must := range []string{"v1/tune?spec=", "v1/tune/run", "v1/tune/adopt", "meaning_same", "意味は同じ", "意味が違う", "same meaning", "meaning changed", "v1/tune/variant", "言い換えの案を書く", "write a rewording"} {
		if !strings.Contains(html, must) {
			t.Fatalf("UI must have the wording section; missing %q", must)
		}
	}
}

func bufioReader(b *bytes.Buffer) *bufio.Reader { return bufio.NewReader(b) }

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
