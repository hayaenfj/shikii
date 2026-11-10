package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// rawAnswerModel is a model that answers every question "q" with the given JSON.
func rawAnswerModel(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","answers":{"q":` + answer + `}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A yes/no answer without a value must not become a decision: the command fails as it does when the model call
// fails, prints no band, and leaves no record that would be counted as a confident "no".
func TestDecideCommandDoesNotRecordAYesNoAnswerWithoutAValue(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	specPath := writeNoulSpec(t, dir)
	for _, answer := range []string{`{"type":"noul"}`, `{"type":"noul","noul":null}`, `{"type":"choice","probabilities":{"a":0.9,"b":0.1}}`, `{"type":"noul","noul":1.5}`} {
		srv := rawAnswerModel(t, answer)
		out, err := captureErr(func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "a=x"})
		})
		if err == nil || !strings.Contains(err.Error(), "probabilities") || strings.Contains(out, "band=") {
			t.Errorf("%s: want an error and no decision, got err=%v out=%q", answer, err, out)
		}
		recs, rerr := store.Store{Dir: storeDir}.Records("same", 0)
		if rerr != nil || len(recs) != 0 {
			t.Fatalf("%s: nothing may be recorded, got %d records (%v): %+v", answer, len(recs), rerr, recs)
		}
	}
}

// Guard: an answer of exactly zero is the model saying "no". It is decided and recorded as before.
func TestDecideCommandRecordsAYesNoAnswerOfZero(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	specPath := writeNoulSpec(t, dir)
	srv := rawAnswerModel(t, `{"type":"noul","noul":0}`)
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "a=x"})
	})
	if !strings.Contains(out, "band=discard top=yes p=0.000") {
		t.Fatalf("a zero answer is a discard at p=0:\n%s", out)
	}
	recs, err := store.Store{Dir: storeDir}.Records("same", 0)
	if err != nil || len(recs) != 1 || recs[0].Raw["yes"] != 0 || recs[0].Raw["no"] != 1 {
		t.Fatalf("the zero answer must be recorded once: %+v %v", recs, err)
	}
	// A fake built from the Answer type (as the other tests' models are) keeps deciding, a zero included.
	typed := fakeNoulByMessage(t, "m", map[string]float64{"x": 0, "y": 0.9})
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", typed.URL, "--store", storeDir, "-i", "a=x"})
	})
	if !strings.Contains(out, "band=discard top=yes p=0.000") {
		t.Fatalf("a zero from a typed fake is a discard at p=0:\n%s", out)
	}
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", typed.URL, "--store", storeDir, "-i", "a=y"})
	})
	if !strings.Contains(out, "band=ask top=yes p=0.900") {
		t.Fatalf("a value keeps deciding as before:\n%s", out)
	}
}
