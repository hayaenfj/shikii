package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

// Audit 0.9.2 (behavior), test holes: checks that survived when their line was broken on purpose.

// Test hole: hookSpecPath picks <id>-v<N>.yaml only when its id is the same (a file named needs-plan-v9.yaml holding another question is not used).
func TestHookSpecPathIgnoresAnotherIDsVersion(t *testing.T) {
	agent, _, res := wordingCheckFixture(t)
	b, _ := os.ReadFile(res.SpecPath)
	other := regexp.MustCompile(`(?m)^version: \d+$`).ReplaceAllString(string(b), "version: 99")
	if other == string(b) {
		t.Fatal("the fixture's spec has a version line")
	}
	stray := filepath.Join(agent, "decisions", "needs-plan-v9.yaml")
	if err := os.WriteFile(stray, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := hookSpecPath(agent, "needs-plan"); p != filepath.Join(agent, "decisions", "needs-plan.yaml") {
		t.Fatalf("a version file of another id is not used: %s", p)
	}
}

// Test hole: when the question changed after it was measured (another hash), the agent is not asked to check that old result
// (adopting it would fail with "the spec changed", and the person would be told it could not be adopted).
func TestWordingCheckNotAskedAfterTheQuestionChanged(t *testing.T) {
	agent, repo, res := wordingCheckFixture(t)
	sp, err := spec.Load(res.SpecPath)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(res.SpecPath)
	q := strings.TrimSpace(sp.Question)
	changed := strings.Replace(string(b), q, q+" (changed)", 1)
	if changed == string(b) {
		t.Fatal("the question is in the file as is")
	}
	if err := os.WriteFile(res.SpecPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": repo, "prompt": "Fix the typo in the README."}))
	if strings.Contains(ctx, "hook variants check") {
		t.Fatalf("do not ask to check a result measured on another question: %q", ctx)
	}
	_ = agent
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
