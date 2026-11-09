package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// For serve tests: set up a spec folder, a store and a fake model, and stand up the handler with httptest.
func newServeFixture(t *testing.T, pii string, probs map[string]float64) (srvURL string, specPath string, storeDir string) {
	t.Helper()
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath = writeSpec(t, specsDir, pii) // id: intent, min_n: 2
	storeDir = filepath.Join(dir, ".shikii")
	model := fakeSystemOne(t, "m", probs)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: storeDir, ef: ef, autoCalibrate: true})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL, specPath, storeDir
}

func postJSON(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func getJSON(t *testing.T, url string, into any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode
}

func bindHostFor(marker string) string { return systemone.BindHostFor([]string{marker}, "") }

// writeNamedNoulSpec writes a noul spec with the given id (min_n 4) into dir.
func writeNamedNoulSpec(t *testing.T, dir, id string) string {
	t.Helper()
	path := filepath.Join(dir, id+".yaml")
	src := "id: " + id + "\nversion: 1\ntype: noul\nquestion: Same case?\nstate:\n  template: \"{{ a }}\"\n  required: [a]\npolicy:\n  send_to: [local]\n  pii: false\nthresholds:\n  act: 0.70\n  ask: 0.50\neval:\n  min_n: 4\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// outcomeFixture is a project whose changes-in-scope has n decisions an outcome answered "yes", and the ones OutcomeCheckSample picks.
func outcomeFixture(t *testing.T, n int) (root string, sampled []store.Record) {
	t.Helper()
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root = filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "changes-in-scope.yaml"))
	st := store.Store{Dir: root}
	for i := 0; i < n; i++ {
		inputs := map[string]string{"request": fmt.Sprint("change ", i), "files": "a.go"}
		r := store.Record{TraceID: fmt.Sprintf("%016x", i+1), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Inputs: inputs, InputsHash: store.HashInputs(inputs), Model: "m", Raw: map[string]float64{"yes": .6, "no": .4}, Top: "yes", P: .6, Band: "ask", At: time.Now()}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		labels, err := store.LabelsFor(&r, "yes", 0.5, "outcome")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
		if store.OutcomeCheckSample(r.TraceID) {
			sampled = append(sampled, r)
		}
	}
	return root, sampled
}
