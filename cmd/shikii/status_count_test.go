package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

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
		t.Fatalf("fixture: the page counts 3 truths, got %d: %s", page, rec.Body.String())
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
