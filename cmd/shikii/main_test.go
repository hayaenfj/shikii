package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// fakeSystemOne returns a fixed choice answer.
func fakeSystemOne(t *testing.T, model string, probs map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		top, _ := argmax(probs)
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeSpec(t *testing.T, dir, pii string) string {
	path := filepath.Join(dir, "intent.yaml")
	src := `id: intent
version: 1
type: choice
question: What is asked?
options:
  - id: change_request
    description: wants a change
  - id: question
    description: wants an answer
  - id: other
    description: none
state:
  template: "{{ message }}"
  required: [message]
policy:
  send_to: [local]
  pii: ` + pii + `
thresholds:
  act: 0.75
  ask: 0.60
eval:
  min_n: 2
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func capture(t *testing.T, fn func() error) string {
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	defer func() { stdout = old }()
	if err := fn(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	return buf.String()
}

var traceRe = regexp.MustCompile(`trace=([0-9a-f]{16})`)

// fakeByMessage returns a different distribution per state (message).
func fakeByMessage(t *testing.T, model string, table map[string]map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		probs, ok := table[strings.TrimSpace(req.State)]
		if !ok {
			http.Error(w, "unknown state "+req.State, 400)
			return
		}
		top, _ := argmax(probs)
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mustHash(t *testing.T, specPath string) string {
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	return sp.Hash()
}

// fakeNoulByMessage returns a fixed p(yes) per state.
func fakeNoulByMessage(t *testing.T, model string, table map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		p, ok := table[strings.TrimSpace(req.State)]
		if !ok {
			http.Error(w, "unknown state "+req.State, 400)
			return
		}
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "noul", Noul: p}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeNoulSpec(t *testing.T, dir string) string {
	path := filepath.Join(dir, "same.yaml")
	src := `id: same
version: 1
type: noul
question: Same case?
state:
  template: "{{ a }}"
  required: [a]
policy:
  send_to: [local]
  pii: false
thresholds:
  act: 0.70
  ask: 0.50
eval:
  min_n: 4
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLoad(t *testing.T, path string) *spec.Spec {
	t.Helper()
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// Reproduction tests for the third audit (2026-10-18).

func boolPtrMain(b bool) *bool { return &b }

// Tests that close the holes found in the audit of 2026-10-17. Each was confirmed to fail before being fixed.

// shippedModelURL is the model's address as the package ships it, read before TestMain closes it: the port of the real model server
// on the machine that runs the tests.
var shippedModelURL = hookLocalURL

// closedModelURL is the model's address every test starts with: a port nothing listens on.
const closedModelURL = "http://127.0.0.1:1"

// TestMain removes the developer's keys (TYPESAFE_API_KEY / DECISIONS_API_KEY) and SHIKII_MODEL before the tests.
// Otherwise tests that assume the local fake model send requests to the real api.typesafe.ai,
// and the store's records, calibrations and bands change and the tests fail (2026-10-23: 6 failed right after the key was put in an environment variable).
// Tests that try Jev (hook_hosted_test.go) call t.Setenv themselves.
func TestMain(m *testing.M) {
	for _, k := range []string{"TYPESAFE_API_KEY", "DECISIONS_API_KEY", "SHIKII_MODEL"} {
		os.Unsetenv(k)
	}
	// No test asks this machine's model server or looks on its PATH. Closing the port used to be each test's own work (hookEnv,
	// freshMachine), and a test that only set a temporary HOME and ran init or hook install asked http://127.0.0.1:8080/health and
	// found this machine's llama-server (0.31.0 audit C A-8, audit D A-6 and U-10: 17 tests did). Now every test starts closed: one
	// that needs a model sets its own address (hookEnv with a model, fakeAgentModel), one that needs llama-server found replaces lookPath.
	hookLocalURL = closedModelURL
	lookPath = func(file string) (string, error) {
		return "", fmt.Errorf("%s: the tests do not look on this machine's PATH", file)
	}
	// No test starts real background processes (up, serve) or a browser (a test that skipped the harness forgot a stub, and a real one was left running. 0.9.1)
	hookSpawn = recordSpawn
	openBrowser = func(url string) { opened = append(opened, url) }
	stopProcess = func(int) error { return nil } // uninstall never signals a real process from tests (0.9.3)
	processCommand = func(int) string { return "" }
	// Writing files out to the disk (on macOS a flush of the disk's cache) made the eight test groups wait on each other: a test that
	// takes 0.3 s alone took 1 to 2 s. Tests do not need the data to survive a power cut.
	store.SyncTemp = func(*os.File) error { return nil }
	syncPersonFile = func(*os.File) error { return nil }
	os.Exit(m.Run())
}
